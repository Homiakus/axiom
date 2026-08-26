package adgo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)

func TestMemoryAdmissionControllerExactDeadlineBoundary(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)

	controller := NewMemoryAdmissionController(WithMemoryAdmissionClock(clock))
	policy := AdmissionPolicy{MaxConcurrent: 1}

	// Acquire permit with 10 second TTL
	lease, err := controller.Acquire(ctx, "res-1", policy, 10*time.Second)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if lease.ExpiresAt != start.Add(10*time.Second) {
		t.Fatalf("ExpiresAt = %v, want %v", lease.ExpiresAt, start.Add(10*time.Second))
	}

	// Advance clock by 9.999s (before expiry) -> second acquire must fail
	_ = clock.Advance(10*time.Second - time.Millisecond)
	_, err = controller.Acquire(ctx, "res-1", policy, 10*time.Second)
	if !errors.Is(err, ErrAdmissionDenied) {
		t.Fatalf("Acquire before expiry returned %v, want %v", err, ErrAdmissionDenied)
	}

	// Advance clock by 1ms (reaching exactly 10s) -> permit is expired, acquire must succeed
	_ = clock.Advance(time.Millisecond)
	secondLease, err := controller.Acquire(ctx, "res-1", policy, 10*time.Second)
	if err != nil {
		t.Fatalf("Acquire at exact expiry deadline failed: %v", err)
	}
	if secondLease.Token == lease.Token {
		t.Fatalf("second token = %q, want new token", secondLease.Token)
	}
}

func TestMemoryAdmissionControllerTokenRefillExactLogicalInterval(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)

	controller := NewMemoryAdmissionController(WithMemoryAdmissionClock(clock))
	policy := AdmissionPolicy{
		Rate:   2,
		Period: time.Second,
		Burst:  2,
	}

	// First two tokens consume the burst
	l1, err := controller.Acquire(ctx, "api", policy, time.Minute)
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	_ = controller.Release(ctx, l1)

	l2, err := controller.Acquire(ctx, "api", policy, time.Minute)
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}
	_ = controller.Release(ctx, l2)

	// Third acquire should be rate-limited
	_, err = controller.Acquire(ctx, "api", policy, time.Minute)
	var denied *AdmissionDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("Acquire 3 returned %v, want *AdmissionDeniedError", err)
	}
	if denied.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v, want > 0", denied.RetryAfter)
	}

	// Advance clock by 500ms -> exactly 1 token refilled (2 tokens/sec * 0.5s = 1.0)
	_ = clock.Advance(500 * time.Millisecond)
	l3, err := controller.Acquire(ctx, "api", policy, time.Minute)
	if err != nil {
		t.Fatalf("Acquire after 500ms refill failed: %v", err)
	}
	_ = controller.Release(ctx, l3)
}

func TestMemoryAdmissionControllerHeartbeatExtension(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)

	controller := NewMemoryAdmissionController(WithMemoryAdmissionClock(clock))
	policy := AdmissionPolicy{MaxConcurrent: 1}

	lease, err := controller.Acquire(ctx, "db", policy, 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Advance clock by 4 seconds (1 second before expiry)
	_ = clock.Advance(4 * time.Second)

	// Heartbeat extends lease by another 5 seconds from current time (start + 4s + 5s = start + 9s)
	extended, err := controller.Heartbeat(ctx, lease, 5*time.Second)
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	expectedExpiry := start.Add(9 * time.Second)
	if extended.ExpiresAt != expectedExpiry {
		t.Fatalf("extended.ExpiresAt = %v, want %v", extended.ExpiresAt, expectedExpiry)
	}

	// Advance clock by 2 seconds to start + 6s (past original 5s TTL)
	_ = clock.Advance(2 * time.Second)

	// Snapshot should show 1 in flight because heartbeat extended it
	snap, err := controller.Snapshot(ctx, "db")
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	if snap.InFlight != 1 {
		t.Fatalf("Snapshot InFlight = %d, want 1", snap.InFlight)
	}
}

func TestFileAdmissionControllerDeterministicClock(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)

	root := t.TempDir()
	controller, err := NewFileAdmissionController(root, WithFileAdmissionClock(clock))
	if err != nil {
		t.Fatalf("NewFileAdmissionController failed: %v", err)
	}

	policy := AdmissionPolicy{MaxConcurrent: 1}

	lease, err := controller.Acquire(ctx, "file-res", policy, 10*time.Second)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if lease.ExpiresAt != start.Add(10*time.Second) {
		t.Fatalf("ExpiresAt = %v, want %v", lease.ExpiresAt, start.Add(10*time.Second))
	}

	// Advance clock past 10s
	_ = clock.Advance(11 * time.Second)

	// Stale permit should be purged upon snapshot / next acquire
	snap, err := controller.Snapshot(ctx, "file-res")
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	if snap.InFlight != 0 {
		t.Fatalf("Snapshot InFlight after clock advance = %d, want 0", snap.InFlight)
	}

	// Reacquire should succeed
	lease2, err := controller.Acquire(ctx, "file-res", policy, 10*time.Second)
	if err != nil {
		t.Fatalf("Reacquire after expiry failed: %v", err)
	}
	if lease2.ExpiresAt != start.Add(21*time.Second) {
		t.Fatalf("lease2.ExpiresAt = %v, want %v", lease2.ExpiresAt, start.Add(21*time.Second))
	}
}
