package axiom

import (
	"context"
	"errors"
	"testing"
	"time"
)

const externalWorkerSource = `domain ExternalWorker

signal Run

context State:
  key: String = "job-1"
  done: Bool = false

policy resilient:
  retry: 2
  backoff: fixed(5ms)
  timeout: 1s
  concurrency: parallel
  idempotency: required

activity Work:
  input:
    key = State.key
  output:
    ok: Bool
  effect: external
  idempotencyKey: State.key
  policy: resilient

rule execute:
  on Run
  run: Work
  write:
    State.done = output.ok
`

func compileExternalWorkerModule(t *testing.T) *Module {
	t.Helper()
	module, err := Compile([]byte(externalWorkerSource))
	if err != nil {
		t.Fatalf("Compile(externalWorkerSource) error = %v", err)
	}
	return module
}

func newExternalWorkerFixture(t *testing.T, executionID string) (*Engine, Store) {
	t.Helper()
	module := compileExternalWorkerModule(t)
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
	engine, store := newExternalWorkerFixture(t, "external-complete")

	claim, err := engine.ClaimExternalActivity(ctx, "external-complete", "worker-a", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("ClaimExternalActivity() error = %v", err)
	}
	if claim == nil {
		t.Fatal("ClaimExternalActivity() returned nil claim")
	}
	if claim.ActivityName != "Work" || claim.Token.WorkerID != "worker-a" || claim.Token.Attempt != 1 {
		t.Fatalf("claim = %#v", claim)
	}
	if claim.IdempotencyKey != "job-1" {
		t.Fatalf("claim idempotency key = %q, want job-1", claim.IdempotencyKey)
	}

	stale := claim.Token
	stale.WorkerID = "worker-b"
	if err := engine.CompleteExternalActivity(ctx, stale, Output{"ok": true}); !errors.Is(err, ErrExternalActivityClaimStale) {
		t.Fatalf("wrong-worker completion error = %v, want ErrExternalActivityClaimStale", err)
	}
	beforeHeartbeat := claim.LeaseUntil
	time.Sleep(10 * time.Millisecond)
	if err := engine.HeartbeatExternalActivity(ctx, claim.Token); err != nil {
		t.Fatalf("HeartbeatExternalActivity() error = %v", err)
	}
	tasks, err := store.ListTasks(ctx, "external-complete")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks() len=%d err=%v", len(tasks), err)
	}
	if !tasks[0].LockedUntil.After(beforeHeartbeat) {
		t.Fatalf("heartbeat did not extend lease: before=%s after=%s", beforeHeartbeat, tasks[0].LockedUntil)
	}
	remaining := tasks[0].LockedUntil.Sub(tasks[0].UpdatedAt)
	if remaining < 150*time.Millisecond || remaining > 250*time.Millisecond {
		t.Fatalf("heartbeat changed requested lease duration unexpectedly: %v", remaining)
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
	first, err := engine.ClaimExternalActivity(ctx, "external-reclaim", "worker-a", 20*time.Millisecond)
	if err != nil || first == nil {
		t.Fatalf("first claim = %#v, err=%v", first, err)
	}
	time.Sleep(35 * time.Millisecond)
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

func TestExternalWorkerNewShortTTLDoesNotStealLiveLongLease(t *testing.T) {
	ctx := context.Background()
	engine, _ := newExternalWorkerFixture(t, "external-cross-ttl")
	first, err := engine.ClaimExternalActivity(ctx, "external-cross-ttl", "worker-long", 200*time.Millisecond)
	if err != nil || first == nil {
		t.Fatalf("first claim = %#v, err=%v", first, err)
	}

	// A claimant asking for a much shorter lease must not use its own TTL as the
	// expiry threshold for an already persisted long lease.
	second, err := engine.ClaimExternalActivity(ctx, "external-cross-ttl", "worker-short", 5*time.Millisecond)
	if err != nil {
		t.Fatalf("second immediate ClaimExternalActivity() error = %v", err)
	}
	if second != nil {
		t.Fatalf("short-TTL claimant stole live long lease: %#v", second)
	}
	if err := engine.HeartbeatExternalActivity(ctx, first.Token); err != nil {
		t.Fatalf("original long claim became stale unexpectedly: %v", err)
	}
}

type externalWorkerSemanticClock struct{ now time.Time }

func (c externalWorkerSemanticClock) Now() time.Time { return c.now }

func TestExternalWorkerLeaseIgnoresSemanticWorkflowClock(t *testing.T) {
	module := compileExternalWorkerModule(t)
	store := NewMemoryStore()
	engine, err := New(
		module,
		WithStore(store),
		WithClock(externalWorkerSemanticClock{now: time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)}),
		Act("Work", func(context.Context, Input) (Output, error) { return Output{"ok": true}, nil }),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "external-clock-domain", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "external-clock-domain", "Run", nil); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	claim, err := engine.ClaimExternalActivity(ctx, "external-clock-domain", "worker-clock", 200*time.Millisecond)
	if err != nil || claim == nil {
		t.Fatalf("claim = %#v, err=%v", claim, err)
	}
	if err := engine.HeartbeatExternalActivity(ctx, claim.Token); err != nil {
		t.Fatalf("semantic clock incorrectly expired operational lease: %v", err)
	}
	if err := engine.CompleteExternalActivity(ctx, claim.Token, Output{"ok": true}); err != nil {
		t.Fatalf("completion under divergent semantic clock failed: %v", err)
	}
}

func containsUnsafeExternalText(value string) bool {
	return value == "TEMP_UNAVAILABLE\nsecret=token" || value == "secret=token"
}
