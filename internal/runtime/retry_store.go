package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/compiler"
	"github.com/Homiakus/axiom/internal/lang"
)

const (
	defaultRetryBackoff = 100 * time.Millisecond
	maxRetryBackoff     = 30 * time.Second
)

// ErrRetryScheduled indicates that the current activity attempt failed, but
// the task was durably returned to pending state and may be retried later.
var ErrRetryScheduled = errors.New("activity retry scheduled")

// RetryScheduledError describes a persisted retry checkpoint. Low-level users
// of Engine.RunUntilIdle can inspect this error; the higher-level Run API waits
// for the next attempt automatically.
type RetryScheduledError struct {
	TaskID        string
	ExecutionID   string
	ActivityName  string
	Attempt       int
	MaxAttempts   int
	NextAttemptAt time.Time
}

func (e *RetryScheduledError) Error() string {
	if e == nil {
		return ErrRetryScheduled.Error()
	}
	return fmt.Sprintf(
		"%s: activity=%s task=%s attempt=%d/%d next=%s",
		ErrRetryScheduled,
		e.ActivityName,
		e.TaskID,
		e.Attempt,
		e.MaxAttempts,
		e.NextAttemptAt.UTC().Format(time.RFC3339Nano),
	)
}

func (e *RetryScheduledError) Unwrap() error { return ErrRetryScheduled }

func retryScheduled(err error) (*RetryScheduledError, bool) {
	var retry *RetryScheduledError
	if !errors.As(err, &retry) || retry == nil {
		return nil, false
	}
	return retry, true
}

type retryStoreState struct {
	mu     sync.Mutex
	leased map[string]*ActivityTask
}

type retryStore struct {
	Store
	module *compiler.Module
	state  *retryStoreState
	now    func() time.Time
}

type retryTransactionalStore struct {
	*retryStore
	transactional TransactionalStore
}

type retryStoreTransaction struct {
	*retryStore
	tx StoreTransaction
}

func newRetryStore(module *compiler.Module, store Store, now func() time.Time) Store {
	if store == nil {
		return nil
	}
	state := &retryStoreState{leased: map[string]*ActivityTask{}}
	base := &retryStore{Store: store, module: module, state: state, now: now}
	if transactional, ok := store.(TransactionalStore); ok {
		return &retryTransactionalStore{retryStore: base, transactional: transactional}
	}
	return base
}

func (s *retryStore) currentTime() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func (s *retryTransactionalStore) BeginTransaction(ctx context.Context) (StoreTransaction, error) {
	tx, err := s.transactional.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	base := &retryStore{Store: tx, module: s.module, state: s.state, now: s.now}
	return &retryStoreTransaction{retryStore: base, tx: tx}, nil
}

func (s *retryStoreTransaction) Commit() error   { return s.tx.Commit() }
func (s *retryStoreTransaction) Rollback() error { return s.tx.Rollback() }

func (s *retryStore) PollTask(ctx context.Context, executionID string) (*ActivityTask, error) {
	due, err := s.hasDuePendingTask(ctx, executionID)
	if err != nil || !due {
		return nil, err
	}
	task, err := s.Store.PollTask(ctx, executionID)
	if err != nil || task == nil {
		return task, err
	}
	s.rememberLease(task)
	return task, nil
}

func (s *retryStore) PollTaskWithLease(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*ActivityTask, error) {
	due, err := s.hasDuePendingTask(ctx, executionID)
	if err != nil || !due {
		return nil, err
	}
	task, err := s.Store.PollTaskWithLease(ctx, executionID, workerID, leaseTTL)
	if err != nil || task == nil {
		return task, err
	}
	s.rememberLease(task)
	return task, nil
}

func (s *retryStore) CompleteTask(ctx context.Context, taskID string, result map[string]any) error {
	if err := s.Store.CompleteTask(ctx, taskID, result); err != nil {
		return err
	}
	s.forgetLease(taskID)
	return nil
}

func (s *retryStore) FailTask(ctx context.Context, taskID string, errorMessage string) error {
	task := s.leasedTask(taskID)
	if task == nil || !isRetryableActivityFailure(errorMessage) || ctx.Err() != nil || task.MaxAttempts <= 1 {
		if err := s.Store.FailTask(ctx, taskID, errorMessage); err != nil {
			return err
		}
		s.forgetLease(taskID)
		return nil
	}

	if task.Attempt >= task.MaxAttempts {
		if err := s.Store.AppendHistory(ctx, task.ExecutionID, "ActivityRetryExhausted", map[string]any{
			"activity":    task.ActivityName,
			"rule":        task.RuleName,
			"task":        task.ID,
			"attempt":     task.Attempt,
			"maxAttempts": task.MaxAttempts,
			"error":       errorMessage,
		}); err != nil {
			return err
		}
		if err := s.Store.FailTask(ctx, taskID, errorMessage); err != nil {
			return err
		}
		s.forgetLease(taskID)
		return nil
	}

	now := s.currentTime()
	delay := retryDelay(s.module, task.ActivityName, task.Attempt)
	nextAttemptAt := now.Add(delay)
	next := cloneRetryTask(task)
	next.Status = TaskPending
	next.Error = errorMessage
	next.Result = nil
	next.LockedBy = ""
	next.LockedUntil = time.Time{}
	next.NextAttemptAt = nextAttemptAt
	next.UpdatedAt = now

	if err := s.Store.UpdateTask(ctx, next); err != nil {
		return err
	}
	if err := s.Store.AppendHistory(ctx, task.ExecutionID, "ActivityRetryScheduled", map[string]any{
		"activity":      task.ActivityName,
		"rule":          task.RuleName,
		"task":          task.ID,
		"attempt":       task.Attempt,
		"maxAttempts":   task.MaxAttempts,
		"delay":         delay.String(),
		"nextAttemptAt": nextAttemptAt.Format(time.RFC3339Nano),
		"error":         errorMessage,
	}); err != nil {
		return err
	}
	s.forgetLease(taskID)
	return &RetryScheduledError{
		TaskID:        task.ID,
		ExecutionID:   task.ExecutionID,
		ActivityName:  task.ActivityName,
		Attempt:       task.Attempt,
		MaxAttempts:   task.MaxAttempts,
		NextAttemptAt: nextAttemptAt,
	}
}

func (s *retryStore) hasDuePendingTask(ctx context.Context, executionID string) (bool, error) {
	tasks, err := s.Store.ListTasks(ctx, executionID)
	if err != nil {
		return false, err
	}
	now := s.currentTime()
	for _, task := range tasks {
		if task == nil || task.Status != TaskPending {
			continue
		}
		if task.NextAttemptAt.IsZero() || !task.NextAttemptAt.After(now) {
			return true, nil
		}
	}
	return false, nil
}

func (s *retryStore) rememberLease(task *ActivityTask) {
	if task == nil || s.state == nil {
		return
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.leased[task.ID] = cloneRetryTask(task)
}

func (s *retryStore) leasedTask(taskID string) *ActivityTask {
	if s.state == nil {
		return nil
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return cloneRetryTask(s.state.leased[taskID])
}

func (s *retryStore) forgetLease(taskID string) {
	if s.state == nil {
		return
	}
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	delete(s.state.leased, taskID)
}

func cloneRetryTask(task *ActivityTask) *ActivityTask {
	if task == nil {
		return nil
	}
	copy := *task
	copy.Input = cloneAnyMap(task.Input)
	copy.Result = cloneAnyMap(task.Result)
	return &copy
}

func isRetryableActivityFailure(message string) bool {
	return strings.HasPrefix(message, "AX505:") || message == "AX505" || strings.Contains(message, "AX505")
}

func retryDelay(module *compiler.Module, activityName string, attempt int) time.Duration {
	mode := "exponential"
	base := defaultRetryBackoff

	if module != nil {
		if activity, ok := module.Activities[activityName]; ok && activity.Policy != "" {
			if policy, ok := module.Policies[activity.Policy]; ok {
				if expression := policy.Entries["backoff"]; expression != nil {
					if parsedMode, parsedBase, ok := parseRetryBackoff(expression); ok {
						mode = parsedMode
						base = parsedBase
					}
				}
			}
		}
	}

	if base <= 0 {
		return 0
	}
	if mode == "fixed" {
		if base > maxRetryBackoff {
			return maxRetryBackoff
		}
		return base
	}

	delay := base
	for current := 1; current < attempt && delay < maxRetryBackoff; current++ {
		if delay >= maxRetryBackoff/2 {
			return maxRetryBackoff
		}
		delay *= 2
	}
	if delay > maxRetryBackoff {
		return maxRetryBackoff
	}
	return delay
}

func parseRetryBackoff(expression *lang.Expr) (string, time.Duration, bool) {
	if duration, ok := retryDuration(expression); ok {
		return "fixed", duration, true
	}
	if expression == nil || expression.Kind != lang.ExprCall || len(expression.Args) != 1 {
		return "", 0, false
	}
	if expression.Name != "fixed" && expression.Name != "exponential" {
		return "", 0, false
	}
	duration, ok := retryDuration(expression.Args[0])
	if !ok {
		return "", 0, false
	}
	return expression.Name, duration, true
}

func retryDuration(expression *lang.Expr) (time.Duration, bool) {
	if expression == nil || expression.Kind != lang.ExprLiteral {
		return 0, false
	}
	switch value := expression.Value.(type) {
	case lang.DurationLiteral:
		duration, err := time.ParseDuration(string(value))
		return duration, err == nil
	case time.Duration:
		return value, true
	default:
		return 0, false
	}
}

func drainUntilIdle(ctx context.Context, engine *Engine, executionID string) error {
	for {
		err := engine.RunUntilIdle(ctx, executionID)
		if err == nil {
			return nil
		}
		retry, ok := retryScheduled(err)
		if !ok {
			return err
		}
		wait := retry.NextAttemptAt.Sub(engine.now())
		if wait <= 0 {
			continue
		}
		timer := engine.newTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C():
		}
	}
}
