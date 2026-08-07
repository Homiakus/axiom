package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/lang"
)

var ErrExecutionNotFound = errors.New("execution not found")

func (e *Engine) Start(ctx context.Context, executionID string, initialContext map[string]any) error {
	return e.withStoreTransaction(ctx, func(working *Engine) error {
		return working.start(ctx, executionID, initialContext)
	})
}

func (e *Engine) start(ctx context.Context, executionID string, initialContext map[string]any) error {
	if executionID == "" {
		return diag.Error{Code: "AX400", Kind: "runtime", Message: "execution id is required", Hint: "Pass a non-empty execution ID to Start."}
	}
	now := e.now()
	execution := &Execution{
		ID:              executionID,
		Domain:          e.module.Domain,
		Status:          StatusStarted,
		Context:         map[string]map[string]any{},
		Computed:        map[string]any{},
		Facts:           map[string]FactValue{},
		ModuleHash:      e.module.CompiledHash,
		CompilerVersion: e.module.CompilerVersion,
		PlanVersion:     e.module.PlanVersion,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := e.applyDefaults(execution); err != nil {
		return err
	}
	e.syncAllFieldState(execution)
	changed, err := e.applyPatch(execution, initialContext)
	if err != nil {
		return err
	}
	changedAtoms, err := e.recomputeFast(execution, nil)
	if err != nil {
		return err
	}
	if err := e.checkClaimsFast(execution, changedAtoms); err != nil {
		execution.Status = StatusFailed
		return err
	}
	if err := e.store.CreateExecution(ctx, execution); err != nil {
		return fmt.Errorf("create execution: %w", err)
	}
	if err := e.store.AppendHistory(ctx, executionID, "ExecutionStarted", map[string]any{
		"executionID":     executionID,
		"domain":          e.module.Domain,
		"moduleHash":      e.module.CompiledHash,
		"compilerVersion": e.module.CompilerVersion,
		"planVersion":     e.module.PlanVersion,
		"createdAt":       now.Format(time.RFC3339Nano),
	}); err != nil {
		return err
	}
	if len(changed) > 0 {
		if err := e.store.AppendHistory(ctx, executionID, "ContextPatched", map[string]any{"changed": changed, "values": e.changedValues(execution, changed)}); err != nil {
			return err
		}
	}
	return e.store.SaveExecution(ctx, execution)
}

func (e *Engine) Signal(ctx context.Context, executionID string, signalName string, payload map[string]any) error {
	return e.withStoreTransaction(ctx, func(working *Engine) error {
		return working.signal(ctx, executionID, signalName, payload)
	})
}

func (e *Engine) signal(ctx context.Context, executionID string, signalName string, payload map[string]any) error {
	if _, ok := e.module.Signals[signalName]; !ok {
		return diag.Error{Code: "AX401", Kind: "runtime", Entity: signalName, Message: fmt.Sprintf("unknown signal %s", signalName), Hint: "Use a signal declared in the .axm module."}
	}
	execution, err := e.store.GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	e.prepareExecution(execution)
	execution.Status = StatusRunning
	if err := e.store.AppendHistory(ctx, executionID, "SignalReceived", map[string]any{"signal": signalName, "payload": payload}); err != nil {
		return err
	}
	queue := e.ruleQueueForSignal(signalName)
	err = e.processRules(ctx, execution, queue, evalEnv{execution: execution, signal: payload, changed: map[string]struct{}{}})
	if err != nil {
		execution.Status = StatusFailed
		_ = e.store.SaveExecution(ctx, execution)
		return err
	}
	execution.Status = StatusWaiting
	return e.store.SaveExecution(ctx, execution)
}

func (e *Engine) Patch(ctx context.Context, executionID string, patch map[string]any) error {
	return e.withStoreTransaction(ctx, func(working *Engine) error {
		return working.patch(ctx, executionID, patch)
	})
}

func (e *Engine) patch(ctx context.Context, executionID string, patch map[string]any) error {
	execution, err := e.store.GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	e.prepareExecution(execution)
	execution.Status = StatusRunning
	changed, err := e.applyPatch(execution, patch)
	if err != nil {
		return err
	}
	if err := e.store.AppendHistory(ctx, executionID, "ContextPatched", map[string]any{"changed": changed, "values": e.changedValues(execution, changed)}); err != nil {
		return err
	}
	changedAtoms, err := e.recomputeFast(execution, changedSet(changed))
	if err != nil {
		execution.Status = StatusFailed
		_ = e.store.SaveExecution(ctx, execution)
		return err
	}
	queue := e.rulesForChangedFast(changed, changedAtoms)
	err = e.processRules(ctx, execution, queue, evalEnv{execution: execution, changed: changedSet(changed)})
	if err != nil {
		execution.Status = StatusFailed
		_ = e.store.SaveExecution(ctx, execution)
		return err
	}
	execution.Status = StatusWaiting
	return e.store.SaveExecution(ctx, execution)
}

func (e *Engine) RunUntilIdle(ctx context.Context, executionID string) error {
	for {
		task, err := e.store.PollTaskWithLease(ctx, executionID, "inline-worker", time.Minute)
		if err != nil {
			return err
		}
		if task == nil {
			return nil
		}
		activity, ok := e.activities[task.ActivityName]
		if !ok {
			return diag.Error{Code: "AX501", Kind: "config", Entity: task.ActivityName, Message: fmt.Sprintf("activity %s is not registered", task.ActivityName), Hint: "Register the Go function with WithActivity or Register before running the engine."}
		}
		result, runErr := activity(ctx, cloneAnyMap(task.Input))
		if err := e.withStoreTransaction(ctx, func(working *Engine) error {
			return working.completeActivity(ctx, executionID, task, result, runErr)
		}); err != nil {
			return err
		}
	}
}

func (e *Engine) completeActivity(ctx context.Context, executionID string, task *ActivityTask, result map[string]any, runErr error) error {
	execution, err := e.store.GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	e.prepareExecution(execution)
	if runErr != nil {
		activityErr := diag.Error{
			Code:    "AX505",
			Kind:    "activity",
			Entity:  task.ActivityName,
			Message: fmt.Sprintf("activity %s failed: %v", task.ActivityName, runErr),
			Hint:    "Inspect the registered Go activity and the input payload recorded in history.",
			Cause:   runErr,
		}
		task.Status = TaskFailed
		task.Error = activityErr.Error()
		task.UpdatedAt = e.now()
		if err := e.store.FailTask(ctx, task.ID, activityErr.Error()); err != nil {
			return err
		}
		if caught, err := e.handleActivityCatch(ctx, execution, task, activityErr, runErr); caught {
			return err
		}
		if err := e.store.AppendHistory(ctx, executionID, "ActivityFailed", map[string]any{"activity": task.ActivityName, "rule": task.RuleName, "error": activityErr.Error()}); err != nil {
			return err
		}
		execution.Status = StatusFailed
		if err := e.store.SaveExecution(ctx, execution); err != nil {
			return err
		}
		return activityErr
	}
	if err := e.validateActivityOutput(task.ActivityName, result); err != nil {
		task.Status = TaskFailed
		task.Error = err.Error()
		task.UpdatedAt = e.now()
		if updateErr := e.store.FailTask(ctx, task.ID, err.Error()); updateErr != nil {
			return updateErr
		}
		if historyErr := e.store.AppendHistory(ctx, executionID, "ActivityFailed", map[string]any{"activity": task.ActivityName, "rule": task.RuleName, "error": err.Error()}); historyErr != nil {
			return historyErr
		}
		execution.Status = StatusFailed
		_ = e.store.SaveExecution(ctx, execution)
		return err
	}
	task.Status = TaskCompleted
	task.Result = cloneAnyMap(result)
	task.UpdatedAt = e.now()
	if err := e.store.CompleteTask(ctx, task.ID, result); err != nil {
		return err
	}
	if err := e.store.AppendHistory(ctx, executionID, "ActivityCompleted", map[string]any{"activity": task.ActivityName, "rule": task.RuleName, "result": result}); err != nil {
		return err
	}
	rule := e.module.Rules[task.RuleName]
	changed, err := e.applyWrites(ctx, execution, rule, evalEnv{execution: execution, output: result, changed: map[string]struct{}{}})
	if err != nil {
		execution.Status = StatusFailed
		_ = e.store.SaveExecution(ctx, execution)
		return err
	}
	changedAtoms, err := e.recomputeFast(execution, changedSet(changed))
	if err != nil {
		execution.Status = StatusFailed
		_ = e.store.SaveExecution(ctx, execution)
		return err
	}
	queue := e.rulesForChangedFast(changed, changedAtoms)
	if err := e.processRules(ctx, execution, queue, evalEnv{execution: execution, changed: changedSet(changed)}); err != nil {
		execution.Status = StatusFailed
		_ = e.store.SaveExecution(ctx, execution)
		return err
	}
	execution.Status = StatusWaiting
	return e.store.SaveExecution(ctx, execution)
}

func (e *Engine) Query(ctx context.Context, executionID string, queryName string) (map[string]any, error) {
	execution, err := e.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	e.prepareExecution(execution)
	if _, err := e.recomputeFast(execution, nil); err != nil {
		return nil, err
	}
	switch queryName {
	case "state":
		return map[string]any{"context": cloneContext(execution.Context), "status": execution.Status}, nil
	case "facts":
		return map[string]any{"facts": cloneFacts(execution.Facts)}, nil
	case "history":
		history, err := e.store.ListHistory(ctx, executionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"history": history}, nil
	case "pendingActivities":
		tasks, err := e.store.ListTasks(ctx, executionID)
		if err != nil {
			return nil, err
		}
		var pending []ActivityTask
		for _, task := range tasks {
			if task.Status == TaskPending || task.Status == TaskRunning {
				pending = append(pending, *task)
			}
		}
		return map[string]any{"pendingActivities": pending}, nil
	case "explain":
		history, err := e.store.ListHistory(ctx, executionID)
		if err != nil {
			return nil, err
		}
		tasks, err := e.store.ListTasks(ctx, executionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"status":            execution.Status,
			"facts":             cloneFacts(execution.Facts),
			"pendingActivities": tasks,
			"history":           history,
		}, nil
	}
	query, ok := e.module.Queries[queryName]
	if !ok {
		return nil, diag.Error{Code: "AX402", Kind: "runtime", Entity: queryName, Message: fmt.Sprintf("unknown query %s", queryName), Hint: "Use a built-in query or a query declared in the .axm module."}
	}
	env := evalEnv{execution: execution}
	result := map[string]any{}
	for _, binding := range query.Return {
		value, err := evalExpr(binding.Expr, env)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", queryName, err)
		}
		result[binding.Name] = value
	}
	return result, nil
}

func (e *Engine) prepareExecution(execution *Execution) {
	if execution == nil {
		return
	}
	if execution.ModuleHash == "" {
		execution.ModuleHash = e.module.CompiledHash
	}
	if execution.CompilerVersion == "" {
		execution.CompilerVersion = e.module.CompilerVersion
	}
	if execution.PlanVersion == "" {
		execution.PlanVersion = e.module.PlanVersion
	}
	e.syncAllFieldState(execution)
	if e.fast != nil {
		ensureExecutionState(execution, len(e.fast.fields), len(e.fast.atoms))
	}
}

func (e *Engine) applyDefaults(execution *Execution) error {
	env := evalEnv{execution: execution}
	for _, contextDecl := range e.module.AST.Contexts {
		fields := map[string]any{}
		execution.Context[contextDecl.Name] = fields
		for _, field := range contextDecl.Fields {
			if field.HasDefault {
				value, err := evalExpr(field.Default, env)
				if err != nil {
					return fmt.Errorf("default %s.%s: %w", contextDecl.Name, field.Name, err)
				}
				if err := e.checkContextValue(contextDecl.Name+"."+field.Name, value); err != nil {
					return fmt.Errorf("default %s.%s: %w", contextDecl.Name, field.Name, err)
				}
				fields[field.Name] = value
				e.syncFieldState(execution, contextDecl.Name+"."+field.Name, value)
				continue
			}
			fields[field.Name] = nil
			e.syncFieldState(execution, contextDecl.Name+"."+field.Name, nil)
		}
	}
	return nil
}

func (e *Engine) processRules(ctx context.Context, execution *Execution, queue []string, env evalEnv) error {
	if e.fast != nil {
		env.fieldIDs = e.fast.fieldIDs
		env.dirty = e.fast.dirtyFromChanged(env.changed)
	}
	changedAtoms, err := e.recomputeFast(execution, env.changed)
	if err != nil {
		return err
	}
	if err := e.checkClaimsFast(execution, changedAtoms); err != nil {
		return err
	}
	ruleQueue := newRuleQueue(e.fast, queue)
	seenInTurn := make([]int, len(e.fast.rules))
	evalSummary := ruleEvalSummary{Skipped: map[string]int{}}
	for steps := 0; !ruleQueue.Empty(); steps++ {
		if steps > e.maxSteps {
			return diag.Error{Code: "AX308", Kind: "runtime", Message: "non-convergent rule loop", Hint: "Check rules that repeatedly write fields observed by other rules."}
		}
		ruleID, ok := ruleQueue.Pop()
		if !ok {
			break
		}
		ruleName := e.fast.rules[ruleID].name
		rule, ok := e.module.Rules[ruleName]
		if !ok {
			continue
		}
		seenInTurn[ruleID]++
		if seenInTurn[ruleID] > e.maxSteps/2 {
			return diag.Error{Code: "AX308", Kind: "runtime", Entity: ruleName, Message: fmt.Sprintf("non-convergent rule loop: %s", ruleName), Hint: "Check this rule and its changed(...) dependencies."}
		}
		env.execution = execution
		ready, reason, err := e.ruleReadyFast(rule.Name, env)
		if err != nil {
			return fmt.Errorf("rule %s %s: %w", rule.Name, reason, err)
		}
		if !ready {
			evalSummary.TotalCandidates++
			evalSummary.Skipped[reason]++
			evalSummary.sampleSkipped(rule.Name)
			if e.traceLevel == TraceFull {
				if err := e.store.AppendHistory(ctx, execution.ID, "RuleSkipped", map[string]any{"rule": rule.Name, "reason": reason}); err != nil {
					return err
				}
			}
			continue
		}
		evalSummary.TotalCandidates++
		evalSummary.Scheduled = append(evalSummary.Scheduled, rule.Name)
		if e.traceLevel == TraceFull {
			if err := e.store.AppendHistory(ctx, execution.ID, "RuleScheduled", map[string]any{"rule": rule.Name}); err != nil {
				return err
			}
		}
		if rule.Run != "" {
			if err := e.scheduleActivity(ctx, execution, rule, env); err != nil {
				return err
			}
			continue
		}
		changed, err := e.applyWrites(ctx, execution, rule, env)
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			continue
		}
		env.changed = changedSet(changed)
		if e.fast != nil {
			env.dirty = e.fast.dirtyFromChanged(env.changed)
		}
		changedAtoms, err = e.recomputeFast(execution, env.changed)
		if err != nil {
			return err
		}
		if err := e.checkClaimsFast(execution, changedAtoms); err != nil {
			return err
		}
		ruleQueue.PushNames(e.rulesForChangedFast(changed, changedAtoms))
	}
	if e.traceLevel == TraceAggregate && evalSummary.TotalCandidates > 0 {
		if err := e.store.AppendHistory(ctx, execution.ID, "RulesEvaluated", evalSummary.payload()); err != nil {
			return err
		}
	}
	if err := e.checkClaimsFast(execution, nil); err != nil {
		return err
	}
	if err := e.store.AppendHistory(ctx, execution.ID, "ExecutionReachedFixpoint", map[string]any{}); err != nil {
		return err
	}
	return e.store.SaveExecution(ctx, execution)
}

type ruleEvalSummary struct {
	TotalCandidates int
	Scheduled       []string
	Skipped         map[string]int
	SkippedSample   []string
}

func (s *ruleEvalSummary) sampleSkipped(ruleName string) {
	if len(s.SkippedSample) >= 20 {
		return
	}
	s.SkippedSample = append(s.SkippedSample, ruleName)
}

func (s ruleEvalSummary) payload() map[string]any {
	return map[string]any{
		"totalCandidates": s.TotalCandidates,
		"scheduled":       append([]string{}, s.Scheduled...),
		"skipped":         s.Skipped,
		"skippedSample":   append([]string{}, s.SkippedSample...),
	}
}

func (e *Engine) scheduleActivity(ctx context.Context, execution *Execution, rule lang.RuleDecl, env evalEnv) error {
	activity, ok := e.module.Activities[rule.Run]
	if !ok {
		return diag.Error{Code: "AX501", Kind: "config", Entity: rule.Run, Message: fmt.Sprintf("unknown activity %s", rule.Run), Hint: "Declare the activity in .axm and register its Go function."}
	}
	input := map[string]any{}
	for _, binding := range activity.Input {
		value, err := evalExpr(binding.Expr, env)
		if err != nil {
			return fmt.Errorf("activity %s input %s: %w", activity.Name, binding.Name, err)
		}
		input[binding.Name] = value
	}
	var key string
	if activity.IdempotencyKey != nil {
		value, err := evalExpr(activity.IdempotencyKey, env)
		if err != nil {
			return fmt.Errorf("activity %s idempotency key: %w", activity.Name, err)
		}
		key = fmt.Sprint(value)
	}
	taskSeq := 1
	if indexed, ok := e.store.(TaskDedupStore); ok {
		task, err := indexed.FindTask(ctx, execution.ID, rule.Name, activity.Name, key)
		if err != nil {
			return err
		}
		if task != nil && task.Status != TaskFailed {
			return e.store.AppendHistory(ctx, execution.ID, "ActivityDeduplicated", map[string]any{"activity": activity.Name, "rule": rule.Name, "task": task.ID})
		}
		taskSeq, err = indexed.NextTaskSeq(ctx, execution.ID)
		if err != nil {
			return err
		}
	} else {
		tasks, err := e.store.ListTasks(ctx, execution.ID)
		if err != nil {
			return err
		}
		taskSeq = len(tasks) + 1
		for _, task := range tasks {
			if task.RuleName == rule.Name && task.ActivityName == activity.Name && task.IdempotencyKey == key && task.Status != TaskFailed {
				return e.store.AppendHistory(ctx, execution.ID, "ActivityDeduplicated", map[string]any{"activity": activity.Name, "rule": rule.Name, "task": task.ID})
			}
		}
	}
	task := &ActivityTask{
		ID:             taskID(execution.ID, rule.Name, activity.Name, taskSeq),
		ExecutionID:    execution.ID,
		RuleName:       rule.Name,
		ActivityName:   activity.Name,
		Input:          input,
		IdempotencyKey: key,
		Status:         TaskPending,
		MaxAttempts:    e.maxAttemptsForActivity(activity),
		CreatedAt:      e.now(),
		UpdatedAt:      e.now(),
	}
	if err := e.store.AppendHistory(ctx, execution.ID, "ActivityScheduled", map[string]any{"activity": activity.Name, "rule": rule.Name, "task": task.ID, "input": input}); err != nil {
		return err
	}
	return e.store.EnqueueTask(ctx, task)
}

func (e *Engine) maxAttemptsForActivity(activity lang.ActivityDecl) int {
	maxAttempts := 1
	if activity.Policy == "" {
		return maxAttempts
	}
	policy, ok := e.module.Policies[activity.Policy]
	if !ok {
		return maxAttempts
	}
	retry, ok := policy.Entries["retry"]
	if !ok || retry == nil || retry.Kind != lang.ExprLiteral {
		return maxAttempts
	}
	if n, ok := retry.Value.(int); ok && n >= 0 {
		return n + 1
	}
	return maxAttempts
}

func (e *Engine) applyWrites(ctx context.Context, execution *Execution, rule lang.RuleDecl, env evalEnv) ([]string, error) {
	if err := e.checkClaimsFast(execution, nil); err != nil {
		return nil, err
	}
	rollback := contextRollback{}
	changed := make([]string, 0, len(rule.Writes))
	writes := make(map[string]any, len(rule.Writes))
	for _, write := range rule.Writes {
		value, err := evalExpr(write.Expr, env)
		if err != nil {
			rollback.restore(e, execution)
			return nil, fmt.Errorf("rule %s write %s: %w", rule.Name, write.Name, err)
		}
		if err := e.checkContextValue(write.Name, value); err != nil {
			rollback.restore(e, execution)
			return nil, fmt.Errorf("rule %s write %s: %w", rule.Name, write.Name, err)
		}
		rollback.capture(execution, write.Name)
		if e.setContextValue(execution, write.Name, value) {
			changed = append(changed, write.Name)
			writes[write.Name] = value
		}
	}
	sort.Strings(changed)
	if len(changed) > 0 {
		beforeComputed := cloneAnyMap(execution.Computed)
		beforeFacts := cloneFacts(execution.Facts)
		beforeFast := e.cloneFastState(execution)
		changedAtoms, err := e.recomputeFast(execution, changedSet(changed))
		if err != nil {
			rollback.restore(e, execution)
			execution.Computed = beforeComputed
			execution.Facts = beforeFacts
			e.restoreFastState(execution, beforeFast)
			return nil, err
		}
		if err := e.checkClaimsFast(execution, changedAtoms); err != nil {
			rollback.restore(e, execution)
			execution.Computed = beforeComputed
			execution.Facts = beforeFacts
			e.restoreFastState(execution, beforeFast)
			return nil, err
		}
	}
	if err := e.store.AppendHistory(ctx, execution.ID, "WriteApplied", map[string]any{"rule": rule.Name, "writes": writes, "changed": changed, "values": e.changedValues(execution, changed)}); err != nil {
		return nil, err
	}
	return changed, nil
}

type contextRollback struct {
	fields []contextFieldSnapshot
	seen   map[string]struct{}
}

type contextFieldSnapshot struct {
	contextName string
	fieldName   string
	value       any
	exists      bool
}

func (r *contextRollback) capture(execution *Execution, target string) {
	field := contextFieldName(target)
	if r.seen == nil {
		r.seen = map[string]struct{}{}
	}
	if _, ok := r.seen[field]; ok {
		return
	}
	r.seen[field] = struct{}{}
	parts := strings.Split(field, ".")
	if len(parts) < 2 {
		return
	}
	fields := execution.Context[parts[0]]
	value, exists := fields[parts[1]]
	r.fields = append(r.fields, contextFieldSnapshot{
		contextName: parts[0],
		fieldName:   parts[1],
		value:       cloneAny(value),
		exists:      exists,
	})
}

func (r *contextRollback) restore(e *Engine, execution *Execution) {
	for i := len(r.fields) - 1; i >= 0; i-- {
		snapshot := r.fields[i]
		if execution.Context[snapshot.contextName] == nil {
			execution.Context[snapshot.contextName] = map[string]any{}
		}
		field := snapshot.contextName + "." + snapshot.fieldName
		if snapshot.exists {
			execution.Context[snapshot.contextName][snapshot.fieldName] = cloneAny(snapshot.value)
			e.syncFieldState(execution, field, snapshot.value)
			continue
		}
		delete(execution.Context[snapshot.contextName], snapshot.fieldName)
		e.syncFieldState(execution, field, nil)
	}
}

func (e *Engine) recompute(execution *Execution, changed map[string]struct{}) error {
	env := evalEnv{execution: execution, changed: changed}
	for i := 0; i < len(e.module.AST.Computeds)+1; i++ {
		changedAny := false
		for _, computed := range e.module.AST.Computeds {
			value, err := evalExpr(computed.Expr, env)
			if err != nil {
				return fmt.Errorf("computed %s: %w", computed.Name, err)
			}
			if !reflect.DeepEqual(execution.Computed[computed.Name], value) {
				execution.Computed[computed.Name] = value
				changedAny = true
			}
		}
		if !changedAny {
			break
		}
	}
	for i := 0; i < len(e.module.AST.Facts)+1; i++ {
		changedAny := false
		for _, fact := range e.module.AST.Facts {
			ok, err := evalAll(fact.When, env)
			if err != nil {
				return fmt.Errorf("fact %s: %w", fact.Name, err)
			}
			next := FactValue{True: ok, Exposed: map[string]any{}}
			if ok {
				for _, expose := range fact.Expose {
					value, err := evalExpr(expose.Expr, env)
					if err != nil {
						return fmt.Errorf("fact %s expose %s: %w", fact.Name, expose.Name, err)
					}
					next.Exposed[expose.Name] = value
				}
			}
			if !reflect.DeepEqual(execution.Facts[fact.Name], next) {
				execution.Facts[fact.Name] = next
				changedAny = true
			}
		}
		if !changedAny {
			break
		}
	}
	return nil
}

func (e *Engine) checkClaims(execution *Execution) error {
	env := evalEnv{execution: execution}
	for _, claim := range e.module.AST.Claims {
		ok, err := evalAll(claim.Always, env)
		if err != nil {
			return fmt.Errorf("claim %s: %w", claim.Name, err)
		}
		if !ok {
			return diag.Error{Code: "AX403", Kind: "runtime", Entity: claim.Name, Message: fmt.Sprintf("claim failed: %s", claim.Name), Hint: "Inspect the rule write or patch that made the claim false."}
		}
	}
	return nil
}

func (e *Engine) rulesForChanged(changed []string) []string {
	var rules []string
	for _, field := range changed {
		rules = append(rules, e.module.Indexes.ChangedIndex[field]...)
	}
	return e.ruleQueue(rules)
}

func (e *Engine) ruleQueue(rules []string) []string {
	seen := map[string]struct{}{}
	var queue []string
	for _, rule := range rules {
		if _, ok := seen[rule]; ok {
			continue
		}
		seen[rule] = struct{}{}
		queue = append(queue, rule)
	}
	return queue
}

func (e *Engine) applyPatch(execution *Execution, patch map[string]any) ([]string, error) {
	var changed []string
	for key, value := range patch {
		if strings.Contains(key, ".") {
			if err := e.checkContextValue(key, value); err != nil {
				return nil, err
			}
			if e.setContextValue(execution, key, value) {
				changed = append(changed, contextFieldName(key))
			}
			continue
		}
		nested, ok := value.(map[string]any)
		if !ok {
			return nil, diag.Error{Code: "AX404", Kind: "runtime", Entity: key, Message: fmt.Sprintf("context patch %s must be an object or qualified field", key), Hint: "Use either Context.field as the key or pass Context: map[string]any{...}."}
		}
		for field, fieldValue := range nested {
			qualified := key + "." + field
			if err := e.checkContextValue(qualified, fieldValue); err != nil {
				return nil, err
			}
			if e.setContextValue(execution, qualified, fieldValue) {
				changed = append(changed, qualified)
			}
		}
	}
	sort.Strings(changed)
	return unique(changed), nil
}

func (e *Engine) validateActivityOutput(activityName string, output map[string]any) error {
	activity, ok := e.module.Activities[activityName]
	if !ok {
		return diag.Error{Code: "AX501", Kind: "config", Entity: activityName, Message: fmt.Sprintf("unknown activity %s", activityName)}
	}
	for _, field := range activity.Output {
		value, exists := output[field.Name]
		if !exists {
			return diag.Error{
				Code:    "AX503",
				Kind:    "activity",
				Entity:  activityName + "." + field.Name,
				Message: fmt.Sprintf("activity %s output is missing field %s", activityName, field.Name),
				Hint:    "Return every field declared in the activity output block.",
			}
		}
		if !valueMatchesType(value, field.Type) {
			return diag.Error{
				Code:    "AX504",
				Kind:    "activity",
				Entity:  activityName + "." + field.Name,
				Message: fmt.Sprintf("activity %s output field %s has type %T, want %s", activityName, field.Name, value, field.Type),
				Hint:    "Make the Go activity output match the .axm output type.",
			}
	}
	return nil
}

func (e *Engine) setContextValue(execution *Execution, target string, value any) bool {
	changed := setContextValue(execution, target, value)
	if changed {
		field := contextFieldName(target)
		e.syncFieldState(execution, field, resolveRef(field, evalEnv{execution: execution}))
	}
	return changed
}

func setContextValue(execution *Execution, target string, value any) bool {
	firstDot := strings.IndexByte(target, '.')
	if firstDot < 0 {
		return false
	}
	contextName := target[:firstDot]
	rest := target[firstDot+1:]
	if execution.Context[contextName] == nil {
		execution.Context[contextName] = map[string]any{}
	}
	secondDot := strings.IndexByte(rest, '.')
	if secondDot < 0 {
		fieldName := rest
		current := execution.Context[contextName][fieldName]
		if typedEqual(current, value) {
			return false
		}
		execution.Context[contextName][fieldName] = value
		return true
	}
	fieldName := rest[:secondDot]
	current := execution.Context[contextName][fieldName]
	root, ok := current.(map[string]any)
	if !ok || root == nil {
		root = map[string]any{}
	} else {
		root = cloneAnyMap(root)
	}
	parts := strings.Split(rest[secondDot+1:], ".")
	setNested(root, parts, value)
	if typedEqual(current, root) {
		return false
	}
	execution.Context[contextName][fieldName] = root
	return true
}

func (e *Engine) syncFieldState(execution *Execution, field string, value any) {
	if e == nil || e.fast == nil || execution == nil {
		return
	}
	fieldID, ok := e.fast.fieldID(field)
	if !ok {
		return
	}
	ensureExecutionState(execution, len(e.fast.fields), len(e.fast.atoms))
	present := bitset(execution.RuntimeState.Present)
	boolValues := bitset(execution.RuntimeState.BoolValues)
	if value == nil {
		present.clear(fieldID)
		boolValues.clear(fieldID)
		delete(execution.RuntimeState.Values, uint32(fieldID))
		return
	}
	present.set(fieldID)
	if b, ok := value.(bool); ok {
		if b {
			boolValues.set(fieldID)
		} else {
			boolValues.clear(fieldID)
		}
		delete(execution.RuntimeState.Values, uint32(fieldID))
		return
	}
	boolValues.clear(fieldID)
	execution.RuntimeState.Values[uint32(fieldID)] = valueOf(value)
}

func (e *Engine) syncAllFieldState(execution *Execution) {
	if e == nil || e.fast == nil || execution == nil {
		return
	}
	ensureExecutionState(execution, len(e.fast.fields), len(e.fast.atoms))
	for contextName, fields := range execution.Context {
		for fieldName, value := range fields {
			e.syncFieldState(execution, contextName+"."+fieldName, value)
		}
	}
}

func setNested(root map[string]any, path []string, value any) {
	if len(path) == 1 {
		root[path[0]] = value
		return
	}
	next, ok := root[path[0]].(map[string]any)
	if !ok || next == nil {
		next = map[string]any{}
		root[path[0]] = next
	}
	setNested(next, path[1:], value)
}

func contextFieldName(ref string) string {
	first := strings.IndexByte(ref, '.')
	if first < 0 {
		return ref
	}
	second := strings.IndexByte(ref[first+1:], '.')
	if second < 0 {
		return ref
	}
	return ref[:first+1+second]
}

func changedSet(changed []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, value := range changed {
		set[value] = struct{}{}
	}
	return set
}

func unique(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (e *Engine) changedValues(execution *Execution, changed []string) map[string]any {
	values := map[string]any{}
	for _, field := range changed {
		values[field] = cloneAny(resolveRef(field, evalEnv{execution: execution}))
	}
	return values
}

func taskID(executionID string, ruleName string, activityName string, seq int) string {
	return fmt.Sprintf("%s:%s:%s:%d", executionID, ruleName, activityName, seq)
}
