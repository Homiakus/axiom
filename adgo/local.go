package adgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// LocalRunOptions configures Engine.RunLocal. RunLocal is intended for CLIs,
// embedded applications and durable nested workflows that want production
// Engine semantics without implementing their own coordinator/worker loop.
type LocalRunOptions struct {
	Worker WorkerSpec

	// WaitForExternal keeps the call alive while the execution is waiting only
	// for an external event. The default is false so a CLI can return a durable
	// waiting execution instead of blocking indefinitely.
	WaitForExternal bool
}

// RunLocal drives one execution using the production Engine protocol until it
// reaches a terminal/human state, or until it is durably waiting for an
// external event and WaitForExternal is false.
//
// Control flow, retries, leases, heartbeats, fencing, scheduling and commit
// boundaries remain owned by ADGO. Application code only supplies activities.
func (e *Engine) RunLocal(ctx context.Context, executionID string, options LocalRunOptions) (*Execution, error) {
	if e == nil {
		return nil, fmt.Errorf("adgo: engine is nil")
	}
	if executionID == "" {
		return nil, fmt.Errorf("adgo: execution id is required")
	}
	spec := e.normalizeWorker(options.Worker)

	for {
		if err := ctx.Err(); err != nil {
			execution, _ := e.store.Load(context.Background(), executionID)
			return execution, err
		}

		_, advanceErr := e.Advance(ctx, executionID)
		if advanceErr != nil && !errors.Is(advanceErr, ErrDeadlock) {
			return nil, advanceErr
		}
		execution, err := e.store.Load(ctx, executionID)
		if err != nil {
			return nil, err
		}
		if terminal(execution.Status) || execution.Status == StatusHuman {
			if errors.Is(advanceErr, ErrDeadlock) {
				return execution, advanceErr
			}
			return execution, nil
		}

		items := make([]*WorkItem, 0, spec.Concurrency)
		for len(items) < spec.Concurrency {
			item, pollErr := e.pollExecution(ctx, executionID, spec)
			if errors.Is(pollErr, ErrNoWork) {
				break
			}
			if pollErr != nil {
				return execution, pollErr
			}
			items = append(items, item)
		}
		if len(items) > 0 {
			if err := e.executeLocalBatch(ctx, spec, items); err != nil {
				return nil, err
			}
			continue
		}

		execution, err = e.store.Load(ctx, executionID)
		if err != nil {
			return nil, err
		}
		if terminal(execution.Status) || execution.Status == StatusHuman {
			return execution, nil
		}
		if delay, ok := localWakeDelay(execution); ok {
			if delay < 0 {
				delay = 0
			}
			if err := sleepContext(ctx, delay); err != nil {
				return execution, err
			}
			continue
		}
		if !options.WaitForExternal {
			return execution, nil
		}
		if err := sleepContext(ctx, spec.PollInterval); err != nil {
			return execution, err
		}
	}
}

// pollExecution is the execution-scoped counterpart of Poll. It prevents an
// embedded RunLocal caller from accidentally claiming work belonging to another
// execution sharing the same durable store.
func (e *Engine) pollExecution(ctx context.Context, executionID string, spec WorkerSpec) (*WorkItem, error) {
	spec = e.normalizeWorker(spec)
	execution, err := e.store.Load(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if err := e.verifyPinned(execution); err != nil {
		return nil, err
	}
	if terminal(execution.Status) || executionPaused(execution) {
		return nil, ErrNoWork
	}

	type pending struct {
		task  TaskRuntime
		node  Node
		score float64
	}
	now := time.Now().UTC()
	list := make([]pending, 0, len(execution.ActiveTasks))
	for _, task := range execution.ActiveTasks {
		if task.Status != TaskPending {
			continue
		}
		node, ok := e.plan.Nodes[task.NodeID]
		if !ok || !workerAccepts(spec, node, task.Activity) {
			continue
		}
		age := now.Sub(execution.CreatedAt).Minutes()
		score := node.CriticalPathWeight + 3*node.ExpectedQualityGain - node.EstimatedCost - 0.05*node.EstimatedLatency.Seconds() + 0.001*age
		list = append(list, pending{task: task, node: node, score: score})
	}
	if len(list) == 0 {
		return nil, ErrNoWork
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score == list[j].score {
			return list[i].task.ID < list[j].task.ID
		}
		return list[i].score > list[j].score
	})
	for _, candidate := range list {
		item, claimErr := e.claim(ctx, executionID, candidate.task.ID, spec, candidate.score)
		if claimErr == nil {
			return item, nil
		}
		if errors.Is(claimErr, ErrStaleTask) || errors.Is(claimErr, ErrConflict) {
			continue
		}
		return nil, claimErr
	}
	return nil, ErrNoWork
}

func (e *Engine) executeLocalBatch(ctx context.Context, base WorkerSpec, items []*WorkItem) error {
	if len(items) == 1 {
		return e.executeWorkItem(ctx, base, items[0])
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(items))
	for index, item := range items {
		index, item := index, item
		wg.Add(1)
		go func() {
			defer wg.Done()
			spec := base
			spec.ID = fmt.Sprintf("%s/local-%d", base.ID, index+1)
			// The task was claimed using base.ID. Preserve the fencing identity;
			// the suffix is only useful when RunWorker itself performs the claim.
			spec.ID = item.Token.WorkerID
			err := e.executeWorkItem(ctx, spec, item)
			if err != nil && !errors.Is(err, ErrStaleTask) {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func localWakeDelay(execution *Execution) (time.Duration, bool) {
	if execution == nil {
		return 0, false
	}
	now := time.Now().UTC()
	var wake time.Time
	consider := func(at time.Time) {
		if at.IsZero() {
			return
		}
		if wake.IsZero() || at.Before(wake) {
			wake = at
		}
	}
	for _, node := range execution.Nodes {
		if node != nil && node.Status == NodePending {
			consider(node.NotBefore)
		}
	}
	for _, task := range execution.ActiveTasks {
		if task.Status == TaskRunning {
			consider(task.LeaseUntil)
		}
	}
	if wake.IsZero() {
		return 0, false
	}
	return wake.Sub(now), true
}
