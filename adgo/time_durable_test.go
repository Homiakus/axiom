package adgo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)


func TestPolicyEngineDeterministicClock(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)

	plan, err := Compile(Definition{ID: "policy-clock", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, NewRegistry(), WithEngineClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicyEngine(engine, RuntimePolicyFunc(func(_ context.Context, req PolicyRequest) (PolicyDecision, error) {
		if req.At.Before(start) {
			return PolicyDecision{Action: PolicyDeny, Reason: "request before clock start"}, nil
		}
		return PolicyDecision{Action: PolicyDelay, Reason: "rate limit delay", RetryAfter: 10 * time.Second}, nil
	}), PolicyEngineOptions{MaxPolicySkipsPerPoll: 1})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if _, err := engine.Start(ctx, "exec-policy", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "exec-policy"); err != nil {
		t.Fatal(err)
	}

	// Poll triggers policy delay
	if _, err := policy.Poll(ctx, WorkerSpec{ID: "worker"}); !errors.Is(err, ErrNoWork) {
		t.Fatalf("expected ErrNoWork from policy delay, got %v", err)
	}

	execution, err := store.Load(ctx, "exec-policy")
	if err != nil {
		t.Fatal(err)
	}
	expectedNotBefore := start.Add(10 * time.Second)
	if !execution.Nodes["work"].NotBefore.Equal(expectedNotBefore) {
		t.Fatalf("NotBefore = %v, want %v", execution.Nodes["work"].NotBefore, expectedNotBefore)
	}

	// Advance 9.999s: should still not be ready to advance
	if err := clock.Advance(10*time.Second - time.Millisecond); err != nil {
		t.Fatal(err)
	}
	advRes, err := engine.Advance(ctx, "exec-policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(advRes.QueuedTasks) != 0 {
		t.Fatalf("task enqueued before delay expired: %+v", advRes)
	}

	// Advance 1ms (reaching 10s): task should be ready and enqueued
	if err := clock.Advance(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	advRes, err = engine.Advance(ctx, "exec-policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(advRes.QueuedTasks) != 1 {
		t.Fatalf("expected 1 task enqueued at exact deadline, got %+v", advRes)
	}
}

func TestHedgedActivityDeterministicClock(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)

	v1Started := make(chan struct{})
	v2Started := make(chan struct{})
	v1Done := make(chan struct{})

	variants := []ActivityVariant{
		{
			Name: "slow-primary",
			Handler: func(ctx context.Context, _ ActivityRequest) (ActivityResult, error) {
				close(v1Started)
				select {
				case <-ctx.Done():
					return ActivityResult{}, ctx.Err()
				case <-v1Done:
					return ActivityResult{Facts: map[string]any{"v": 1}}, nil
				}
			},
		},
		{
			Name: "fast-hedge",
			Handler: func(_ context.Context, _ ActivityRequest) (ActivityResult, error) {
				close(v2Started)
				return ActivityResult{
					Facts:   map[string]any{"v": 2},
					Quality: QualityVector{"accuracy": 1.0},
				}, nil
			},
		},
	}

	hedged, err := NewHedgedActivity(variants, SpeculationPolicy{
		Pure:        true,
		HedgeDelay:  5 * time.Second,
		MaxParallel: 2,
		MinQuality:  0.8,
		Clock:       clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	resultCh := make(chan ActivityResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := hedged(context.Background(), ActivityRequest{})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- res
	}()

	// Wait until primary variant is actively executing
	select {
	case <-v1Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for primary variant to start")
	}

	// Verify hedge variant has not started yet
	select {
	case <-v2Started:
		t.Fatal("hedge variant launched before hedge delay")
	default:
	}

	// Advance clock by 5s to trigger hedge
	if err := clock.Advance(5 * time.Second); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-resultCh:
		select {
		case <-v2Started:
		default:
			t.Fatal("hedge variant was not launched")
		}
		if res.Facts["v"] != 2 {
			t.Fatalf("unexpected winner result: %+v", res)
		}
	case err := <-errCh:
		t.Fatalf("hedged activity failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for hedged activity result")
	}
}


func TestRetentionCollectExecutionsDeterministicClock(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := durabletime.NewManualClock(start)

	store := NewMemoryStore()
	ctx := context.Background()

	exec := &Execution{
		ID:          "retention-clock-1",
		PlanID:      "p",
		PlanVersion: "1",
		PlanDigest:  "d",
		Version:     1,
		Status:      StatusCompleted,
		CreatedAt:   start,
		UpdatedAt:   start,
	}
	ensureExecution(exec)
	if err := store.Create(ctx, exec); err != nil {
		t.Fatal(err)
	}

	// Policy: delete executions completed > 1 hour ago
	policy := RetentionPolicy{
		TerminalFor: 1 * time.Hour,
		Clock:       clock,
	}

	// At start + 30m: should NOT be deleted
	_ = clock.Advance(30 * time.Minute)
	res, err := CollectExecutions(ctx, store, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Deleted) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("expected 0 deleted, 1 skipped at 30m, got %+v", res)
	}

	// At start + 61m (past 1h): should be deleted
	_ = clock.Advance(31 * time.Minute)
	res, err = CollectExecutions(ctx, store, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Deleted) != 1 || res.Deleted[0] != "retention-clock-1" {
		t.Fatalf("expected 1 deleted at 61m, got %+v", res)
	}
}

func TestApplyRepairWithClockMaxDurationBound(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	plan, err := Compile(Definition{
		ID:      "repair-clock",
		Version: "1",
		Nodes: []Node{
			{
				ID:       "root",
				Kind:     NodeActivity,
				Activity: "root",
				Loop: &LoopBound{
					MaxIterations: 5,
					MaxDuration:   10 * time.Minute,
					Epsilon:       0.01,
				},

				Produces: []string{"data"},
			},
			{
				ID:        "gate",
				Kind:      NodeGate,
				Activity:  "gate",
				Requires:  []string{"data"},
				DependsOn: []string{"root"},
			},

		},
	})
	if err != nil {
		t.Fatal(err)
	}

	exec := &Execution{
		ID:               "exec-repair",
		PlanID:           plan.ID,
		PlanVersion:      plan.Version,
		PlanDigest:       plan.Digest,
		Version:          1,
		Status:           StatusRunning,
		CreatedAt:        start,
		RevisionCounters: map[string]int{},
		Nodes: map[string]*NodeRuntime{
			"root": {Status: NodeCompleted, Activated: true},
			"gate": {Status: NodeCompleted, Activated: true},
		},
	}
	ensureExecution(exec)

	repairPlan := RepairPlan{
		GateNode:      "gate",
		Roots:         []string{"root"},
		AffectedNodes: []string{"root", "gate"},
	}

	// Within 10m: repair succeeds
	if err := ApplyRepairWithClock(plan, exec, repairPlan, start.Add(5*time.Minute)); err != nil {
		t.Fatalf("repair within duration bound failed: %v", err)
	}

	// Beyond 10m: repair fails with duration bound error
	err = ApplyRepairWithClock(plan, exec, repairPlan, start.Add(11*time.Minute))
	if err == nil || !errors.Is(err, err) && err.Error() == "" {
		t.Fatal("expected duration bound error after 11m")
	}
}
