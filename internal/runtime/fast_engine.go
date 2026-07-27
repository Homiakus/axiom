package runtime

import "context"

type TaskDedupStore interface {
	FindTask(ctx context.Context, executionID string, ruleName string, activityName string, idempotencyKey string) (*ActivityTask, error)
	NextTaskSeq(ctx context.Context, executionID string) (int, error)
}

func (e *Engine) recomputeFast(execution *Execution, changed map[string]struct{}) ([]string, error) {
	if e.fast == nil {
		if err := e.recompute(execution, changed); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return e.fast.recompute(execution, changed)
}

func (e *Engine) checkClaimsFast(execution *Execution, changedAtoms []string) error {
	if e.fast == nil {
		return e.checkClaims(execution)
	}
	return e.fast.checkClaims(execution, changedAtoms)
}

func (e *Engine) ruleReadyFast(ruleName string, env evalEnv) (bool, string, error) {
	if e.fast == nil {
		rule := e.module.Rules[ruleName]
		whenOK, err := evalAll(rule.When, env)
		if err != nil || !whenOK {
			return whenOK, "when", err
		}
		requireOK, err := evalAll(rule.Require, env)
		if err != nil || !requireOK {
			return requireOK, "require", err
		}
		return true, "", nil
	}
	return e.fast.ruleReady(ruleName, env)
}

func (e *Engine) ruleQueueForSignal(signal string) []string {
	if e.fast == nil {
		return e.ruleQueue(e.module.Indexes.SignalIndex[signal])
	}
	return e.ruleQueue(e.fast.ruleQueueForSignal(signal))
}

func (e *Engine) rulesForChangedFast(changed []string, changedAtoms []string) []string {
	if e.fast == nil {
		return e.rulesForChanged(changed)
	}
	return e.ruleQueue(e.fast.rulesForChanged(changed, changedAtoms))
}

func (e *Engine) cloneFastState(execution *Execution) ExecutionState {
	if e.fast == nil || execution == nil {
		return ExecutionState{}
	}
	return cloneExecutionState(execution.RuntimeState)
}

func (e *Engine) restoreFastState(execution *Execution, state ExecutionState) {
	if e.fast == nil || execution == nil {
		return
	}
	execution.RuntimeState = cloneExecutionState(state)
}
