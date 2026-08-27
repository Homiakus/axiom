package axiom

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExternalWorkerRetryAfterRaisesPolicyBackoffFloor(t *testing.T) {
	ctx := context.Background()
	engine, store := newExternalWorkerFixture(t, "external-retry-after")
	claim, err := engine.ClaimExternalActivity(ctx, "external-retry-after", "worker-a", time.Second)
	if err != nil || claim == nil {
		t.Fatalf("claim = %#v err=%v", claim, err)
	}

	started := time.Now()
	err = engine.FailExternalActivityAfter(ctx, claim.Token, "UPSTREAM_BUSY", 250*time.Millisecond)
	if !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("FailExternalActivityAfter() error = %v, want ErrRetryScheduled", err)
	}
	var scheduled *RetryScheduledError
	if !errors.As(err, &scheduled) || scheduled == nil {
		t.Fatalf("retry error = %T %v", err, err)
	}
	if scheduled.NextAttemptAt.Before(started.Add(200 * time.Millisecond)) {
		t.Fatalf("provider retry-after floor was not honored: next=%s started=%s", scheduled.NextAttemptAt, started)
	}

	tasks, err := store.ListTasks(ctx, "external-retry-after")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks() len=%d err=%v", len(tasks), err)
	}
	if tasks[0].Status != TaskPending || tasks[0].Attempt != 1 {
		t.Fatalf("deferred task = %#v", tasks[0])
	}
	if tasks[0].NextAttemptAt.Before(started.Add(200 * time.Millisecond)) {
		t.Fatalf("persisted retry-after floor was not honored: %s", tasks[0].NextAttemptAt)
	}

	history, err := store.ListHistory(ctx, "external-retry-after")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	deferred := false
	for _, entry := range history {
		if entry.Type == "ActivityRetryDeferred" {
			deferred = true
			if code, _ := entry.Payload["code"].(string); code != "UPSTREAM_BUSY" {
				t.Fatalf("deferred code = %q", code)
			}
		}
	}
	if !deferred {
		t.Fatal("ActivityRetryDeferred history entry missing")
	}
}

func TestExternalWorkerRetryAfterRejectsUnsafeBoundsWithoutConsumingClaim(t *testing.T) {
	ctx := context.Background()
	engine, _ := newExternalWorkerFixture(t, "external-retry-after-bounds")
	claim, err := engine.ClaimExternalActivity(ctx, "external-retry-after-bounds", "worker-a", time.Second)
	if err != nil || claim == nil {
		t.Fatalf("claim = %#v err=%v", claim, err)
	}

	for _, delay := range []time.Duration{0, -time.Second, 8 * 24 * time.Hour} {
		if err := engine.FailExternalActivityAfter(ctx, claim.Token, "UPSTREAM_BUSY", delay); !errors.Is(err, ErrExternalActivityClaimInvalid) {
			t.Fatalf("delay %v error = %v, want invalid claim", delay, err)
		}
	}
	if err := engine.HeartbeatExternalActivity(ctx, claim.Token); err != nil {
		t.Fatalf("invalid retry-after consumed the live claim: %v", err)
	}
}
