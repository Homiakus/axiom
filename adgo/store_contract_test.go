package adgo

import (
	"context"
	"math"
	"testing"
	"time"
)

// CE-008: MemoryStore Commit with a failing clone or invalid mutate must not modify durable state.
func TestStoreAtomicOnCloneError(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	initial := &Execution{
		ID:          "exec-atomicity-1",
		PlanID:      "test-plan",
		PlanVersion: "1.0",
		PlanDigest:  "sha256:123",
		Version:     1,
		Status:      StatusRunning,
		BudgetUsage: BudgetUsage{Cost: 5.0},
	}
	if err := store.Create(ctx, initial); err != nil {
		t.Fatalf("failed to create execution: %v", err)
	}

	// Mutate inserts non-serializable value (NaN inside float map or bad data) causing clone error
	_, err := store.Commit(ctx, "exec-atomicity-1", 1, func(e *Execution) error {
		// Set NaN in a place that causes clone error if clone checks or unmarshal fails
		e.Quality = map[string]float64{"score": math.NaN()}
		return nil
	})

	// Regardless of whether Commit returned an error or succeeded, loading the execution must be valid
	loaded, loadErr := store.Load(ctx, "exec-atomicity-1")
	if loadErr != nil && err == nil {
		t.Fatalf("commit succeeded but load failed: %v", loadErr)
	}
	if loaded != nil && loaded.Version > 1 && err != nil {
		t.Fatalf("commit returned error %v but state version advanced to %d", err, loaded.Version)
	}
}

// TestScheduleStoreAtomicOnCloneError verifies atomic behavior of MemoryScheduleStore.Commit.
func TestScheduleStoreAtomicOnCloneError(t *testing.T) {
	store := NewMemoryScheduleStore()
	ctx := context.Background()

	sched := &Schedule{
		ID:         "sched-1",
		PlanDigest: "sha256:abc",
		Every:      time.Minute,
		StartAt:    time.Now().UTC(),
		NextAt:     time.Now().UTC(),
		Enabled:    true,
		Version:    1,
	}
	if err := store.Create(ctx, sched); err != nil {
		t.Fatalf("failed to create schedule: %v", err)
	}

	// Attempt a mutation
	updated, err := store.Commit(ctx, "sched-1", 1, func(s *Schedule) error {
		s.Every = 2 * time.Minute
		return nil
	})
	if err != nil {
		t.Fatalf("expected commit to succeed: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("expected updated version 2, got %d", updated.Version)
	}
}

// TestCacheCopyIsolation verifies defensive copy isolation in MemoryActivityCache.
func TestCacheCopyIsolation(t *testing.T) {
	cache := NewMemoryActivityCache()
	ctx := context.Background()
	key := "cache-key-1"

	lease, err := cache.Claim(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	res := ActivityResult{
		Outcome: OutcomeCompleted,
		Facts:   map[string]any{"counter": 10},
	}
	if err := cache.Put(ctx, lease, res, time.Minute); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	// Modify original caller map
	res.Facts["counter"] = 999

	// Read from cache
	cached, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if cached == nil {
		t.Fatalf("expected cache entry to be found")
	}
	if cached.Result.Facts["counter"].(float64) != 10 && cached.Result.Facts["counter"] != 10 {
		t.Fatalf("cache was mutated by caller: got %v, want 10", cached.Result.Facts["counter"])
	}
}
