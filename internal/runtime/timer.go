package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/lang"
)

// TimerSchedule is one executable timer trigger resolved for an execution.
type TimerSchedule struct {
	Rule       string
	Expression string
	DueAt      time.Time
	Key        string
}

// TimerExecutionSource returns execution IDs currently owned by this process
// for timer scheduling. Distributed ownership remains an application concern.
type TimerExecutionSource func(ctx context.Context) ([]string, error)

// TimerWorkerOptions controls the wall-clock timer polling loop.
type TimerWorkerOptions struct {
	PollInterval time.Duration
	ErrorBuffer  int
}

// NextTimer returns the earliest not-yet-fired timer for one execution.
func (e *Engine) NextTimer(ctx context.Context, executionID string) (*TimerSchedule, error) {
	execution, err := e.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	e.prepareExecution(execution)
	history, err := e.store.ListHistory(ctx, executionID)
	if err != nil {
		return nil, err
	}
	fired := firedTimerKeys(history)
	schedules, err := e.timerSchedules(execution)
	if err != nil {
		return nil, err
	}
	for _, schedule := range schedules {
		if _, done := fired[schedule.Key]; done {
			continue
		}
		copy := schedule
		return &copy, nil
	}
	return nil, nil
}

// RunDueTimers evaluates all timer triggers due at or before now for one
// execution. Firing records and rule effects are committed atomically when the
// configured store supports transactions.
func (e *Engine) RunDueTimers(ctx context.Context, executionID string, now time.Time) (int, error) {
	if now.IsZero() {
		now = e.now()
	} else {
		now = now.UTC()
	}
	firedCount := 0
	err := e.withStoreTransaction(ctx, func(working *Engine) error {
		execution, err := working.store.GetExecution(ctx, executionID)
		if err != nil {
			return err
		}
		working.prepareExecution(execution)
		history, err := working.store.ListHistory(ctx, executionID)
		if err != nil {
			return err
		}
		alreadyFired := firedTimerKeys(history)
		schedules, err := working.timerSchedules(execution)
		if err != nil {
			return err
		}

		queue := make([]string, 0)
		for _, schedule := range schedules {
			if schedule.DueAt.After(now) {
				break
			}
			if _, done := alreadyFired[schedule.Key]; done {
				continue
			}
			if err := working.store.AppendHistory(ctx, executionID, "TimerFired", map[string]any{
				"rule":       schedule.Rule,
				"expression": schedule.Expression,
				"dueAt":      schedule.DueAt.Format(time.RFC3339Nano),
				"key":        schedule.Key,
				"firedAt":    now.Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			alreadyFired[schedule.Key] = struct{}{}
			queue = append(queue, schedule.Rule)
			firedCount++
		}
		if firedCount == 0 {
			return nil
		}

		execution.Status = StatusRunning
		if err := working.processRules(ctx, execution, working.ruleQueue(queue), evalEnv{
			execution: execution,
			changed:   map[string]struct{}{},
		}); err != nil {
			return diag.Error{
				Code:    "AX514",
				Kind:    "runtime",
				Entity:  executionID,
				Message: fmt.Sprintf("timer rule processing failed: %v", err),
				Hint:    "Inspect the due timer rules and claims. TimerFired is rolled back with the transaction when possible.",
				Cause:   err,
			}
		}
		execution.Status = StatusWaiting
		return working.store.SaveExecution(ctx, execution)
	})
	if err != nil {
		return 0, err
	}
	return firedCount, nil
}

// StartTimerWorker runs due timers for execution IDs supplied by source until
// ctx is canceled. source should return only executions currently owned by this
// process/worker.
func (e *Engine) StartTimerWorker(ctx context.Context, source TimerExecutionSource, options TimerWorkerOptions) <-chan error {
	buffer := options.ErrorBuffer
	if buffer <= 0 {
		buffer = 16
	}
	errorsCh := make(chan error, buffer)
	interval := options.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	go func() {
		defer close(errorsCh)
		if source == nil {
			sendTimerWorkerError(errorsCh, diag.Error{
				Code:    "AX512",
				Kind:    "config",
				Message: "timer worker execution source is required",
				Hint:    "Pass a function that returns execution IDs currently owned by this process.",
			})
			return
		}

		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}

			ids, err := source(ctx)
			if err != nil {
				sendTimerWorkerError(errorsCh, fmt.Errorf("timer execution source: %w", err))
			} else {
				sort.Strings(ids)
				ids = uniqueStrings(ids)
				now := e.now()
				for _, executionID := range ids {
					if executionID == "" {
						continue
					}
					if _, err := e.RunDueTimers(ctx, executionID, now); err != nil {
						sendTimerWorkerError(errorsCh, fmt.Errorf("timer execution %s: %w", executionID, err))
					}
				}
			}

			timer.Reset(interval)
		}
	}()
	return errorsCh
}

func (e *Engine) timerSchedules(execution *Execution) ([]TimerSchedule, error) {
	if e == nil || e.module == nil || execution == nil {
		return nil, nil
	}
	var schedules []TimerSchedule
	ruleNames := make([]string, 0, len(e.module.Rules))
	for name := range e.module.Rules {
		ruleNames = append(ruleNames, name)
	}
	sort.Strings(ruleNames)

	for _, ruleName := range ruleNames {
		rule := e.module.Rules[ruleName]
		for _, trigger := range rule.Triggers {
			if trigger.Kind != lang.TriggerTimer {
				continue
			}
			dueAt, ready, err := e.resolveTimerExpression(execution, trigger.Target)
			if err != nil {
				return nil, diag.Error{
					Code:    "AX512",
					Kind:    "runtime",
					Entity:  ruleName,
					Line:    rule.Line,
					Message: fmt.Sprintf("invalid timer %q: %v", trigger.Target, err),
					Hint:    "Use timer(Context.deadline) or timer(<duration> after Context.timeField) with an RFC3339 Time/String value.",
					Cause:   err,
				}
			}
			if !ready {
				continue
			}
			schedules = append(schedules, TimerSchedule{
				Rule:       ruleName,
				Expression: trigger.Target,
				DueAt:      dueAt.UTC(),
				Key:        timerScheduleKey(ruleName, trigger.Target, dueAt),
			})
		}
	}
	sort.SliceStable(schedules, func(i, j int) bool {
		if schedules[i].DueAt.Equal(schedules[j].DueAt) {
			if schedules[i].Rule == schedules[j].Rule {
				return schedules[i].Expression < schedules[j].Expression
			}
			return schedules[i].Rule < schedules[j].Rule
		}
		return schedules[i].DueAt.Before(schedules[j].DueAt)
	})
	return schedules, nil
}

func (e *Engine) resolveTimerExpression(execution *Execution, raw string) (time.Time, bool, error) {
	expression := strings.TrimSpace(raw)
	if strings.HasPrefix(expression, "timer(") && strings.HasSuffix(expression, ")") {
		expression = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expression, "timer("), ")"))
	}
	if expression == "" {
		return time.Time{}, false, fmt.Errorf("timer expression is empty")
	}

	if before, after, ok := strings.Cut(expression, " after "); ok {
		delay, err := time.ParseDuration(strings.TrimSpace(before))
		if err != nil {
			return time.Time{}, false, fmt.Errorf("parse timer delay %q: %w", strings.TrimSpace(before), err)
		}
		base, ready, err := e.resolveTimerReference(execution, strings.TrimSpace(after))
		if err != nil || !ready {
			return time.Time{}, ready, err
		}
		return base.Add(delay).UTC(), true, nil
	}

	reference := strings.TrimSpace(strings.TrimPrefix(expression, "at "))
	return e.resolveTimerReference(execution, reference)
}

func (e *Engine) resolveTimerReference(execution *Execution, reference string) (time.Time, bool, error) {
	if reference == "runtime.createdAt" {
		if execution.CreatedAt.IsZero() {
			return time.Time{}, false, nil
		}
		return execution.CreatedAt.UTC(), true, nil
	}
	if reference == "runtime.updatedAt" {
		if execution.UpdatedAt.IsZero() {
			return time.Time{}, false, nil
		}
		return execution.UpdatedAt.UTC(), true, nil
	}
	if _, ok := e.module.Symbols[reference]; !ok {
		return time.Time{}, false, fmt.Errorf("unknown timer reference %s", reference)
	}
	value := resolveRef(reference, evalEnv{execution: execution})
	if value == nil {
		return time.Time{}, false, nil
	}
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return time.Time{}, false, nil
		}
		return typed.UTC(), true, nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(typed))
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(typed))
		}
		if err != nil {
			return time.Time{}, false, fmt.Errorf("timer reference %s must contain RFC3339 time: %w", reference, err)
		}
		return parsed.UTC(), true, nil
	default:
		return time.Time{}, false, fmt.Errorf("timer reference %s has unsupported value type %T", reference, value)
	}
}

func firedTimerKeys(history []HistoryEntry) map[string]struct{} {
	out := map[string]struct{}{}
	for _, entry := range history {
		if entry.Type != "TimerFired" {
			continue
		}
		key, _ := entry.Payload["key"].(string)
		if key != "" {
			out[key] = struct{}{}
		}
	}
	return out
}

func timerScheduleKey(ruleName, expression string, dueAt time.Time) string {
	sum := sha256.Sum256([]byte(ruleName + "\x00" + strings.TrimSpace(expression) + "\x00" + dueAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:])
}

func sendTimerWorkerError(ch chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	var previous string
	for index, value := range values {
		if index > 0 && value == previous {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}
