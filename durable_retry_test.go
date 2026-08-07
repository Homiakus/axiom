package axiom

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

const durableRetrySource = `domain DurableRetry

signal Run

context State:
  done: Bool = false

policy resilient:
  retry: 2
  backoff: fixed(5ms)
  timeout: 1s
  concurrency: parallel
  idempotency: optional

activity Work:
  output:
    ok: Bool
  effect: local
  policy: resilient

rule execute:
  on Run
  run: Work
  write:
    State.done = output.ok
`

func compileDurableRetryModule(t *testing.T) *Module {
	t.Helper()
	module, err := Compile([]byte(durableRetrySource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return module
}

func TestDurableRetryPersistsAcrossEngineReplacement(t *testing.T) {
	module := compileDurableRetryModule(t)
	store := NewMemoryStore()
	ctx := context.Background()
	var attempts atomic.Int32

	first, err := New(module, WithStore(store), Act("Work", func(context.Context, Input) (Output, error) {
		attempts.Add(1)
		return nil, errors.New("temporary")
	}))
	if err != nil {
		t.Fatalf("New(first) error = %v", err)
	}
	if err := first.Start(ctx, "retry-memory", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := first.Signal(ctx, "retry-memory", "Run", nil); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}

	err = first.RunUntilIdle(ctx, "retry-memory")
	if !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("first RunUntilIdle() error = %v, want ErrRetryScheduled", err)
	}
	firstTask := onlyRetryTask(t, store, "retry-memory")
	if firstTask.Status != TaskPending || firstTask.Attempt != 1 || firstTask.MaxAttempts != 3 {
		t.Fatalf("task after first attempt = %#v", firstTask)
	}
	if firstTask.NextAttemptAt.IsZero() {
		t.Fatal("NextAttemptAt was not persisted")
	}

	// A deferred task must be treated as temporarily idle, not spin in the
	// in-memory pending queue while its NextAttemptAt is still in the future.
	started := time.Now()
	if err := first.RunUntilIdle(ctx, "retry-memory"); err != nil {
		t.Fatalf("RunUntilIdle(before due) error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("RunUntilIdle(before due) took %v; delayed task likely spun", elapsed)
	}

	waitForRetry(t, firstTask.NextAttemptAt)
	second, err := New(module, WithStore(store), Act("Work", func(context.Context, Input) (Output, error) {
		current := attempts.Add(1)
		if current < 3 {
			return nil, errors.New("temporary")
		}
		return Output{"ok": true}, nil
	}))
	if err != nil {
		t.Fatalf("New(second) error = %v", err)
	}

	err = second.RunUntilIdle(ctx, "retry-memory")
	if !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("second RunUntilIdle() error = %v, want ErrRetryScheduled", err)
	}
	secondTask := onlyRetryTask(t, store, "retry-memory")
	if secondTask.Attempt != 2 || secondTask.Status != TaskPending {
		t.Fatalf("task after second attempt = %#v", secondTask)
	}
	waitForRetry(t, secondTask.NextAttemptAt)
	if err := second.RunUntilIdle(ctx, "retry-memory"); err != nil {
		t.Fatalf("third RunUntilIdle() error = %v", err)
	}

	finalTask := onlyRetryTask(t, store, "retry-memory")
	if finalTask.Status != TaskCompleted || finalTask.Attempt != 3 {
		t.Fatalf("final task = %#v", finalTask)
	}
	state, err := second.Query(ctx, "retry-memory", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	contexts := state["context"].(map[string]map[string]any)
	if contexts["State"]["done"] != true {
		t.Fatalf("State.done = %#v, want true", contexts["State"]["done"])
	}
	history, err := store.ListHistory(ctx, "retry-memory")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if got := countHistoryType(history, "ActivityRetryScheduled"); got != 2 {
		t.Fatalf("ActivityRetryScheduled count = %d, want 2", got)
	}
	if countHistoryType(history, "ActivityFailed") != 0 {
		t.Fatalf("successful durable retry must not emit terminal ActivityFailed: %#v", history)
	}
}

func TestDurableRetryExhaustionBecomesTerminalFailure(t *testing.T) {
	source := []byte(`domain RetryExhaustion

signal Run

context State:
  done: Bool = false

policy resilient:
  retry: 1
  backoff: 2ms
  timeout: 1s
  concurrency: parallel
  idempotency: optional

activity Work:
  output:
    ok: Bool
  effect: local
  policy: resilient

rule execute:
  on Run
  run: Work
  write:
    State.done = output.ok
`)
	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	engine, err := New(module, WithStore(store), Act("Work", func(context.Context, Input) (Output, error) {
		return nil, errors.New("still unavailable")
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "retry-exhaust", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "retry-exhaust", "Run", nil); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if err := engine.RunUntilIdle(ctx, "retry-exhaust"); !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("first RunUntilIdle() error = %v, want ErrRetryScheduled", err)
	}
	task := onlyRetryTask(t, store, "retry-exhaust")
	waitForRetry(t, task.NextAttemptAt)
	if err := engine.RunUntilIdle(ctx, "retry-exhaust"); err == nil || !containsErrorCode(err, "AX505") {
		t.Fatalf("terminal RunUntilIdle() error = %v, want AX505", err)
	}

	task = onlyRetryTask(t, store, "retry-exhaust")
	if task.Status != TaskFailed || task.Attempt != 2 || task.MaxAttempts != 2 {
		t.Fatalf("terminal task = %#v", task)
	}
	history, err := store.ListHistory(ctx, "retry-exhaust")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if countHistoryType(history, "ActivityRetryScheduled") != 1 {
		t.Fatalf("history missing exactly one retry checkpoint: %#v", history)
	}
	if countHistoryType(history, "ActivityRetryExhausted") != 1 {
		t.Fatalf("history missing ActivityRetryExhausted: %#v", history)
	}
	if countHistoryType(history, "ActivityFailed") != 1 {
		t.Fatalf("history missing terminal ActivityFailed: %#v", history)
	}
}

func TestRunDispatchDrainsDurableRetryAutomatically(t *testing.T) {
	module := compileDurableRetryModule(t)
	var attempts atomic.Int32
	engine, err := New(module, Act("Work", func(context.Context, Input) (Output, error) {
		if attempts.Add(1) < 2 {
			return nil, errors.New("temporary")
		}
		return Output{"ok": true}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Execution("run-api-retry").Signal(ctx, "Run", nil); err != nil {
		t.Fatalf("Run.Signal() error = %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("handler attempts = %d, want 2", attempts.Load())
	}
	tasks, err := engine.Execution("run-api-retry").PendingActivities(ctx)
	if err != nil {
		t.Fatalf("PendingActivities() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("pending activities = %#v, want none", tasks)
	}
}

func TestPebbleDurableRetrySurvivesStoreReopen(t *testing.T) {
	module := compileDurableRetryModule(t)
	dir := t.TempDir()
	ctx := context.Background()

	store, err := OpenPebble(dir)
	if err != nil {
		t.Fatalf("OpenPebble() error = %v", err)
	}
	first, err := New(module, WithStore(store), Act("Work", func(context.Context, Input) (Output, error) {
		return nil, errors.New("temporary")
	}))
	if err != nil {
		t.Fatalf("New(first) error = %v", err)
	}
	if err := first.Start(ctx, "retry-pebble", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := first.Signal(ctx, "retry-pebble", "Run", nil); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if err := first.RunUntilIdle(ctx, "retry-pebble"); !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("first RunUntilIdle() error = %v, want ErrRetryScheduled", err)
	}
	persisted := onlyRetryTask(t, store, "retry-pebble")
	if persisted.Attempt != 1 || persisted.NextAttemptAt.IsZero() {
		t.Fatalf("persisted retry task = %#v", persisted)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	waitForRetry(t, persisted.NextAttemptAt)
	reopened, err := OpenPebble(dir)
	if err != nil {
		t.Fatalf("reopen OpenPebble() error = %v", err)
	}
	defer reopened.Close()
	second, err := New(module, WithStore(reopened), Act("Work", func(context.Context, Input) (Output, error) {
		return Output{"ok": true}, nil
	}))
	if err != nil {
		t.Fatalf("New(second) error = %v", err)
	}
	if err := second.RunUntilIdle(ctx, "retry-pebble"); err != nil {
		t.Fatalf("RunUntilIdle(after reopen) error = %v", err)
	}
	finalTask := onlyRetryTask(t, reopened, "retry-pebble")
	if finalTask.Status != TaskCompleted || finalTask.Attempt != 2 {
		t.Fatalf("task after reopen = %#v", finalTask)
	}
}

func onlyRetryTask(t *testing.T, store Store, executionID string) *ActivityTask {
	t.Helper()
	tasks, err := store.ListTasks(context.Background(), executionID)
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1: %#v", len(tasks), tasks)
	}
	return tasks[0]
}

func waitForRetry(t *testing.T, due time.Time) {
	t.Helper()
	wait := time.Until(due) + 2*time.Millisecond
	if wait > 0 {
		time.Sleep(wait)
	}
}

func countHistoryType(history []HistoryEntry, entryType string) int {
	count := 0
	for _, entry := range history {
		if entry.Type == entryType {
			count++
		}
	}
	return count
}

func containsErrorCode(err error, code string) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || len(err.Error()) >= len(code) && err.Error()[:len(code)] == code || fmt.Sprint(err) == code
}
