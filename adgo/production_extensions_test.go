package adgo

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSchedulerReservesActiveAndBatchCost(t *testing.T) {
	plan, err := Compile(Definition{
		ID: "budget-admission", Version: "1", GlobalConcurrency: 3,
		Nodes: []Node{
			{ID: "active", Kind: NodeActivity, Activity: "a", EstimatedCost: 4},
			{ID: "one", Kind: NodeActivity, Activity: "b", EstimatedCost: 4},
			{ID: "two", Kind: NodeActivity, Activity: "c", EstimatedCost: 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	execution := &Execution{
		ID: "e", CreatedAt: time.Now(), BudgetLimit: BudgetLimit{MaxCost: 10},
		BudgetUsage: BudgetUsage{Cost: 1},
		ActiveTasks: map[string]TaskRuntime{"active": {ID: "active", NodeID: "active", Activity: "a", Status: TaskRunning}},
	}
	selected := DefaultScheduler().Select(plan, execution, []Candidate{
		{Node: plan.Nodes["one"], Activity: "b"},
		{Node: plan.Nodes["two"], Activity: "c"},
	})
	if len(selected) != 1 {
		t.Fatalf("selected=%d want 1; active+usage+one batch must reserve budget", len(selected))
	}
}

func TestDurableRouterHealthSurvivesRestart(t *testing.T) {
	registry := NewRegistry()
	registry.Provider("llm", Provider{Name: "primary", Activity: "primary", Quality: .95, Privacy: .9, Cost: .1})
	registry.Provider("llm", Provider{Name: "fallback", Activity: "fallback", Quality: .80, Privacy: .9, Cost: .1})
	store, err := NewFileProviderHealthStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := RouterConfig{FailureThreshold: 1, BaseCooldown: time.Minute, MaxCooldown: time.Minute, EWMAAlpha: .5}
	first := NewDurableAdaptiveRouter(registry, config, store)
	if err := first.ReportContext(context.Background(), "llm", "primary", time.Second, nil, Fail(FailureTransient, errors.New("down"))); err != nil {
		t.Fatal(err)
	}
	second := NewDurableAdaptiveRouter(registry, config, store)
	provider, err := second.Resolve(context.Background(), "llm", ProviderPolicy{AllowFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Name != "fallback" {
		t.Fatalf("provider=%s want fallback", provider.Name)
	}
}

func TestAdmissionControllerConcurrencyAndExpiry(t *testing.T) {
	controller := NewMemoryAdmissionController()
	policy := AdmissionPolicy{MaxConcurrent: 1}
	first, err := controller.Acquire(context.Background(), "openai", policy, 15*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Acquire(context.Background(), "openai", policy, time.Second); !errors.Is(err, ErrAdmissionDenied) {
		t.Fatalf("second acquire err=%v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := controller.Acquire(context.Background(), "openai", policy, time.Second); err != nil {
		t.Fatalf("expired permit should recover: %v", err)
	}
	if err := controller.Release(context.Background(), first); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionWrapperReturnsDurableRateLimit(t *testing.T) {
	controller := NewMemoryAdmissionController()
	policy := AdmissionPolicy{MaxConcurrent: 1}
	lease, err := controller.Acquire(context.Background(), "provider", policy, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Release(context.Background(), lease)
	wrapped := WithAdmission(controller, "provider", policy, time.Second, noopActivity)
	_, err = wrapped(context.Background(), ActivityRequest{})
	var failure *FailureError
	if !errors.As(err, &failure) || failure.Class != FailureRateLimit || failure.RetryAfter <= 0 {
		t.Fatalf("err=%v failure=%+v", err, failure)
	}
}

func TestHostRunsMultiplePlanVersions(t *testing.T) {
	store := NewMemoryStore()
	host, err := NewHost(store)
	if err != nil {
		t.Fatal(err)
	}
	planA, _ := Compile(Definition{ID: "flow", Version: "1", Nodes: []Node{{ID: "a", Kind: NodeActivity, Activity: "a"}}})
	planB, _ := Compile(Definition{ID: "flow", Version: "2", Nodes: []Node{{ID: "b", Kind: NodeActivity, Activity: "b"}}})
	regA := NewRegistry()
	regA.Activity("a", noopActivity)
	regB := NewRegistry()
	regB.Activity("b", noopActivity)
	if _, err := host.Register(planA, regA); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Register(planB, regB); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := host.Start(ctx, PlanRef{Digest: planA.Digest}, "a-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Start(ctx, PlanRef{Digest: planB.Digest}, "b-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Advance(ctx, "a-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Advance(ctx, "b-1"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 2 {
		item, err := host.Poll(ctx, WorkerSpec{ID: "w"})
		if err != nil {
			t.Fatal(err)
		}
		seen[item.Work.Token.ExecutionID] = true
		engine, _, err := host.engineForExecution(ctx, item.Work.Token.ExecutionID)
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.executeWorkItem(ctx, WorkerSpec{ID: "w"}, item.Work); err != nil {
			t.Fatal(err)
		}
	}
	if !seen["a-1"] || !seen["b-1"] {
		t.Fatalf("seen=%v", seen)
	}
}
