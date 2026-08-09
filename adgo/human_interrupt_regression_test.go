package adgo

import (
	"context"
	"testing"
	"time"
)

func repairEscalationFixture(t *testing.T) (*Plan, *Registry) {
	t.Helper()
	plan, err := Compile(Definition{
		ID:      "repair-escalation-human-interrupt",
		Version: "1",
		Nodes: []Node{
			{
				ID:       "root",
				Kind:     NodeActivity,
				Activity: "root.activity",
				Next:     []Transition{{To: "gate"}},
				Loop: &LoopBound{
					MaxIterations: 1,
					MaxCost:       10,
					MaxDuration:   time.Minute,
					Epsilon:       0.001,
				},
			},
			{
				ID:       "gate",
				Kind:     NodeGate,
				Activity: "gate.activity",
			},
		},
	})
	if err != nil {
		t.Fatalf("compile repair escalation fixture: %v", err)
	}
	registry := NewRegistry()
	registry.Activity("root.activity", func(context.Context, ActivityRequest) (ActivityResult, error) {
		return ActivityResult{}, nil
	})
	registry.Gate("gate.activity", func(context.Context, Snapshot) (GateResult, error) {
		return GateResult{
			Outcome: OutcomeRepair,
			Violations: []Violation{{
				Code:       "force_repair",
				Message:    "force repair until bounded loop escalates",
				RepairFrom: []string{"root"},
			}},
		}, nil
	})
	return plan, registry
}

func TestEnginePreservesHumanRepairEscalation(t *testing.T) {
	plan, registry := repairEscalationFixture(t)
	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, registry, WithEnginePollInterval(time.Millisecond))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := engine.Start(ctx, "engine-human-repair", nil, BudgetLimit{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	final, err := engine.RunLocal(ctx, "engine-human-repair", LocalRunOptions{Worker: WorkerSpec{ID: "local", Concurrency: 1, PollInterval: time.Millisecond, LeaseTTL: time.Second}})
	if err != nil {
		t.Fatalf("RunLocal: %v", err)
	}
	assertHumanRepairInterrupt(t, final)

	result, err := engine.Advance(ctx, final.ID)
	if err != nil {
		t.Fatalf("Advance while awaiting human decision: %v", err)
	}
	if result.Status != StatusHuman || result.Progressed {
		t.Fatalf("Advance result=%+v want awaiting_human without progress", result)
	}
	after, err := engine.Get(ctx, final.ID)
	if err != nil {
		t.Fatalf("get after repeated Advance: %v", err)
	}
	assertHumanRepairInterrupt(t, after)
}

func TestRuntimePreservesHumanRepairEscalation(t *testing.T) {
	plan, registry := repairEscalationFixture(t)
	store := NewMemoryStore()
	runtime, err := NewRuntime(plan, store, registry, WithLeaseTTL(time.Second))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := runtime.Start(ctx, "runtime-human-repair", nil, BudgetLimit{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	final, err := runtime.Run(ctx, "runtime-human-repair")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertHumanRepairInterrupt(t, final)

	step, err := runtime.Step(ctx, final.ID)
	if err != nil {
		t.Fatalf("Step while awaiting human decision: %v", err)
	}
	if step.Status != StatusHuman || step.Progressed {
		t.Fatalf("Step result=%+v want awaiting_human without progress", step)
	}
	after, err := store.Load(ctx, final.ID)
	if err != nil {
		t.Fatalf("load after repeated Step: %v", err)
	}
	assertHumanRepairInterrupt(t, after)
}

func assertHumanRepairInterrupt(t *testing.T, execution *Execution) {
	t.Helper()
	if execution == nil {
		t.Fatal("execution is nil")
	}
	if execution.Status != StatusHuman {
		t.Fatalf("status=%s want %s; failure=%q", execution.Status, StatusHuman, execution.Failure)
	}
	if execution.Failure != "" {
		t.Errorf("human repair interrupt must not have terminal failure: %q", execution.Failure)
	}
	if got := execution.WaitingFor["gate"]; got != "HumanRepairDecision" {
		t.Errorf("waitingFor[gate]=%q want HumanRepairDecision", got)
	}
	if rt := execution.Nodes["gate"]; rt == nil || rt.Status != NodeWaiting {
		t.Errorf("gate runtime=%+v want waiting", rt)
	}
	if got := execution.RevisionCounters["root"]; got != 1 {
		t.Errorf("root revision counter=%d want 1", got)
	}
	for _, entry := range execution.History {
		if entry.Type == "deadlock" {
			t.Fatalf("human repair escalation was overwritten by deadlock history: %+v", entry)
		}
	}
}
