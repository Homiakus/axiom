package adgo

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// CE-005: Definition with EstimatedCost=NaN or Inf must be rejected by Compile with a validation error.
func TestCompileRejectsNonFinite(t *testing.T) {
	cases := []struct {
		name string
		node Node
	}{
		{
			name: "NaN EstimatedCost",
			node: Node{
				ID:            "n1",
				Kind:          NodeActivity,
				Activity:      "act1",
				EstimatedCost: math.NaN(),
			},
		},
		{
			name: "+Inf EstimatedCost",
			node: Node{
				ID:            "n1",
				Kind:          NodeActivity,
				Activity:      "act1",
				EstimatedCost: math.Inf(1),
			},
		},
		{
			name: "-Inf EstimatedCost",
			node: Node{
				ID:            "n1",
				Kind:          NodeActivity,
				Activity:      "act1",
				EstimatedCost: math.Inf(-1),
			},
		},
		{
			name: "NaN ExpectedQualityGain",
			node: Node{
				ID:                  "n1",
				Kind:                NodeActivity,
				Activity:            "act1",
				ExpectedQualityGain: math.NaN(),
			},
		},
		{
			name: "NaN CriticalPathWeight",
			node: Node{
				ID:                 "n1",
				Kind:               NodeActivity,
				Activity:           "act1",
				CriticalPathWeight: math.NaN(),
			},
		},
		{
			name: "NaN JitterFraction",
			node: Node{
				ID:       "n1",
				Kind:     NodeActivity,
				Activity: "act1",
				Retry:    RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, JitterFraction: math.NaN()},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := Definition{
				ID:      "plan-" + tc.name,
				Version: "1.0",
				Nodes:   []Node{tc.node},
			}
			_, err := Compile(def)
			if err == nil {
				t.Fatalf("expected Compile to fail for %s, but got nil error", tc.name)
			}
		})
	}
}

// CE-006: PlanProposal with EstimatedCost=NaN must be rejected by ValidatePlanDelta.
func TestPlanDeltaRejectsNonFinite(t *testing.T) {
	baseDef := Definition{
		ID:      "base-plan",
		Version: "1.0",
		Nodes: []Node{
			{ID: "start", Kind: NodeActivity, Activity: "act-start", Next: []Transition{{To: "end"}}},
			{ID: "end", Kind: NodeActivity, Activity: "act-end"},
		},
	}
	basePlan, err := Compile(baseDef)
	if err != nil {
		t.Fatalf("failed to compile base plan: %v", err)
	}

	proposal := PlanProposal{
		Reason:      "repair",
		AttachAfter: "start",
		RejoinAt:    "end",
		Nodes: []Node{
			{
				ID:            "adaptive-1",
				Kind:          NodeActivity,
				Activity:      "act-adaptive",
				EstimatedCost: math.NaN(),
			},
		},
	}
	policy := PlanDeltaPolicy{
		RemainingBudget: 10.0,
		AllowedActivities: map[string]bool{
			"act-adaptive": true,
		},
	}

	_, err = ValidatePlanDelta(basePlan, proposal, policy)
	if err == nil {
		t.Fatalf("expected ValidatePlanDelta to reject proposal with NaN EstimatedCost, got nil error")
	}
}

// BenchmarkCompileChain benchmarks compilation of deep linear chains of nodes.
func BenchmarkCompileChain(b *testing.B) {
	for _, n := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			nodes := make([]Node, n)
			for i := 0; i < n; i++ {
				nodes[i] = Node{
					ID:       fmt.Sprintf("node-%d", i),
					Kind:     NodeActivity,
					Activity: "act",
				}
				if i < n-1 {
					nodes[i].Next = []Transition{{To: fmt.Sprintf("node-%d", i+1)}}
				}
				if i > 0 {
					nodes[i].DependsOn = []string{fmt.Sprintf("node-%d", i-1)}
				}
			}
			def := Definition{
				ID:      "chain-plan",
				Version: "1.0",
				Nodes:   nodes,
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := Compile(def)
				if err != nil {
					b.Fatalf("compile failed: %v", err)
				}
			}
		})
	}
}
