package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/lang"
)

type durableActivityFailure struct {
	cause error
}

func (e durableActivityFailure) Error() string {
	return "activity execution failed"
}

func (e durableActivityFailure) Unwrap() error {
	return e.cause
}

func redactActivityFailure(err error) error {
	if err == nil {
		return nil
	}
	return durableActivityFailure{cause: err}
}

// RunUntilIdleWithPolicies drains activity tasks while enforcing the retry and
// timeout values declared by the activity policy.
func (e *Engine) RunUntilIdleWithPolicies(ctx context.Context, executionID string) error {
	return e.runUntilIdleWithPolicies(ctx, executionID)
}

func (e *Engine) runUntilIdleWithPolicies(ctx context.Context, executionID string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		task, err := e.store.PollTaskWithLease(ctx, executionID, "inline-worker", time.Minute)
		if err != nil {
			return err
		}
		if task == nil {
			return nil
		}

		activity, ok := e.activities[task.ActivityName]
		if !ok {
			return diag.Error{
				Code:    "AX501",
				Kind:    "config",
				Entity:  task.ActivityName,
				Message: fmt.Sprintf("activity %s is not registered", task.ActivityName),
				Hint:    "Register the Go function with WithActivity or Act before running the engine.",
			}
		}

		activityCtx := ctx
		cancel := func() {}
		if timeout := e.activityTimeout(task.ActivityName); timeout > 0 {
			activityCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		result, runErr := activity(activityCtx, cloneAnyMap(task.Input))
		cancel()

		if err := ctx.Err(); err != nil {
			return err
		}
		durableErr := redactActivityFailure(runErr)
		if durableErr != nil && task.Attempt < task.MaxAttempts {
			if err := e.withStoreTransaction(ctx, func(working *Engine) error {
				return working.scheduleActivityRetry(ctx, task, durableErr)
			}); err != nil {
				return err
			}
			continue
		}

		if err := e.withStoreTransaction(ctx, func(working *Engine) error {
			return working.completeActivity(ctx, executionID, task, result, durableErr)
		}); err != nil {
			return err
		}
	}
}

func (e *Engine) activityTimeout(activityName string) time.Duration {
	activity, ok := e.module.Activities[activityName]
	if !ok || activity.Policy == "" {
		return 0
	}
	policy, ok := e.module.Policies[activity.Policy]
	if !ok {
		return 0
	}
	expr, ok := policy.Entries["timeout"]
	if !ok || expr == nil || expr.Kind != lang.ExprLiteral {
		return 0
	}

	var text string
	switch value := expr.Value.(type) {
	case lang.DurationLiteral:
		text = string(value)
	case string:
		text = value
	default:
		return 0
	}
	duration, err := time.ParseDuration(text)
	if err != nil || duration <= 0 {
		return 0
	}
	return duration
}

func (e *Engine) scheduleActivityRetry(ctx context.Context, task *ActivityTask, runErr error) error {
	if task == nil {
		return fmt.Errorf("axiom: activity task is required")
	}
	now := e.now()
	task.Status = TaskPending
	task.Error = runErr.Error()
	task.LockedBy = ""
	task.LockedUntil = time.Time{}
	task.NextAttemptAt = now
	task.UpdatedAt = now
	if err := e.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return e.store.AppendHistory(ctx, task.ExecutionID, "ActivityRetryScheduled", map[string]any{
		"activity":    task.ActivityName,
		"rule":        task.RuleName,
		"task":        task.ID,
		"attempt":     task.Attempt,
		"maxAttempts": task.MaxAttempts,
		"error":       runErr.Error(),
	})
}
