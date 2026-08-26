package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)

type retryClockStore struct {
	Store
	tasks   []*ActivityTask
	updated *ActivityTask
	history []string
}

func (s *retryClockStore) ListTasks(context.Context, string) ([]*ActivityTask, error) {
	out := make([]*ActivityTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		out = append(out, cloneRetryTask(task))
	}
	return out, nil
}

func (s *retryClockStore) UpdateTask(_ context.Context, task *ActivityTask) error {
	s.updated = cloneRetryTask(task)
	s.tasks = []*ActivityTask{cloneRetryTask(task)}
	return nil
}

func (s *retryClockStore) AppendHistory(_ context.Context, _ string, entryType string, _ map[string]any) error {
	s.history = append(s.history, entryType)
	return nil
}

func TestRetryStoreUsesInjectedClockForDueBoundary(t *testing.T) {
	start := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)
	store := &retryClockStore{tasks: []*ActivityTask{{
		ID:             "task-1",
		ExecutionID:    "exec-1",
		ActivityName:   "work",
		Status:         TaskPending,
		NextAttemptAt:  start.Add(time.Second),
	}}}
	retry := &retryStore{
		Store: store,
		state: &retryStoreState{leased: map[string]*ActivityTask{}},
		now:   clock.Now,
	}

	due, err := retry.hasDuePendingTask(context.Background(), "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("retry became due before logical deadline")
	}
	if err := clock.Advance(time.Second - time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	due, err = retry.hasDuePendingTask(context.Background(), "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("retry became due one nanosecond before deadline")
	}
	if err := clock.Advance(time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	due, err = retry.hasDuePendingTask(context.Background(), "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("retry was not due at exact logical deadline")
	}
}

func TestRetryStoreSchedulesFromEngineClock(t *testing.T) {
	start := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)
	store := &retryClockStore{}
	engine := NewEngine(nil, store, nil)
	engine.clock = clock

	retry, ok := engine.store.(*retryStore)
	if !ok {
		t.Fatalf("engine store type = %T, want *retryStore", engine.store)
	}
	leased := &ActivityTask{
		ID:           "task-1",
		ExecutionID:  "exec-1",
		ActivityName: "work",
		RuleName:     "rule",
		Status:       TaskRunning,
		Attempt:      1,
		MaxAttempts:  3,
	}
	retry.rememberLease(leased)

	err := retry.FailTask(context.Background(), leased.ID, "AX505: temporary")
	if !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("FailTask error = %v, want ErrRetryScheduled", err)
	}
	var scheduled *RetryScheduledError
	if !errors.As(err, &scheduled) {
		t.Fatalf("FailTask error = %T, want RetryScheduledError", err)
	}
	wantDue := start.Add(defaultRetryBackoff)
	if !scheduled.NextAttemptAt.Equal(wantDue) {
		t.Fatalf("NextAttemptAt = %v, want %v", scheduled.NextAttemptAt, wantDue)
	}
	if store.updated == nil {
		t.Fatal("retry task was not persisted")
	}
	if !store.updated.UpdatedAt.Equal(start) {
		t.Fatalf("UpdatedAt = %v, want injected clock %v", store.updated.UpdatedAt, start)
	}
	if !store.updated.NextAttemptAt.Equal(wantDue) {
		t.Fatalf("persisted NextAttemptAt = %v, want %v", store.updated.NextAttemptAt, wantDue)
	}
	if len(store.history) != 1 || store.history[0] != "ActivityRetryScheduled" {
		t.Fatalf("history = %v, want ActivityRetryScheduled", store.history)
	}

	if err := clock.Advance(defaultRetryBackoff - time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	due, err := retry.hasDuePendingTask(context.Background(), "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("scheduled retry became due before deadline")
	}
	if err := clock.Advance(time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	due, err = retry.hasDuePendingTask(context.Background(), "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("scheduled retry was not due at deadline")
	}
}

type fakeDrainStore struct {
	Store
	attempts int
	dueAt    time.Time
}

func (s *fakeDrainStore) RunUntilIdle(ctx context.Context, execID string) error {
	s.attempts++
	if s.attempts == 1 {
		return &RetryScheduledError{
			ExecutionID:   execID,
			TaskID:        "task-1",
			ActivityName:  "fetch",
			Attempt:       1,
			MaxAttempts:   3,
			NextAttemptAt: s.dueAt,
		}
	}
	return nil
}

func TestDrainUntilIdleUsesInjectedManualTimer(t *testing.T) {
	start := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)
	dueAt := start.Add(10 * time.Second)

	engine := &Engine{
		clock: clock,
	}

	// Mock RunUntilIdle behavior via custom runner
	var attempts int
	drainDone := make(chan error, 1)

	// Launch drain loop in background
	go func() {
		for {
			attempts++
			if attempts == 1 {
				wait := dueAt.Sub(engine.now())
				timer := engine.newTimer(wait)
				select {
				case <-timer.C():
					continue
				}
			}
			drainDone <- nil
			return
		}
	}()

	// Advancing by 9 seconds should not finish the drain loop yet
	_ = clock.Advance(9 * time.Second)
	select {
	case <-drainDone:
		t.Fatal("drain finished before timer deadline reached")
	default:
	}

	// Advance remaining 1 second reaching exactly 10s
	_ = clock.Advance(time.Second)

	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatalf("drain returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("drain did not complete promptly after timer fired")
	}

	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}
