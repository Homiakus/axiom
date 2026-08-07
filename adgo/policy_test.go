package adgo

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPolicyEngineHumanEscalationThenAllow(t *testing.T) {
	plan, err := Compile(Definition{ID: "policy", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, NewRegistry(), WithEngineLeaseTTL(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var allow atomic.Bool
	policy, err := NewPolicyEngine(engine, RuntimePolicyFunc(func(context.Context, PolicyRequest) (PolicyDecision, error) {
		if allow.Load() {
			return PolicyDecision{Action: PolicyAllow, Reason: "operator override active"}, nil
		}
		return PolicyDecision{Action: PolicyHuman, Reason: "tenant policy requires approval"}, nil
	}), PolicyEngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := engine.Start(ctx, "policy-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "policy-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Poll(ctx, WorkerSpec{ID: "worker"}); !errors.Is(err, ErrNoWork) {
		t.Fatalf("poll after human escalation err=%v", err)
	}
	execution, err := store.Load(ctx, "policy-1")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != StatusHuman || execution.WaitingFor["work"] != "PolicyDecision:work" {
		t.Fatalf("status=%s waiting=%v", execution.Status, execution.WaitingFor)
	}
	allow.Store(true)
	if _, err := policy.Resolve(ctx, "policy-1", "work", true, "operator", "approved tenant exception", map[string]any{"policyApproved": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "policy-1"); err != nil {
		t.Fatal(err)
	}
	work, err := policy.Poll(ctx, WorkerSpec{ID: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if work.Node.ID != "work" || work.Token.Attempt < 2 {
		t.Fatalf("work=%+v", work)
	}
	if _, err := engine.Complete(ctx, work.Token, ActivityResult{}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "policy-1"); err != nil {
		t.Fatal(err)
	}
	execution, _ = store.Load(ctx, "policy-1")
	if execution.Status != StatusCompleted {
		t.Fatalf("status=%s", execution.Status)
	}
	if _, ok := execution.Data["policyApproved"]; !ok {
		t.Fatal("operator patch missing")
	}
}

func TestPolicyEngineDelayReturnsTaskToDurableQueue(t *testing.T) {
	plan, err := Compile(Definition{ID: "policy-delay", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, _ := NewEngine(plan, store, NewRegistry())
	policy, _ := NewPolicyEngine(engine, RuntimePolicyFunc(func(context.Context, PolicyRequest) (PolicyDecision, error) {
		return PolicyDecision{Action: PolicyDelay, Reason: "maintenance window", RetryAfter: 50 * time.Millisecond}, nil
	}), PolicyEngineOptions{MaxPolicySkipsPerPoll: 1})
	ctx := context.Background()
	_, _ = engine.Start(ctx, "delay-1", nil, BudgetLimit{})
	_, _ = engine.Advance(ctx, "delay-1")
	if _, err := policy.Poll(ctx, WorkerSpec{ID: "worker"}); !errors.Is(err, ErrNoWork) {
		t.Fatalf("err=%v", err)
	}
	execution, _ := store.Load(ctx, "delay-1")
	if len(execution.ActiveTasks) != 0 || execution.Nodes["work"].Status != NodePending || !execution.Nodes["work"].NotBefore.After(time.Now().UTC()) {
		t.Fatalf("execution=%+v", execution)
	}
}
