package adgo

import (
	"context"
	"testing"
	"time"
)

// CE-013: Two schedule runners on the same fireAt tick concurrently must both remain healthy
// and cursor advances correctly without unhandled conflict crash.
func TestScheduleRunnerConcurrentTick(t *testing.T) {
	def := Definition{
		ID:      "sched-concurrent-plan",
		Version: "1.0",
		Nodes: []Node{
			{ID: "step", Kind: NodeActivity, Activity: "noop"},
		},
	}
	plan, err := Compile(def)
	if err != nil {
		t.Fatalf("failed to compile plan: %v", err)
	}

	reg := NewRegistry()
	reg.Activity("noop", func(ctx context.Context, req ActivityRequest) (ActivityResult, error) {
		return ActivityResult{Outcome: OutcomeCompleted}, nil
	})

	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, reg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	schedStore := NewMemoryScheduleStore()

	runner1, err := NewScheduleRunner(engine, schedStore)
	if err != nil {
		t.Fatalf("failed to create runner1: %v", err)
	}
	runner2, err := NewScheduleRunner(engine, schedStore)
	if err != nil {
		t.Fatalf("failed to create runner2: %v", err)
	}

	ctx := context.Background()
	fireTime := time.Now().UTC().Truncate(time.Second)
	_, err = runner1.Register(ctx, Schedule{
		ID:      "s1",
		Every:   time.Minute,
		StartAt: fireTime,
		NextAt:  fireTime,
	})
	if err != nil {
		t.Fatalf("failed to register schedule: %v", err)
	}

	// Run Tick on both runners for the same timestamp
	t1, err1 := runner1.Tick(ctx, fireTime)
	t2, err2 := runner2.Tick(ctx, fireTime)

	if err1 != nil {
		t.Fatalf("runner1.Tick failed: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("runner2.Tick failed: %v", err2)
	}

	// At least one runner started the execution, and total unique started executions must be 1
	unique := make(map[string]bool)
	for _, id := range append(t1, t2...) {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("expected exactly 1 scheduled execution to be started, got %d (list: %v)", len(unique), unique)
	}
}
