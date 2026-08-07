package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/compiler"
	"github.com/Homiakus/axiom/internal/lang"
)

type activityRuntimePolicy struct {
	timeout     time.Duration
	concurrency string
}

func applyActivityPolicies(module *compiler.Module, activities ActivityRegistry) ActivityRegistry {
	if len(activities) == 0 || module == nil {
		return activities
	}
	out := make(ActivityRegistry, len(activities))
	for name, fn := range activities {
		if fn == nil {
			continue
		}
		policy := activityPolicy(module, name)
		out[name] = wrapActivityWithPolicy(fn, policy)
	}
	return out
}

func activityPolicy(module *compiler.Module, activityName string) activityRuntimePolicy {
	activity, ok := module.Activities[activityName]
	if !ok || activity.Policy == "" {
		return activityRuntimePolicy{}
	}
	policy, ok := module.Policies[activity.Policy]
	if !ok {
		return activityRuntimePolicy{}
	}

	var result activityRuntimePolicy
	if expr := policy.Entries["timeout"]; expr != nil && expr.Kind == lang.ExprLiteral {
		switch value := expr.Value.(type) {
		case lang.DurationLiteral:
			if parsed, err := time.ParseDuration(string(value)); err == nil && parsed > 0 {
				result.timeout = parsed
			}
		case time.Duration:
			if value > 0 {
				result.timeout = value
			}
		}
	}
	if expr := policy.Entries["concurrency"]; expr != nil && expr.Kind == lang.ExprLiteral {
		if value, ok := expr.Value.(string); ok {
			result.concurrency = value
		}
	}
	return result
}

func wrapActivityWithPolicy(fn Activity, policy activityRuntimePolicy) Activity {
	invoke := func(ctx context.Context, input map[string]any) (map[string]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		attemptCtx := ctx
		cancel := func() {}
		if policy.timeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, policy.timeout)
		}
		result, err := fn(attemptCtx, input)
		attemptErr := attemptCtx.Err()
		cancel()
		if err == nil {
			return result, nil
		}
		if parentErr := ctx.Err(); parentErr != nil {
			return nil, parentErr
		}
		if errors.Is(attemptErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("activity timed out after %s: %w", policy.timeout, context.DeadlineExceeded)
		}
		return nil, err
	}

	switch policy.concurrency {
	case "once":
		var mu sync.Mutex
		return func(ctx context.Context, input map[string]any) (map[string]any, error) {
			mu.Lock()
			defer mu.Unlock()
			return invoke(ctx, input)
		}
	default:
		return invoke
	}
}
