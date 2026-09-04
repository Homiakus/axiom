package axiom

import (
	"context"
	"strings"
	"testing"
	"time"
)

const absoluteTimerSource = `domain TimerRuntime

context State:
  deadline: Time = "2026-08-07T12:10:00Z"
  fired: Bool = false
  count: Int = 0

rule expire:
  on timer(State.deadline)
  write:
    State.fired = true
    State.count = State.count + 1
`

func TestTimerNextDueAndOneShotFiring(t *testing.T) {
	module, err := Compile([]byte(absoluteTimerSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	run := engine.Execution("timer-one-shot")
	if err := engine.Start(ctx, run.ID(), nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	next, err := run.NextTimer(ctx)
	if err != nil {
		t.Fatalf("NextTimer() error = %v", err)
	}
	wantDue := time.Date(2026, 8, 7, 12, 10, 0, 0, time.UTC)
	if next == nil || !next.DueAt.Equal(wantDue) || next.Rule != "expire" || next.Key == "" {
		t.Fatalf("NextTimer() = %#v, want expire at %s", next, wantDue)
	}

	before := wantDue.Add(-time.Nanosecond)
	count, err := run.RunDueTimers(ctx, before)
	if err != nil {
		t.Fatalf("RunDueTimers(before) error = %v", err)
	}
	if count != 0 {
		t.Fatalf("RunDueTimers(before) = %d, want 0", count)
	}
	assertTimerState(t, run, false, 0)

	count, err = run.RunDueTimers(ctx, wantDue)
	if err != nil {
		t.Fatalf("RunDueTimers(due) error = %v", err)
	}
	if count != 1 {
		t.Fatalf("RunDueTimers(due) = %d, want 1", count)
	}
	assertTimerState(t, run, true, 1)

	count, err = run.RunDueTimers(ctx, wantDue.Add(time.Hour))
	if err != nil {
		t.Fatalf("RunDueTimers(second) error = %v", err)
	}
	if count != 0 {
		t.Fatalf("RunDueTimers(second) = %d, want 0", count)
	}
	assertTimerState(t, run, true, 1)

	next, err = run.NextTimer(ctx)
	if err != nil {
		t.Fatalf("NextTimer(after fire) error = %v", err)
	}
	if next != nil {
		t.Fatalf("NextTimer(after fire) = %#v, want nil", next)
	}
	history, err := run.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if got := countHistoryType(history, "TimerFired"); got != 1 {
		t.Fatalf("TimerFired count = %d, want 1", got)
	}
}

func TestTimerReschedulesWhenReferencedDeadlineChanges(t *testing.T) {
	module, err := Compile([]byte(absoluteTimerSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	run := engine.Execution("timer-reschedule")
	if err := engine.Start(ctx, run.ID(), nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	firstDue := time.Date(2026, 8, 7, 12, 10, 0, 0, time.UTC)
	if fired, err := run.RunDueTimers(ctx, firstDue); err != nil || fired != 1 {
		t.Fatalf("first RunDueTimers() fired=%d err=%v", fired, err)
	}

	secondDue := firstDue.Add(time.Hour)
	if err := run.Patch(ctx, Patch{"State.deadline": secondDue.Format(time.RFC3339)}); err != nil {
		t.Fatalf("Patch(deadline) error = %v", err)
	}
	next, err := run.NextTimer(ctx)
	if err != nil {
		t.Fatalf("NextTimer(rescheduled) error = %v", err)
	}
	if next == nil || !next.DueAt.Equal(secondDue) {
		t.Fatalf("NextTimer(rescheduled) = %#v, want %s", next, secondDue)
	}
	if fired, err := run.RunDueTimers(ctx, secondDue); err != nil || fired != 1 {
		t.Fatalf("second RunDueTimers() fired=%d err=%v", fired, err)
	}
	assertTimerState(t, run, true, 2)
}

func TestTimerSupportsDurationAfterRuntimeCreatedAt(t *testing.T) {
	source := []byte(`domain TimerCreatedAt

context State:
  fired: Bool = false

rule expire:
  on timer(5m after runtime.createdAt)
  write:
    State.fired = true
`)
	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	engine, err := New(module, WithStore(store))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "timer-created-at", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	execution, err := store.GetExecution(ctx, "timer-created-at")
	if err != nil {
		t.Fatalf("GetExecution() error = %v", err)
	}
	next, err := engine.Execution("timer-created-at").NextTimer(ctx)
	if err != nil {
		t.Fatalf("NextTimer() error = %v", err)
	}
	want := execution.CreatedAt.Add(5 * time.Minute)
	if next == nil || !next.DueAt.Equal(want) {
		t.Fatalf("NextTimer() = %#v, want due %s", next, want)
	}
}

func TestInvalidTimerExpressionFailsWithAX512(t *testing.T) {
	source := []byte(`domain InvalidTimer

context State:
  createdAt: Time = "2026-08-07T12:00:00Z"

rule invalid:
  on timer(banana after State.createdAt)
  write:
    State.createdAt = State.createdAt
`)
	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "invalid-timer", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, err = engine.Execution("invalid-timer").NextTimer(ctx)
	if err == nil || !strings.Contains(err.Error(), "AX512") {
		t.Fatalf("NextTimer() error = %v, want AX512", err)
	}
}

func TestTimerRuleFailureRollsBackTimerFiringWithPebble(t *testing.T) {
	source := []byte(`domain TimerRollback

context State:
  deadline: Time = "2026-08-07T12:00:00Z"
  fired: Bool = false

claim stayFalse:
  always:
    not State.fired

rule invalid:
  on timer(State.deadline)
  write:
    State.fired = true
`)
	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatalf("OpenPebble() error = %v", err)
	}
	defer store.Close()
	engine, err := New(module, WithStore(store), WithProductionMode())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	run := engine.Execution("timer-rollback")
	if err := engine.Start(ctx, run.ID(), nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	due := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	if _, err := run.RunDueTimers(ctx, due); err == nil || !strings.Contains(err.Error(), "AX514") {
		t.Fatalf("RunDueTimers() error = %v, want AX514", err)
	}

	history, err := run.History(ctx)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if got := countHistoryType(history, "TimerFired"); got != 0 {
		t.Fatalf("TimerFired count after rollback = %d, want 0", got)
	}
	assertTimerState(t, run, false, -1)

	next, err := run.NextTimer(ctx)
	if err != nil {
		t.Fatalf("NextTimer(after rollback) error = %v", err)
	}
	if next == nil || !next.DueAt.Equal(due) {
		t.Fatalf("NextTimer(after rollback) = %#v, want timer still due", next)
	}
}

func TestTimerWorkerFiresOwnedExecution(t *testing.T) {
	module, err := Compile([]byte(absoluteTimerSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	run := engine.Execution("timer-worker")
	if err := engine.Start(ctx, run.ID(), Patch{"State.deadline": "2000-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	errorsCh := engine.StartTimerWorker(workerCtx, func(context.Context) ([]string, error) {
		return []string{run.ID(), run.ID()}, nil
	}, TimerWorkerOptions{PollInterval: time.Millisecond})
	defer func() {
		cancel()
		for range errorsCh {
		}
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var state struct {
			Deadline string `json:"deadline"`
			Fired    bool   `json:"fired"`
			Count    int    `json:"count"`
		}
		if err := run.State(ctx, &state); err != nil {
			t.Fatalf("State() error = %v", err)
		}
		if state.Fired {
			if state.Count != 1 {
				t.Fatalf("timer worker count = %d, want 1", state.Count)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timer worker did not fire owned execution before timeout")
}

func assertTimerState(t *testing.T, run *Run, fired bool, count int) {
	t.Helper()
	var state struct {
		Deadline string `json:"deadline"`
		Fired    bool   `json:"fired"`
		Count    int    `json:"count"`
	}
	if err := run.State(context.Background(), &state); err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if state.Fired != fired {
		t.Fatalf("State.fired = %v, want %v", state.Fired, fired)
	}
	if count >= 0 && state.Count != count {
		t.Fatalf("State.count = %d, want %d", state.Count, count)
	}
}
