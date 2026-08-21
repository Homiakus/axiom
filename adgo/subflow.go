package adgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

type FanoutItem struct {
	ID      string
	Initial map[string]any
}

type ChildResult struct {
	ID     string
	Status ExecutionStatus
	Error  string
}

type FanoutResult struct {
	Children  []ChildResult
	Completed int
	Waiting   int
	Failed    int
	Satisfied bool
}

// RunFanout executes or resumes a bounded set of durable child executions.
// Child execution IDs are deterministic, so repeating the parent activity after
// a crash resumes existing children instead of duplicating side effects.
func RunFanout(ctx context.Context, parentExecutionID, nodeID string, child *Runtime, items []FanoutItem, maxFanout, concurrency int, join JoinSpec) (FanoutResult, error) {
	if child == nil {
		return FanoutResult{}, fmt.Errorf("adgo: child runtime is required")
	}
	if maxFanout <= 0 {
		return FanoutResult{}, fmt.Errorf("adgo: maxFanout must be > 0")
	}
	if len(items) > maxFanout {
		return FanoutResult{}, fmt.Errorf("adgo: fanout %d exceeds maxFanout %d", len(items), maxFanout)
	}
	if concurrency <= 0 || concurrency > len(items) {
		concurrency = len(items)
	}
	if concurrency == 0 {
		return FanoutResult{Satisfied: join.Mode == JoinAll}, nil
	}

	type indexed struct {
		i int
		r ChildResult
	}
	sem := make(chan struct{}, concurrency)
	ch := make(chan indexed, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		if item.ID == "" {
			return FanoutResult{}, fmt.Errorf("adgo: fanout item id is required")
		}
		wg.Add(1)
		go func(i int, item FanoutItem) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				ch <- indexed{i: i, r: ChildResult{ID: item.ID, Status: StatusFailed, Error: ctx.Err().Error()}}
				return
			}
			defer func() { <-sem }()
			childID := ChildExecutionID(parentExecutionID, nodeID, item.ID)
			_, err := child.Start(ctx, childID, item.Initial, BudgetLimit{})
			if err != nil && !errors.Is(err, ErrExecutionExists) {
				ch <- indexed{i: i, r: ChildResult{ID: item.ID, Status: StatusFailed, Error: err.Error()}}
				return
			}
			exec, runErr := child.Run(ctx, childID)
			if runErr != nil && exec == nil {
				ch <- indexed{i: i, r: ChildResult{ID: item.ID, Status: StatusFailed, Error: runErr.Error()}}
				return
			}
			cr := ChildResult{ID: item.ID}
			if exec != nil {
				cr.Status = exec.Status
			}
			if runErr != nil {
				cr.Error = runErr.Error()
			}
			ch <- indexed{i: i, r: cr}
		}(i, item)
	}
	wg.Wait()
	close(ch)
	indexedResults := make([]indexed, 0, len(items))
	for v := range ch {
		indexedResults = append(indexedResults, v)
	}
	sort.Slice(indexedResults, func(i, j int) bool { return indexedResults[i].i < indexedResults[j].i })
	out := FanoutResult{Children: make([]ChildResult, 0, len(indexedResults))}
	for _, v := range indexedResults {
		out.Children = append(out.Children, v.r)
		switch v.r.Status {
		case StatusCompleted:
			out.Completed++
		case StatusWaiting, StatusHuman:
			out.Waiting++
		case StatusFailed, StatusDeadlocked, StatusCanceled:
			out.Failed++
		}
	}
	out.Satisfied = fanoutJoinSatisfied(out, len(items), join)
	return out, nil
}

func fanoutJoinSatisfied(r FanoutResult, total int, join JoinSpec) bool {
	switch join.Mode {
	case JoinAny:
		return r.Completed >= 1
	case JoinNOfM, JoinQuorum:
		return join.Threshold > 0 && r.Completed >= join.Threshold
	default:
		return total > 0 && r.Completed == total
	}
}
