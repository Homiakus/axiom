package adgo

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestIndependentRepairAnchors documents the pattern used by clients that have
// multiple gates repairing the same downstream work but require independent
// durable loop budgets. Each repair anchor owns its revision counter and emits
// an epoch fact, so descendant activities receive a new logical input identity
// without coupling unrelated gate budgets.
func TestIndependentRepairAnchors(t *testing.T) {
	boundA := &LoopBound{MaxIterations: 1, MaxCost: 10, MaxDuration: time.Minute, Epsilon: .0001}
	boundB := &LoopBound{MaxIterations: 2, MaxCost: 10, MaxDuration: time.Minute, Epsilon: .0001}
	plan, err := Compile(Definition{ID: "independent-repair-anchors", Version: "1", Nodes: []Node{
		{ID: "start", Kind: NodeActivity, Activity: "start", Next: []Transition{{To: "anchorA"}}},
		{ID: "anchorA", Kind: NodeActivity, Activity: "anchorA", DependsOn: []string{"start"}, Produces: []string{"epochA"}, IdempotencyKey: "{execution}:{node}:{revision}", EstimatedCost: .01, Loop: boundA, Next: []Transition{{To: "anchorB"}}},
		{ID: "anchorB", Kind: NodeActivity, Activity: "anchorB", DependsOn: []string{"anchorA"}, Produces: []string{"epochB"}, IdempotencyKey: "{execution}:{node}:{revision}", EstimatedCost: .01, Loop: boundB, Next: []Transition{{To: "draft"}}},
		{ID: "draft", Kind: NodeActivity, Activity: "draft", DependsOn: []string{"anchorB"}, Produces: []string{"draftVersion"}, Next: []Transition{{To: "verifyA"}}},
		{ID: "verifyA", Kind: NodeActivity, Activity: "verifyA", DependsOn: []string{"draft"}, Next: []Transition{{To: "gateA"}}},
		{ID: "gateA", Kind: NodeGate, Activity: "gateA", DependsOn: []string{"verifyA"}, Gate: &QualityGateSpec{RepairFrom: []string{"anchorA"}}, Next: []Transition{{To: "verifyB", Outcome: OutcomePass}}},
		{ID: "verifyB", Kind: NodeActivity, Activity: "verifyB", DependsOn: []string{"gateA"}, Next: []Transition{{To: "gateB"}}},
		{ID: "gateB", Kind: NodeGate, Activity: "gateB", DependsOn: []string{"verifyB"}, Gate: &QualityGateSpec{RepairFrom: []string{"anchorB"}}, Next: []Transition{{To: "done", Outcome: OutcomePass}}},
		{ID: "done", Kind: NodeActivity, Activity: "done", DependsOn: []string{"gateB"}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	reg.Activity("start", noopActivity)
	reg.Activity("verifyA", noopActivity)
	reg.Activity("verifyB", noopActivity)
	reg.Activity("done", noopActivity)
	reg.Activity("anchorA", func(_ context.Context, req ActivityRequest) (ActivityResult, error) {
		return ActivityResult{Facts: map[string]any{"epochA": req.IdempotencyKey}}, nil
	})
	reg.Activity("anchorB", func(_ context.Context, req ActivityRequest) (ActivityResult, error) {
		return ActivityResult{Facts: map[string]any{"epochB": req.IdempotencyKey}}, nil
	})

	type epochs struct{ a, b string }
	draftInputs := []epochs{}
	reg.Activity("draft", func(_ context.Context, req ActivityRequest) (ActivityResult, error) {
		var a, b string
		if err := json.Unmarshal(req.Data["epochA"], &a); err != nil {
			return ActivityResult{}, err
		}
		if err := json.Unmarshal(req.Data["epochB"], &b); err != nil {
			return ActivityResult{}, err
		}
		draftInputs = append(draftInputs, epochs{a: a, b: b})
		return ActivityResult{Facts: map[string]any{"draftVersion": len(draftInputs)}}, nil
	})

	gateACalls := 0
	reg.Gate("gateA", func(context.Context, Snapshot) (GateResult, error) {
		gateACalls++
		if gateACalls == 1 {
			return GateResult{Outcome: OutcomeRepair, Violations: []Violation{{Code: "gate_a", RepairFrom: []string{"anchorA"}}}}, nil
		}
		return GateResult{Outcome: OutcomePass}, nil
	})
	gateBCalls := 0
	reg.Gate("gateB", func(context.Context, Snapshot) (GateResult, error) {
		gateBCalls++
		if gateBCalls <= 2 {
			return GateResult{Outcome: OutcomeRepair, Violations: []Violation{{Code: "gate_b", RepairFrom: []string{"anchorB"}}}}, nil
		}
		return GateResult{Outcome: OutcomePass}, nil
	})

	store := NewMemoryStore()
	rt, err := NewRuntime(plan, store, reg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Start(context.Background(), "anchors-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	exec, err := rt.Run(context.Background(), "anchors-1")
	if err != nil {
		t.Fatal(err)
	}
	if exec.Status != StatusCompleted {
		t.Fatalf("status=%s failure=%s", exec.Status, exec.Failure)
	}
	if got := len(draftInputs); got != 4 {
		t.Fatalf("draft runs=%d want 4", got)
	}
	if exec.RevisionCounters["anchorA"] != 1 {
		t.Fatalf("anchorA revisions=%d want 1", exec.RevisionCounters["anchorA"])
	}
	if exec.RevisionCounters["anchorB"] != 2 {
		t.Fatalf("anchorB revisions=%d want 2", exec.RevisionCounters["anchorB"])
	}

	if draftInputs[0].a == draftInputs[1].a {
		t.Fatal("gate A repair did not change anchor A epoch")
	}
	if draftInputs[0].b != draftInputs[1].b {
		t.Fatal("gate A repair unexpectedly changed anchor B revision identity")
	}
	if draftInputs[1].a != draftInputs[2].a || draftInputs[2].a != draftInputs[3].a {
		t.Fatal("gate B repairs unexpectedly changed anchor A revision identity")
	}
	if draftInputs[1].b == draftInputs[2].b || draftInputs[2].b == draftInputs[3].b {
		t.Fatal("gate B repairs did not advance anchor B epoch")
	}
}
