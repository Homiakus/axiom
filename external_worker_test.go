package axiom

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newExternalWorkerFixture(t *testing.T, executionID string) (*Engine, Store) {
	t.Helper()
	module := compileDurableRetryModule(t)
	store := NewMemoryStore()
	engine, err := New(module, WithStore(store), Act("Work", func(context.Context, Input) (Output, error) {
		return Output{"ok": true}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, executionID, nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, executionID, "Run", nil); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	return engine, store
}

func TestExternalWorkerClaimHeartbeatCompleteIsFenced(t *testing.T) {
	ctx := context.Background()
	engine, _ := newExternalWorkerFixture(t, "external-complete")

	claim, err := engine.ClaimExternalActivity(ctx, "external-complete", "worker-a", time.Second)
	if err != nil {
		t.Fatalf("ClaimExternalActivity() error = %v", err)
	}
	if claim == nil {
		t.Fatal("ClaimExternalActivity() returned nil claim")
	}
	if claim.ActivityName != "Work" || claim.Token.WorkerID != "worker-a" || claim.Token.Attempt != 1 {
		t.Fatalf("claim = %#v", claim)
	}
	if claim.IdempotencyKey == "" {
		t.Fatal("claim idempotency key is empty")
	}

	stale := claim.Token
	stale.WorkerID = "worker-b"
	if err := engine.CompleteExternalActivity(ctx, stale, Output{"ok": true}); !errors.Is(err, ErrExternalActivityClaimStale) {
		t.Fatalf("wrong-worker completion error = %v, want ErrExternalActivityClaimStale", err)
	}
	if err := engine.HeartbeatExternalActivity(ctx, claim.Token); err != nil {
		t.Fatalf("HeartbeatExternalActivity() error = %v", err)
	}
	if err := engine.CompleteExternalActivity(ctx, claim.Token, Output{"ok": true}); err != nil {
		t.Fatalf("CompleteExternalActivity() error = %v", err)
	}
	if err := engine.CompleteExternalActivity(ctx, claim.Token, Output{"ok": true}); !errors.Is(err, ErrExternalActivityClaimStale) {
		t.Fatalf("duplicate completion error = %v, want stale claim", err)
	}

	state, err := engine.Query(ctx, "external-complete", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	contexts := state["context"].(map[string]map[string]any)
	if contexts["State"]["done"] != true {
		t.Fatalf("State.done = %#v, want true", contexts["State"]["done"])
	}
	claim, err = engine.ClaimExternalActivity(ctx, "external-complete", "worker-a", time.Second)
	if err != nil || claim != nil {
		t.Fatalf("claim after completion = %#v, err=%v; want nil,nil", claim, err)
	}
}

func TestExternalWorkerFailureUsesDurableRetryAndRejectsArbitraryText(t *testing.T) {
	ctx := context.Background()
	engine, store := newExternalWorkerFixture(t, "external-retry")
	claim, err := engine.ClaimExternalActivity(ctx, "external-retry", "worker-a", time.Second)
	if err != nil || claim == nil {
		t.Fatalf("claim = %#v, err=%v", claim, err)
	}

	if err := engine.FailExternalActivity(ctx, claim.Token, "TEMP_UNAVAILABLE\nsecret=token"); !errors.Is(err, ErrExternalActivityClaimInvalid) {
		t.Fatalf("unsafe failure code error = %v, want invalid claim", err)
	}
	if err := engine.HeartbeatExternalActivity(ctx, claim.Token); err != nil {
		t.Fatalf("claim changed after rejected unsafe code: %v", err)
	}

	err = engine.FailExternalActivity(ctx, claim.Token, "TEMP_UNAVAILABLE")
	if !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("FailExternalActivity() error = %v, want ErrRetryScheduled", err)
	}
	tasks, err := store.ListTasks(ctx, "external-retry")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks() len=%d err=%v", len(tasks), err)
	}
	if tasks[0].Status != TaskPending || tasks[0].Attempt != 1 || tasks[0].NextAttemptAt.IsZero() {
		t.Fatalf("retry task = %#v", tasks[0])
	}

	history, err := store.ListHistory(ctx, "external-retry")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	for _, entry := range history {
		if entry.Type == "ActivityRetryScheduled" {
			if text, _ := entry.Payload["error"].(string); text == "" || containsUnsafeExternalText(text) {
				t.Fatalf("retry history error is unsafe: %q", text)
			}
		}
	}
}

func TestExternalWorkerAttemptFencesReclaimedLease(t *testing.T) {
	ctx := context.Background()
	engine, _ := newExternalWorkerFixture(t, "external-reclaim")
	first, err := engine.ClaimExternalActivity(ctx, "external-reclaim", "worker-a", 10*time.Millisecond)
	if err != nil || first == nil {
		t.Fatalf("first claim = %#v, err=%v", first, err)
	}
	time.Sleep(20 * time.Millisecond)
	second, err := engine.ClaimExternalActivity(ctx, "external-reclaim", "worker-b", 50*time.Millisecond)
	if err != nil || second == nil {
		t.Fatalf("second claim = %#v, err=%v", second, err)
	}
	if second.Token.Attempt <= first.Token.Attempt {
		t.Fatalf("reclaimed attempt = %d, first = %d", second.Token.Attempt, first.Token.Attempt)
	}
	if err := engine.CompleteExternalActivity(ctx, first.Token, Output{"ok": true}); !errors.Is(err, ErrExternalActivityClaimStale) {
		t.Fatalf("late first worker completion error = %v, want stale", err)
	}
	if err := engine.CompleteExternalActivity(ctx, second.Token, Output{"ok": true}); err != nil {
		t.Fatalf("second worker completion error = %v", err)
	}
}

func containsUnsafeExternalText(value string) bool {
	return value == "TEMP_UNAVAILABLE\nsecret=token" || value == "secret=token"
}
