package axiom

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func supersessionSource(mode string) []byte {
	return []byte(fmt.Sprintf(`domain Supersession

signal Submit:
  value: Int

context State:
  value: Int = 0

policy lane:
  retry: 0
  timeout: 1s
  concurrency: %s
  idempotency: optional

activity Work:
  input:
    value = signal.value
  output:
    value: Int
  effect: local
  policy: lane

rule process:
  on Submit
  run: Work
  write:
    State.value = output.value
`, mode))
}

func TestConcurrencyLatestKeepsOnlyNewestPendingTask(t *testing.T) {
	module, err := Compile(supersessionSource("latest"))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	var executed []int
	engine, err := New(module, WithStore(store), Act("Work", func(_ context.Context, input Input) (Output, error) {
		executed = append(executed, input["value"].(int))
		return Output{"value": input["value"]}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "latest", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for _, value := range []int{1, 2, 3} {
		if err := engine.Signal(ctx, "latest", "Submit", map[string]any{"value": value}); err != nil {
			t.Fatalf("Signal(%d) error = %v", value, err)
		}
	}

	tasks, err := store.ListTasks(ctx, "latest")
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	assertTaskStatusCounts(t, tasks, map[TaskStatus]int{TaskSuperseded: 2, TaskPending: 1})
	pending := taskWithStatus(t, tasks, TaskPending)
	if pending.Input["value"] != 3 {
		t.Fatalf("latest pending input = %#v, want 3", pending.Input["value"])
	}

	if err := engine.RunUntilIdle(ctx, "latest"); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
	if len(executed) != 1 || executed[0] != 3 {
		t.Fatalf("executed = %#v, want [3]", executed)
	}
	history, err := store.ListHistory(ctx, "latest")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if got := countHistoryType(history, "ActivitySuperseded"); got != 2 {
		t.Fatalf("ActivitySuperseded count = %d, want 2", got)
	}
}

func TestConcurrencyFirstKeepsEarliestActiveTask(t *testing.T) {
	module, err := Compile(supersessionSource("first"))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	var executed []int
	engine, err := New(module, WithStore(store), Act("Work", func(_ context.Context, input Input) (Output, error) {
		executed = append(executed, input["value"].(int))
		return Output{"value": input["value"]}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "first", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for _, value := range []int{1, 2, 3} {
		if err := engine.Signal(ctx, "first", "Submit", map[string]any{"value": value}); err != nil {
			t.Fatalf("Signal(%d) error = %v", value, err)
		}
	}

	tasks, err := store.ListTasks(ctx, "first")
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	assertTaskStatusCounts(t, tasks, map[TaskStatus]int{TaskPending: 1, TaskSuperseded: 2})
	pending := taskWithStatus(t, tasks, TaskPending)
	if pending.Input["value"] != 1 {
		t.Fatalf("first pending input = %#v, want 1", pending.Input["value"])
	}

	if err := engine.RunUntilIdle(ctx, "first"); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
	if len(executed) != 1 || executed[0] != 1 {
		t.Fatalf("executed = %#v, want [1]", executed)
	}
}

func TestConcurrencyLatestNeverSupersedesRunningTask(t *testing.T) {
	module, err := Compile(supersessionSource("latest"))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	engine, err := New(module, WithStore(store), Act("Work", func(_ context.Context, input Input) (Output, error) {
		return Output{"value": input["value"]}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "running", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "running", "Submit", map[string]any{"value": 1}); err != nil {
		t.Fatalf("Signal(1) error = %v", err)
	}
	leased, err := store.PollTaskWithLease(ctx, "running", "test-worker", time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("PollTaskWithLease() task=%#v err=%v", leased, err)
	}
	if leased.Status != TaskRunning {
		t.Fatalf("leased status = %s, want running", leased.Status)
	}

	if err := engine.Signal(ctx, "running", "Submit", map[string]any{"value": 2}); err != nil {
		t.Fatalf("Signal(2) error = %v", err)
	}
	if err := engine.Signal(ctx, "running", "Submit", map[string]any{"value": 3}); err != nil {
		t.Fatalf("Signal(3) error = %v", err)
	}
	tasks, err := store.ListTasks(ctx, "running")
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	assertTaskStatusCounts(t, tasks, map[TaskStatus]int{TaskRunning: 1, TaskSuperseded: 1, TaskPending: 1})
	if taskWithStatus(t, tasks, TaskPending).Input["value"] != 3 {
		t.Fatal("newest task was not preserved behind the running task")
	}
}

func TestPebbleLatestDecisionIsAtomicAcrossEngines(t *testing.T) {
	module, err := Compile(supersessionSource("latest"))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatalf("OpenPebble() error = %v", err)
	}
	defer store.Close()
	handler := Act("Work", func(_ context.Context, input Input) (Output, error) {
		return Output{"value": input["value"]}, nil
	})
	firstEngine, err := New(module, WithStore(store), handler)
	if err != nil {
		t.Fatalf("New(firstEngine) error = %v", err)
	}
	secondEngine, err := New(module, WithStore(store), handler)
	if err != nil {
		t.Fatalf("New(secondEngine) error = %v", err)
	}
	ctx := context.Background()
	if err := firstEngine.Start(ctx, "pebble-latest", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- firstEngine.Signal(ctx, "pebble-latest", "Submit", map[string]any{"value": 10})
	}()
	go func() {
		defer wg.Done()
		errs <- secondEngine.Signal(ctx, "pebble-latest", "Submit", map[string]any{"value": 20})
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Signal() error = %v", err)
		}
	}

	tasks, err := store.ListTasks(ctx, "pebble-latest")
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	assertTaskStatusCounts(t, tasks, map[TaskStatus]int{TaskSuperseded: 1, TaskPending: 1})
}

func assertTaskStatusCounts(t *testing.T, tasks []*ActivityTask, want map[TaskStatus]int) {
	t.Helper()
	got := map[TaskStatus]int{}
	for _, task := range tasks {
		got[task.Status]++
	}
	for status, count := range want {
		if got[status] != count {
			t.Fatalf("status %s count = %d, want %d; tasks=%#v", status, got[status], count, tasks)
		}
	}
	if len(tasks) != totalStatusCount(want) {
		t.Fatalf("tasks = %d, want %d; counts=%#v", len(tasks), totalStatusCount(want), got)
	}
}

func totalStatusCount(values map[TaskStatus]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func taskWithStatus(t *testing.T, tasks []*ActivityTask, status TaskStatus) *ActivityTask {
	t.Helper()
	for _, task := range tasks {
		if task.Status == status {
			return task
		}
	}
	t.Fatalf("no task with status %s: %#v", status, tasks)
	return nil
}
