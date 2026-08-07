package adgo

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type ExecutionQuery struct {
	PlanID       string
	PlanDigest   string
	Statuses     []ExecutionStatus
	UpdatedAfter time.Time
	UpdatedBefore time.Time
	Limit        int
}

func QueryExecutions(ctx context.Context, store Store, query ExecutionQuery) ([]ExecutionSummary, error) {
	catalog, ok := store.(ExecutionCatalog)
	if !ok {
		return nil, fmt.Errorf("adgo: execution query requires ExecutionCatalog")
	}
	ids, err := catalog.ListExecutionIDs(ctx)
	if err != nil {
		return nil, err
	}
	statusSet := map[ExecutionStatus]struct{}{}
	for _, status := range query.Statuses {
		statusSet[status] = struct{}{}
	}
	out := make([]ExecutionSummary, 0)
	for _, id := range ids {
		execution, err := store.Load(ctx, id)
		if err != nil {
			return nil, err
		}
		if query.PlanID != "" && execution.PlanID != query.PlanID {
			continue
		}
		if query.PlanDigest != "" && execution.PlanDigest != query.PlanDigest {
			continue
		}
		if len(statusSet) > 0 {
			if _, ok := statusSet[execution.Status]; !ok {
				continue
			}
		}
		if !query.UpdatedAfter.IsZero() && !execution.UpdatedAt.After(query.UpdatedAfter) {
			continue
		}
		if !query.UpdatedBefore.IsZero() && !execution.UpdatedAt.Before(query.UpdatedBefore) {
			continue
		}
		out = append(out, ExecutionSummary{
			ID:         execution.ID,
			PlanID:     execution.PlanID,
			PlanDigest: execution.PlanDigest,
			Version:    execution.Version,
			Status:     execution.Status,
			Failure:    execution.Failure,
		})
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

type AwaitOptions struct {
	PollInterval time.Duration
	AcceptHuman  bool
	AcceptWaiting bool
}

// Await blocks only the caller; workflow progress remains durable and may be
// performed by any coordinator/worker. It is useful for CLI/API request paths
// that want a synchronous facade over asynchronous execution.
func (e *Engine) Await(ctx context.Context, executionID string, options AwaitOptions) (*Execution, error) {
	if options.PollInterval <= 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	for {
		execution, err := e.store.Load(ctx, executionID)
		if err != nil {
			return nil, err
		}
		if terminal(execution.Status) || (options.AcceptHuman && execution.Status == StatusHuman) || (options.AcceptWaiting && execution.Status == StatusWaiting) {
			return execution, nil
		}
		if _, err := e.Advance(ctx, executionID); err != nil && err != ErrDeadlock {
			return nil, err
		}
		if err := sleepContext(ctx, options.PollInterval); err != nil {
			return nil, err
		}
	}
}

// RewindFrom invalidates one node and all of its target-plan descendants at a
// quiescent point. Every affected node receives a fresh revision epoch, so
// idempotency keys cannot accidentally reuse the result being invalidated.
func (e *Engine) RewindFrom(ctx context.Context, executionID, root, reason, actor string) (*Execution, error) {
	if _, ok := e.plan.Nodes[root]; !ok {
		return nil, fmt.Errorf("adgo: rewind root %s does not exist", root)
	}
	return e.mutate(ctx, executionID, func(x *Execution) error {
		if len(x.ActiveTasks) > 0 {
			return fmt.Errorf("adgo: rewind requires a quiescent execution")
		}
		if x.PlanDigest != e.plan.Digest {
			return fmt.Errorf("adgo: rewind plan pin mismatch")
		}
		affected := map[string]struct{}{root: {}}
		for id := range e.plan.descendants[root] {
			affected[id] = struct{}{}
		}
		ids := make([]string, 0, len(affected))
		for id := range affected {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			node := e.plan.Nodes[id]
			runtime := x.Nodes[id]
			if runtime == nil {
				continue
			}
			runtime.Status = NodeDormant
			runtime.Activated = false
			runtime.Outcome = ""
			runtime.NotBefore = time.Time{}
			runtime.CompletedAt = time.Time{}
			runtime.LastError = ""
			runtime.LastFailure = ""
			runtime.Signature = ""
			delete(x.WaitingFor, id)
			x.RevisionCounters[id]++
			for _, key := range node.Produces {
				delete(x.Data, key)
				delete(x.Artifacts, key)
			}
		}
		x.Nodes[root].Status = NodePending
		x.Nodes[root].Activated = true
		x.Status = StatusRunning
		x.Failure = ""
		appendHistory(x, "execution_rewound", root, reason, map[string]any{"actor": actor, "affected": ids})
		return nil
	})
}
