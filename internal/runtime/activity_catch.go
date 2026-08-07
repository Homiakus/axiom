package runtime

import (
	"context"
	"fmt"

	"github.com/Homiakus/axiom/internal/diag"
)

func (e *Engine) activityCatchTarget(activityName string, runErr error) (signalName string, errorCode string, ok bool) {
	if e == nil || e.module == nil {
		return "", "", false
	}
	activity, exists := e.module.Activities[activityName]
	if !exists || activity.Policy == "" {
		return "", "", false
	}
	policy, exists := e.module.Policies[activity.Policy]
	if !exists || len(policy.Catches) == 0 {
		return "", "", false
	}

	if code, coded := activityErrorCode(runErr); coded {
		if target := policy.Catches[code]; target != "" {
			return target, code, true
		}
		errorCode = code
	}
	if target := policy.Catches["*"]; target != "" {
		return target, errorCode, true
	}
	return "", errorCode, false
}

func (e *Engine) handleActivityCatch(
	ctx context.Context,
	execution *Execution,
	task *ActivityTask,
	activityErr error,
	runErr error,
) (bool, error) {
	signalName, errorCode, caught := e.activityCatchTarget(task.ActivityName, runErr)
	if !caught {
		return false, nil
	}

	payload := map[string]any{
		"activity":    task.ActivityName,
		"rule":        task.RuleName,
		"taskId":      task.ID,
		"errorCode":   errorCode,
		"error":       runErr.Error(),
		"attempt":     task.Attempt,
		"maxAttempts": task.MaxAttempts,
	}
	if err := e.store.AppendHistory(ctx, execution.ID, "ActivityFailed", map[string]any{
		"activity": task.ActivityName,
		"rule":     task.RuleName,
		"task":     task.ID,
		"error":    activityErr.Error(),
		"code":     errorCode,
		"caught":   true,
	}); err != nil {
		return true, err
	}
	if err := e.store.AppendHistory(ctx, execution.ID, "ActivityCaught", map[string]any{
		"activity": task.ActivityName,
		"rule":     task.RuleName,
		"task":     task.ID,
		"code":     errorCode,
		"signal":   signalName,
	}); err != nil {
		return true, err
	}
	if err := e.store.AppendHistory(ctx, execution.ID, "SignalReceived", map[string]any{
		"signal":  signalName,
		"payload": payload,
		"source":  "policy.catch",
	}); err != nil {
		return true, err
	}

	execution.Status = StatusRunning
	queue := e.ruleQueueForSignal(signalName)
	if err := e.processRules(ctx, execution, queue, evalEnv{
		execution: execution,
		signal:    payload,
		changed:   map[string]struct{}{},
	}); err != nil {
		return true, diag.Error{
			Code:    "AX511",
			Kind:    "runtime",
			Entity:  signalName,
			Message: fmt.Sprintf("policy catch signal %s failed: %v", signalName, err),
			Hint:    "Inspect the catch target rules and claims. The catch transaction was rolled back.",
			Cause:   err,
		}
	}
	execution.Status = StatusWaiting
	if err := e.store.SaveExecution(ctx, execution); err != nil {
		return true, err
	}
	return true, nil
}
