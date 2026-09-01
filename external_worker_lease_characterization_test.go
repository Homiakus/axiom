package axiom

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoreExternalLeaseFencingCharacterization(t *testing.T) {
	ctx := context.Background()

	t.Run("zero deadline is stale", func(t *testing.T) {
		engine, store := newExternalWorkerFixture(t, "core-lease-zero")
		claim, err := engine.ClaimExternalActivity(ctx, "core-lease-zero", "worker-a", time.Second)
		if err != nil || claim == nil {
			t.Fatalf("ClaimExternalActivity() = %#v, %v", claim, err)
		}
		tasks, err := store.ListTasks(ctx, "core-lease-zero")
		if err != nil || len(tasks) != 1 {
			t.Fatalf("ListTasks() len=%d err=%v", len(tasks), err)
		}
		task := tasks[0]
		task.LockedUntil = time.Time{}
		if err := store.UpdateTask(ctx, task); err != nil {
			t.Fatalf("UpdateTask() error = %v", err)
		}
		if err := engine.HeartbeatExternalActivity(ctx, claim.Token); !errors.Is(err, ErrExternalActivityClaimStale) {
			t.Fatalf("HeartbeatExternalActivity() error = %v, want ErrExternalActivityClaimStale", err)
		}
	})

	t.Run("closed expiry boundary is stale", func(t *testing.T) {
		engine, store := newExternalWorkerFixture(t, "core-lease-deadline")
		claim, err := engine.ClaimExternalActivity(ctx, "core-lease-deadline", "worker-a", time.Second)
		if err != nil || claim == nil {
			t.Fatalf("ClaimExternalActivity() = %#v, %v", claim, err)
		}
		tasks, err := store.ListTasks(ctx, "core-lease-deadline")
		if err != nil || len(tasks) != 1 {
			t.Fatalf("ListTasks() len=%d err=%v", len(tasks), err)
		}
		task := tasks[0]
		// Persist the current operational wall time as the deadline. Validation
		// occurs at the same or a later wall-clock instant, so a closed boundary
		// (deadline <= now) must reject the claim without a sleep.
		task.LockedUntil = time.Now().UTC()
		if err := store.UpdateTask(ctx, task); err != nil {
			t.Fatalf("UpdateTask() error = %v", err)
		}
		if err := engine.CompleteExternalActivity(ctx, claim.Token, Output{"ok": true}); !errors.Is(err, ErrExternalActivityClaimStale) {
			t.Fatalf("CompleteExternalActivity() error = %v, want ErrExternalActivityClaimStale", err)
		}
	})

	t.Run("reissue increments attempt and fences old token", func(t *testing.T) {
		engine, store := newExternalWorkerFixture(t, "core-lease-reissue")
		first, err := engine.ClaimExternalActivity(ctx, "core-lease-reissue", "worker-a", time.Second)
		if err != nil || first == nil {
			t.Fatalf("first ClaimExternalActivity() = %#v, %v", first, err)
		}
		tasks, err := store.ListTasks(ctx, "core-lease-reissue")
		if err != nil || len(tasks) != 1 {
			t.Fatalf("ListTasks() len=%d err=%v", len(tasks), err)
		}
		task := tasks[0]
		task.LockedUntil = time.Now().UTC().Add(-time.Second)
		if err := store.UpdateTask(ctx, task); err != nil {
			t.Fatalf("UpdateTask() error = %v", err)
		}

		second, err := engine.ClaimExternalActivity(ctx, "core-lease-reissue", "worker-b", time.Second)
		if err != nil || second == nil {
			t.Fatalf("second ClaimExternalActivity() = %#v, %v", second, err)
		}
		if second.Token.Attempt <= first.Token.Attempt {
			t.Fatalf("reissued attempt=%d, first=%d", second.Token.Attempt, first.Token.Attempt)
		}
		if err := engine.CompleteExternalActivity(ctx, first.Token, Output{"ok": true}); !errors.Is(err, ErrExternalActivityClaimStale) {
			t.Fatalf("old token completion error = %v, want ErrExternalActivityClaimStale", err)
		}
		if err := engine.CompleteExternalActivity(ctx, second.Token, Output{"ok": true}); err != nil {
			t.Fatalf("new token completion error = %v", err)
		}
	})

	t.Run("wrong worker and attempt are fenced", func(t *testing.T) {
		engine, _ := newExternalWorkerFixture(t, "core-lease-token")
		claim, err := engine.ClaimExternalActivity(ctx, "core-lease-token", "worker-a", time.Second)
		if err != nil || claim == nil {
			t.Fatalf("ClaimExternalActivity() = %#v, %v", claim, err)
		}
		wrongWorker := claim.Token
		wrongWorker.WorkerID = "worker-b"
		if err := engine.HeartbeatExternalActivity(ctx, wrongWorker); !errors.Is(err, ErrExternalActivityClaimStale) {
			t.Fatalf("wrong worker heartbeat = %v, want stale", err)
		}
		wrongAttempt := claim.Token
		wrongAttempt.Attempt++
		if err := engine.HeartbeatExternalActivity(ctx, wrongAttempt); !errors.Is(err, ErrExternalActivityClaimStale) {
			t.Fatalf("wrong attempt heartbeat = %v, want stale", err)
		}
	})
}
