package adgo

import (
	"strings"
	"testing"
	"time"
)

func TestCompileRejectsUnboundedCycle(t *testing.T) {
	_, err := Compile(Definition{ID: "cycle", Version: "1", Nodes: []Node{
		{ID: "a", Kind: NodeFork, Next: []Transition{{To: "b"}}},
		{ID: "b", Kind: NodeFork, Next: []Transition{{To: "a"}}},
	}})
	if err == nil || !strings.Contains(err.Error(), "ADG060") {
		t.Fatalf("expected ADG060, got %v", err)
	}
}

func TestCompileRejectsUnsafeExternalEffect(t *testing.T) {
	_, err := Compile(Definition{ID: "unsafe", Version: "1", Nodes: []Node{{ID: "publish", Kind: NodeActivity, Activity: "Publish", ExternalEffect: true, Risk: RiskHigh}}})
	if err == nil {
		t.Fatal("expected validation error")
	}
	text := err.Error()
	for _, code := range []string{"ADG030", "ADG031", "ADG032", "ADG033"} {
		if !strings.Contains(text, code) {
			t.Fatalf("expected %s in %s", code, text)
		}
	}
}

func TestCompileStableDigest(t *testing.T) {
	def := Definition{ID: "stable", Version: "7", GlobalConcurrency: 2, AllowedPermissions: []string{"net"}, Nodes: []Node{{ID: "a", Kind: NodeActivity, Activity: "A", Produces: []string{"x"}, Next: []Transition{{To: "b"}}}, {ID: "b", Kind: NodeActivity, Activity: "B", DependsOn: []string{"a"}, Requires: []string{"x"}}}}
	p1, err := Compile(def)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Compile(def)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Digest != p2.Digest {
		t.Fatalf("digest not stable: %s != %s", p1.Digest, p2.Digest)
	}
}

func TestCompileAcceptsBoundedRepairRoot(t *testing.T) {
	_, err := Compile(Definition{ID: "repair", Version: "1", Nodes: []Node{
		{ID: "draft", Kind: NodeActivity, Activity: "Draft", Loop: &LoopBound{MaxIterations: 3, MaxCost: 5, MaxDuration: time.Minute, Epsilon: .001}, Next: []Transition{{To: "gate"}}},
		{ID: "gate", Kind: NodeGate, DependsOn: []string{"draft"}, Gate: &QualityGateSpec{HardFloors: map[string]float64{"factuality": .95}, RepairFrom: []string{"draft"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlanDeltaValidatorRejectsUnauthorizedCapability(t *testing.T) {
	base, err := Compile(Definition{ID: "base", Version: "1", Nodes: []Node{{ID: "a", Kind: NodeActivity, Activity: "A", Next: []Transition{{To: "b"}}}, {ID: "b", Kind: NodeActivity, Activity: "B"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ValidatePlanDelta(base, PlanProposal{AttachAfter: "a", RejoinAt: "b", Nodes: []Node{{ID: "extra", Kind: NodeActivity, Capability: "WebSearch"}}}, PlanDeltaPolicy{AllowedCapabilities: map[string]bool{"LocalSearch": true}, MaxAddedNodes: 3, MaxRisk: RiskHigh})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected capability rejection, got %v", err)
	}
}

func TestValidatedDeltaCompilesAsImmutableChildPlan(t *testing.T) {
	base, err := Compile(Definition{ID: "base2", Version: "1", AllowedPermissions: []string{"net"}, Nodes: []Node{{ID: "a", Kind: NodeActivity, Activity: "A", Produces: []string{"evidence"}, Next: []Transition{{To: "b"}}}, {ID: "b", Kind: NodeActivity, Activity: "B"}}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := ValidatePlanDelta(base, PlanProposal{Reason: "need stronger evidence", AttachAfter: "a", RejoinAt: "b", Nodes: []Node{{ID: "search_legal", Kind: NodeActivity, Activity: "SearchLegal", Requires: []string{"evidence"}, RequiredPermissions: []string{"net"}}}}, PlanDeltaPolicy{AllowedActivities: map[string]bool{"SearchLegal": true}, AllowedPermissions: map[string]bool{"net": true}, MaxAddedNodes: 2, MaxRisk: RiskHigh})
	if err != nil {
		t.Fatal(err)
	}
	child, err := CompileValidatedPlanDelta(base, v)
	if err != nil {
		t.Fatal(err)
	}
	if child.Digest == base.Digest {
		t.Fatal("delta plan must have its own digest")
	}
	if child.Metadata["parentPlan"] != base.Digest {
		t.Fatalf("parent digest missing: %+v", child.Metadata)
	}
}
