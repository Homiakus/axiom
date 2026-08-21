package adgo

import (
	"context"
	"math"
	"testing"
)

// CE-012: Providers [NaN, A] and [A, NaN] must produce identical deterministic results
// regardless of registration order (NaN provider is safely filtered out and never breaks total ordering).
func TestRouterRejectsNonFinitePermutation(t *testing.T) {
	ctx := context.Background()

	// Registry 1: NaN first, valid second
	reg1 := NewRegistry()
	reg1.Provider("cap", Provider{Name: "bad-nan", Quality: math.NaN(), Cost: 1.0})
	reg1.Provider("cap", Provider{Name: "good-p", Quality: 0.9, Cost: 1.0})

	// Registry 2: valid first, NaN second
	reg2 := NewRegistry()
	reg2.Provider("cap", Provider{Name: "good-p", Quality: 0.9, Cost: 1.0})
	reg2.Provider("cap", Provider{Name: "bad-nan", Quality: math.NaN(), Cost: 1.0})

	p1, err1 := reg1.Resolve(ctx, "cap", ProviderPolicy{})
	if err1 != nil {
		t.Fatalf("reg1.Resolve failed: %v", err1)
	}

	p2, err2 := reg2.Resolve(ctx, "cap", ProviderPolicy{})
	if err2 != nil {
		t.Fatalf("reg2.Resolve failed: %v", err2)
	}

	if p1.Name != "good-p" {
		t.Fatalf("expected reg1 to choose 'good-p', got %q", p1.Name)
	}
	if p2.Name != "good-p" {
		t.Fatalf("expected reg2 to choose 'good-p', got %q", p2.Name)
	}
	if p1.Name != p2.Name {
		t.Fatalf("permutation sensitivity detected: reg1 chose %q, reg2 chose %q", p1.Name, p2.Name)
	}
}

// TestSchedulerTieBreak verifies that UtilityScheduler has deterministic tie-breaking on node ID.
func TestSchedulerTieBreak(t *testing.T) {
	sched := DefaultScheduler()
	plan := &Plan{
		Nodes: map[string]Node{
			"node-b": {ID: "node-b"},
			"node-a": {ID: "node-a"},
		},
		descendants: map[string]map[string]struct{}{
			"node-b": {},
			"node-a": {},
		},
	}
	exec := &Execution{
		ID: "exec-1",
	}

	candidates := []Candidate{
		{Node: Node{ID: "node-b"}},
		{Node: Node{ID: "node-a"}},
	}

	selected := sched.Select(plan, exec, candidates)
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected candidates, got %d", len(selected))
	}
	// With equal scores, node-a should come before node-b deterministically
	if selected[0].Node.ID != "node-a" {
		t.Fatalf("expected deterministic tie-break to rank node-a first, got %q", selected[0].Node.ID)
	}
}
