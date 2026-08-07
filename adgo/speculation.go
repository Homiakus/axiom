package adgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type ActivityVariant struct {
	Name    string
	Handler ActivityHandler
}

type SpeculationPolicy struct {
	// Pure must be explicitly true. Speculation against external side effects is
	// rejected because multiple variants may execute concurrently.
	Pure        bool
	HedgeDelay  time.Duration
	MaxParallel int
	MinQuality  float64
}

type variantResult struct {
	name     string
	result   ActivityResult
	err      error
	duration time.Duration
}

// NewHedgedActivity starts the preferred variant first and progressively opens
// additional variants after HedgeDelay. The first result meeting MinQuality
// becomes the provisional winner and cancels the others. Launched results are
// drained so their budget usage can be accounted conservatively.
func NewHedgedActivity(variants []ActivityVariant, policy SpeculationPolicy) (ActivityHandler, error) {
	if !policy.Pure {
		return nil, fmt.Errorf("adgo: hedged execution requires Pure=true")
	}
	variants, err := validateVariants(variants)
	if err != nil {
		return nil, err
	}
	if policy.MaxParallel <= 0 || policy.MaxParallel > len(variants) {
		policy.MaxParallel = len(variants)
	}
	if policy.HedgeDelay <= 0 {
		policy.HedgeDelay = 100 * time.Millisecond
	}
	return func(ctx context.Context, request ActivityRequest) (ActivityResult, error) {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		results := make(chan variantResult, policy.MaxParallel)
		launched := 0
		completed := 0
		launch := func(variant ActivityVariant) {
			launched++
			go func() {
				started := time.Now()
				result, err := variant.Handler(runCtx, request)
				results <- variantResult{name: variant.Name, result: result, err: err, duration: time.Since(started)}
			}()
		}
		launch(variants[0])
		next := 1
		timer := time.NewTimer(policy.HedgeDelay)
		defer timer.Stop()
		all := make([]variantResult, 0, policy.MaxParallel)
		winner := -1

		for completed < launched || (winner < 0 && next < policy.MaxParallel) {
			select {
			case <-ctx.Done():
				cancel()
				return ActivityResult{}, ctx.Err()
			case value := <-results:
				completed++
				all = append(all, value)
				if winner < 0 && value.err == nil && QualityUtility(value.result.Quality) >= policy.MinQuality {
					winner = len(all) - 1
					cancel()
				}
				if winner < 0 && completed == launched && next < policy.MaxParallel {
					launch(variants[next])
					next++
					resetTimer(timer, policy.HedgeDelay)
				}
			case <-timer.C:
				if winner < 0 && next < policy.MaxParallel {
					launch(variants[next])
					next++
					resetTimer(timer, policy.HedgeDelay)
				}
			}
			if winner >= 0 && completed == launched {
				break
			}
			if winner < 0 && completed == launched && next >= policy.MaxParallel {
				break
			}
		}
		return chooseSpeculativeResult(all, policy.MinQuality)
	}, nil
}

// NewEnsembleActivity executes all variants up to MaxParallel and chooses the
// highest-quality successful result with deterministic name tie-breaking. Budget
// usage is the aggregate of every executed variant, not just the winner.
func NewEnsembleActivity(variants []ActivityVariant, policy SpeculationPolicy) (ActivityHandler, error) {
	if !policy.Pure {
		return nil, fmt.Errorf("adgo: ensemble execution requires Pure=true")
	}
	variants, err := validateVariants(variants)
	if err != nil {
		return nil, err
	}
	if policy.MaxParallel <= 0 || policy.MaxParallel > len(variants) {
		policy.MaxParallel = len(variants)
	}
	return func(ctx context.Context, request ActivityRequest) (ActivityResult, error) {
		sem := make(chan struct{}, policy.MaxParallel)
		results := make(chan variantResult, len(variants))
		var wg sync.WaitGroup
		for _, variant := range variants {
			variant := variant
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					results <- variantResult{name: variant.Name, err: ctx.Err()}
					return
				}
				defer func() { <-sem }()
				started := time.Now()
				result, err := variant.Handler(ctx, request)
				results <- variantResult{name: variant.Name, result: result, err: err, duration: time.Since(started)}
			}()
		}
		wg.Wait()
		close(results)
		all := make([]variantResult, 0, len(variants))
		for result := range results {
			all = append(all, result)
		}
		return chooseSpeculativeResult(all, policy.MinQuality)
	}, nil
}

func validateVariants(variants []ActivityVariant) ([]ActivityVariant, error) {
	if len(variants) == 0 {
		return nil, fmt.Errorf("adgo: at least one activity variant is required")
	}
	copy := append([]ActivityVariant(nil), variants...)
	seen := map[string]struct{}{}
	for index, variant := range copy {
		if variant.Handler == nil {
			return nil, fmt.Errorf("adgo: variant %d handler is nil", index)
		}
		if variant.Name == "" {
			variant.Name = fmt.Sprintf("variant-%03d", index+1)
			copy[index].Name = variant.Name
		}
		if _, exists := seen[variant.Name]; exists {
			return nil, fmt.Errorf("adgo: duplicate variant name %q", variant.Name)
		}
		seen[variant.Name] = struct{}{}
	}
	return copy, nil
}

func chooseSpeculativeResult(all []variantResult, minQuality float64) (ActivityResult, error) {
	if len(all) == 0 {
		return ActivityResult{}, errors.New("adgo: no speculative result")
	}
	budget := BudgetUsage{}
	errorsSeen := []error{}
	valid := make([]variantResult, 0, len(all))
	for _, value := range all {
		addBudget(&budget, value.result.Budget)
		if value.err != nil {
			errorsSeen = append(errorsSeen, fmt.Errorf("%s: %w", value.name, value.err))
			continue
		}
		if QualityUtility(value.result.Quality) < minQuality {
			errorsSeen = append(errorsSeen, fmt.Errorf("%s: quality %.4f below %.4f", value.name, QualityUtility(value.result.Quality), minQuality))
			continue
		}
		valid = append(valid, value)
	}
	if len(valid) == 0 {
		return ActivityResult{Budget: budget}, errors.Join(errorsSeen...)
	}
	sort.SliceStable(valid, func(i, j int) bool {
		left := QualityUtility(valid[i].result.Quality)
		right := QualityUtility(valid[j].result.Quality)
		if left == right {
			return valid[i].name < valid[j].name
		}
		return left > right
	})
	winner := valid[0].result
	winner.Budget = budget
	if winner.Metrics == nil {
		winner.Metrics = map[string]float64{}
	}
	winner.Metrics["speculative_variants"] = float64(len(all))
	winner.Metrics["winner_quality"] = QualityUtility(valid[0].result.Quality)
	return winner, nil
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
