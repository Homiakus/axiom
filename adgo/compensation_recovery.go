package adgo

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type CompensationPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Timeout     time.Duration
}

func DefaultCompensationPolicy() CompensationPolicy {
	return CompensationPolicy{MaxAttempts: 5, BaseDelay: 250 * time.Millisecond, MaxDelay: 10 * time.Second, Timeout: 30 * time.Second}
}

// WithCompensationPolicy makes a compensation handler bounded and retry-aware.
// The ActivityRequest idempotency key remains unchanged across attempts, so
// external undo operations must still implement at-least-once semantics.
func WithCompensationPolicy(policy CompensationPolicy, handler CompensationHandler) CompensationHandler {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = 100 * time.Millisecond
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 5 * time.Second
	}
	return func(ctx context.Context, request ActivityRequest) error {
		if handler == nil {
			return fmt.Errorf("adgo: compensation handler is nil")
		}
		var last error
		for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
			callCtx := ctx
			cancel := func() {}
			if policy.Timeout > 0 {
				callCtx, cancel = context.WithTimeout(ctx, policy.Timeout)
			}
			last = handler(callCtx, request)
			cancel()
			if last == nil {
				return nil
			}
			if attempt == policy.MaxAttempts {
				break
			}
			class := DefaultClassify(last)
			if class != FailureTransient && class != FailureRateLimit {
				break
			}
			delay := policy.BaseDelay
			for i := 1; i < attempt && delay < policy.MaxDelay; i++ {
				delay *= 2
			}
			if delay > policy.MaxDelay {
				delay = policy.MaxDelay
			}
			var failure *FailureError
			if errors.As(last, &failure) && failure.RetryAfter > delay {
				delay = failure.RetryAfter
			}
			if err := sleepContext(ctx, delay); err != nil {
				return err
			}
		}
		return last
	}
}

// RecoverCompensation resumes a saga whose process died after the execution was
// durably marked compensating. Cancellation intent is durable, so the final
// status can be reconstructed without a separate volatile coordinator state.
func (e *Engine) RecoverCompensation(ctx context.Context, executionID string) (*Execution, error) {
	execution, err := e.store.Load(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if execution.Status != StatusCompensating {
		return execution, nil
	}
	finalStatus := StatusFailed
	reason := execution.Failure
	if execution.CancelRequested {
		finalStatus = StatusCanceled
		if reason == "" {
			reason = "cancellation requested"
		}
	} else if reason == "" {
		reason = "execution failed and compensation was resumed"
	}
	if len(execution.CompensationStack) == 0 {
		completed, err := e.mutate(ctx, executionID, func(x *Execution) error {
			x.Status = finalStatus
			x.Failure = reason
			x.Metrics.WallTime = time.Since(x.CreatedAt)
			appendHistory(x, "compensation_recovered_empty", "", reason, map[string]any{"finalStatus": finalStatus})
			return nil
		})
		return completed, err
	}
	if err := e.runtime.compensate(ctx, execution, finalStatus, reason); err != nil {
		return e.store.Load(ctx, executionID)
	}
	return e.store.Load(ctx, executionID)
}

// RunResilientCoordinator is the production coordinator loop. In addition to
// normal graph advancement it recognizes interrupted compensation state and
// resumes the saga before scheduling any forward work.
func (e *Engine) RunResilientCoordinator(ctx context.Context) error {
	catalog, ok := e.store.(ExecutionCatalog)
	if !ok {
		return fmt.Errorf("adgo: coordinator requires ExecutionCatalog")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := catalog.ListExecutionIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			execution, err := e.store.Load(ctx, id)
			if err != nil {
				if errors.Is(err, ErrExecutionNotFound) {
					continue
				}
				return err
			}
			if execution.PlanID != e.plan.ID || execution.PlanDigest != e.plan.Digest || terminal(execution.Status) {
				continue
			}
			if execution.Status == StatusCompensating {
				if _, err := e.RecoverCompensation(ctx, id); err != nil {
					return err
				}
				continue
			}
			if _, err := e.Advance(ctx, id); err != nil && !errors.Is(err, ErrDeadlock) {
				return err
			}
		}
		if err := sleepContext(ctx, e.coordinatorInterval); err != nil {
			return err
		}
	}
}

func (h *Host) RunResilientCoordinator(ctx context.Context) error {
	catalog := h.store.(ExecutionCatalog)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := catalog.ListExecutionIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			engine, execution, err := h.engineForExecution(ctx, id)
			if err != nil {
				continue
			}
			if terminal(execution.Status) {
				continue
			}
			if execution.Status == StatusCompensating {
				if _, err := engine.RecoverCompensation(ctx, id); err != nil {
					return err
				}
				continue
			}
			if _, err := engine.Advance(ctx, id); err != nil && !errors.Is(err, ErrDeadlock) {
				return err
			}
		}
		if err := sleepContext(ctx, 50*time.Millisecond); err != nil {
			return err
		}
	}
}
