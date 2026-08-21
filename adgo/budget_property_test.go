package adgo

import (
	"context"
	"math"
	"testing"
	"time"
)

// CE-007: Activity result returning NaN cost must be rejected without mutating execution budget.
func TestBudgetUsageMonotonic(t *testing.T) {
	def := Definition{
		ID:      "budget-plan",
		Version: "1.0",
		Nodes: []Node{
			{ID: "act", Kind: NodeActivity, Activity: "do-work"},
		},
	}
	plan, err := Compile(def)
	if err != nil {
		t.Fatalf("failed to compile plan: %v", err)
	}

	reg := NewRegistry()
	reg.Activity("do-work", func(ctx context.Context, req ActivityRequest) (ActivityResult, error) {
		return ActivityResult{
			Outcome: OutcomeCompleted,
			Budget: BudgetUsage{
				Cost: math.NaN(),
			},
		}, nil
	})

	store := NewMemoryStore()
	rt, err := NewRuntime(plan, store, reg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	ctx := context.Background()

	exec, err := rt.Start(ctx, "exec-1", nil, BudgetLimit{MaxCost: 10})
	if err != nil {
		t.Fatalf("failed to start runtime: %v", err)
	}

	// Manually seed initial cost
	_, err = store.Commit(ctx, "exec-1", exec.Version, func(e *Execution) error {
		e.BudgetUsage.Cost = 9.0
		return nil
	})
	if err != nil {
		t.Fatalf("failed to seed initial cost: %v", err)
	}

	// Run step: handler returns NaN cost
	finished, runErr := rt.Run(ctx, "exec-1")
	if finished == nil && runErr != nil {
		t.Fatalf("Run unexpected failure: %v", runErr)
	}

	// Verify that state did not become NaN
	loaded, err := store.Load(ctx, "exec-1")
	if err != nil {
		t.Fatalf("failed to load execution: %v", err)
	}
	if math.IsNaN(loaded.BudgetUsage.Cost) {
		t.Fatalf("execution budget cost was corrupted to NaN")
	}
	if loaded.BudgetUsage.Cost != 9.0 {
		t.Fatalf("expected budget cost to remain 9.0, got %f", loaded.BudgetUsage.Cost)
	}
}

// TestBudgetAdditionValidation verifies checked addition behavior for all BudgetUsage fields.
func TestBudgetAdditionValidation(t *testing.T) {
	dst := BudgetUsage{
		Cost:           5.0,
		Tokens:         100,
		ActiveDuration: 10 * time.Second,
		LLMCalls:       2,
		SearchQueries:  1,
		BrowserFetches: 1,
	}

	// Valid addition
	validInc := BudgetUsage{
		Cost:           2.5,
		Tokens:         50,
		ActiveDuration: 5 * time.Second,
		LLMCalls:       1,
		SearchQueries:  1,
		BrowserFetches: 1,
	}
	if err := addBudget(&dst, validInc); err != nil {
		t.Fatalf("expected valid addition to succeed, got: %v", err)
	}
	if dst.Cost != 7.5 || dst.Tokens != 150 || dst.ActiveDuration != 15*time.Second {
		t.Fatalf("unexpected destination values after valid addition: %+v", dst)
	}

	// Negative cost rejected
	if err := addBudget(&dst, BudgetUsage{Cost: -1}); err == nil {
		t.Fatalf("expected negative cost to be rejected")
	}

	// NaN cost rejected
	if err := addBudget(&dst, BudgetUsage{Cost: math.NaN()}); err == nil {
		t.Fatalf("expected NaN cost to be rejected")
	}

	// +Inf cost rejected
	if err := addBudget(&dst, BudgetUsage{Cost: math.Inf(1)}); err == nil {
		t.Fatalf("expected +Inf cost to be rejected")
	}
}

// TestBudgetExactlyAtLimit verifies boundary behavior when usage is exactly at or above limit.
func TestBudgetExactlyAtLimit(t *testing.T) {
	e := &Execution{
		BudgetLimit: BudgetLimit{MaxCost: 10.0},
		BudgetUsage: BudgetUsage{Cost: 10.0},
	}
	now := time.Now().UTC()
	if err := checkBudget(e, now); err != ErrBudgetExceeded {
		t.Fatalf("expected ErrBudgetExceeded when Cost == MaxCost, got %v", err)
	}

	e.BudgetUsage.Cost = 9.999
	if err := checkBudget(e, now); err != nil {
		t.Fatalf("expected nil error when Cost < MaxCost, got %v", err)
	}
}
