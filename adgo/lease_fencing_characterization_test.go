package adgo

import (
	"errors"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)

func TestLeaseFencingPredicateCharacterization(t *testing.T) {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	baseTask := TaskRuntime{
		ID:         "task-1",
		NodeID:     "node-1",
		Status:     TaskRunning,
		WorkerID:   "worker-a",
		Attempt:    2,
		LeaseUntil: now.Add(time.Second),
	}
	baseToken := WorkToken{
		ExecutionID: "execution-1",
		TaskID:      baseTask.ID,
		WorkerID:    baseTask.WorkerID,
		Attempt:     baseTask.Attempt,
	}

	check := func(t *testing.T, task TaskRuntime, token WorkToken, at time.Time, wantStale bool) {
		t.Helper()
		execution := &Execution{ActiveTasks: map[string]TaskRuntime{task.ID: task}}
		_, err := validateClaim(execution, token, at)
		if wantStale {
			if !errors.Is(err, ErrStaleTask) {
				t.Fatalf("validateClaim() error = %v, want ErrStaleTask", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("validateClaim() error = %v, want nil", err)
		}
	}

	t.Run("future deadline is current", func(t *testing.T) {
		check(t, baseTask, baseToken, now, false)
	})
	t.Run("deadline equality is stale", func(t *testing.T) {
		task := baseTask
		task.LeaseUntil = now
		check(t, task, baseToken, now, true)
	})
	t.Run("past deadline is stale", func(t *testing.T) {
		task := baseTask
		task.LeaseUntil = now.Add(-time.Nanosecond)
		check(t, task, baseToken, now, true)
	})
	t.Run("zero deadline remains current", func(t *testing.T) {
		task := baseTask
		task.LeaseUntil = time.Time{}
		check(t, task, baseToken, now, false)
	})
	t.Run("wrong worker is fenced", func(t *testing.T) {
		token := baseToken
		token.WorkerID = "worker-b"
		check(t, baseTask, token, now, true)
	})
	t.Run("wrong attempt is fenced", func(t *testing.T) {
		token := baseToken
		token.Attempt--
		check(t, baseTask, token, now, true)
	})
	t.Run("reissued attempt fences old token", func(t *testing.T) {
		task := baseTask
		task.Attempt++
		check(t, task, baseToken, now, true)
		newToken := baseToken
		newToken.Attempt = task.Attempt
		check(t, task, newToken, now, false)
	})
}

func TestLeaseFencingUsesSemanticClockDomain(t *testing.T) {
	start := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)
	engine := &Engine{runtime: &Runtime{clock: clock}}
	task := TaskRuntime{
		ID:         "task-1",
		Status:     TaskRunning,
		WorkerID:   "worker-a",
		Attempt:    1,
		LeaseUntil: start.Add(time.Second),
	}
	token := WorkToken{ExecutionID: "execution-1", TaskID: task.ID, WorkerID: task.WorkerID, Attempt: task.Attempt}
	execution := &Execution{ActiveTasks: map[string]TaskRuntime{task.ID: task}}

	if _, err := validateClaim(execution, token, engine.now()); err != nil {
		t.Fatalf("claim before semantic deadline = %v", err)
	}
	if err := clock.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := validateClaim(execution, token, engine.now()); !errors.Is(err, ErrStaleTask) {
		t.Fatalf("claim at semantic deadline = %v, want ErrStaleTask", err)
	}
}
