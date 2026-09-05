package adgo

import (
	"context"
	"fmt"
)

// CommitFenced executes a trusted infrastructure mutation only while the
// supplied WorkToken still owns the live ADGO task lease. The lease/fence check
// and the mutation run inside the same optimistic store commit, so a stale,
// recovered, or superseded worker cannot publish through this boundary.
//
// This is intentionally an expert integration API rather than a general
// workflow mutation surface. Domain code should normally complete activities
// through Complete. Bridges that use CommitFenced must preserve ADGO control
// fields (Nodes, ActiveTasks, WaitingFor, status and budgets) unless their
// contract explicitly owns those fields.
func (e *Engine) CommitFenced(
	ctx context.Context,
	token WorkToken,
	mutate func(*Execution) error,
) (*Execution, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("adgo: engine/store is required for fenced commit")
	}
	if mutate == nil {
		return nil, fmt.Errorf("adgo: fenced mutation is required")
	}
	if token.ExecutionID == "" || token.TaskID == "" || token.WorkerID == "" || token.Attempt <= 0 {
		return nil, ErrStaleTask
	}

	// Capture one semantic-time instant, matching Complete/Heartbeat lease
	// semantics. validateClaim executes inside e.mutate's store-CAS closure and
	// is therefore re-evaluated against the exact execution version being
	// committed.
	now := e.now()
	return e.mutate(ctx, token.ExecutionID, func(execution *Execution) error {
		if _, err := validateClaim(execution, token, now); err != nil {
			return err
		}
		return mutate(execution)
	})
}
