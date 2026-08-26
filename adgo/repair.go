package adgo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

type RepairPlan struct {
	GateNode            string    `json:"gateNode"`
	Roots               []string  `json:"roots"`
	AffectedNodes       []string  `json:"affectedNodes"`
	InvalidatedOutputs  []string  `json:"invalidatedOutputs"`
	PreservedNodes      []string  `json:"preservedNodes"`
	ExpectedImprovement float64   `json:"expectedImprovement"`
	ExpectedCost        float64   `json:"expectedCost"`
	Risk                RiskLevel `json:"risk"`
	Reason              string    `json:"reason"`
}

type RepairPlanner interface {
	PlanRepair(*Plan, *Execution, string, []Violation) (RepairPlan, error)
}

type DependencyRepairPlanner struct{}

func (DependencyRepairPlanner) PlanRepair(p *Plan, e *Execution, gateID string, violations []Violation) (RepairPlan, error) {
	gate, ok := p.Nodes[gateID]
	if !ok {
		return RepairPlan{}, fmt.Errorf("adgo: gate %q not in plan", gateID)
	}
	rootsSet := map[string]struct{}{}
	for _, v := range violations {
		for _, id := range v.RepairFrom {
			rootsSet[id] = struct{}{}
		}
		for _, key := range v.MissingData {
			for _, producer := range p.producers[key] {
				rootsSet[producer] = struct{}{}
			}
		}
	}
	if len(rootsSet) == 0 && gate.Gate != nil {
		for _, id := range gate.Gate.RepairFrom {
			rootsSet[id] = struct{}{}
		}
	}
	if len(rootsSet) == 0 {
		return RepairPlan{}, fmt.Errorf("adgo: no deterministic repair root for gate %q", gateID)
	}
	affected := map[string]struct{}{gateID: {}}
	invalid := map[string]struct{}{}
	cost := 0.0
	risk := RiskLow
	improvement := 0.0
	for root := range rootsSet {
		if _, ok := p.Nodes[root]; !ok {
			return RepairPlan{}, fmt.Errorf("adgo: repair root %q not in plan", root)
		}
		affected[root] = struct{}{}
		for id := range p.descendants[root] {
			if id == gateID {
				affected[id] = struct{}{}
				continue
			}
			if _, reachesGate := p.descendants[id][gateID]; reachesGate {
				affected[id] = struct{}{}
			}
		}
	}
	for id := range affected {
		n := p.Nodes[id]
		cost += n.EstimatedCost
		if n.Risk > risk {
			risk = n.Risk
		}
		improvement += n.ExpectedQualityGain
		for _, key := range n.Produces {
			invalid[key] = struct{}{}
		}
		for _, key := range n.Writes {
			invalid[key] = struct{}{}
		}
	}
	preserved := []string{}
	for id, rt := range e.Nodes {
		if rt.Status == NodeCompleted {
			if _, bad := affected[id]; !bad {
				preserved = append(preserved, id)
			}
		}
	}
	roots := keys(rootsSet)
	aff := keys(affected)
	inv := keys(invalid)
	sort.Strings(preserved)
	return RepairPlan{GateNode: gateID, Roots: roots, AffectedNodes: aff, InvalidatedOutputs: inv, PreservedNodes: preserved, ExpectedImprovement: improvement, ExpectedCost: cost, Risk: risk, Reason: "dependency-directed minimal repair"}, nil
}

func ApplyRepair(p *Plan, e *Execution, r RepairPlan) error {
	return ApplyRepairWithClock(p, e, r, time.Now().UTC())
}

func ApplyRepairWithClock(p *Plan, e *Execution, r RepairPlan, now time.Time) error {
	for _, root := range r.Roots {
		n := p.Nodes[root]

		b := n.Loop
		if b == nil {
			return fmt.Errorf("adgo: repair root %s has no loop bound", root)
		}
		next := e.RevisionCounters[root] + 1
		if next > b.MaxIterations {
			return fmt.Errorf("adgo: repair root %s exceeded max iterations %d", root, b.MaxIterations)
		}
		if b.MaxCost > 0 && float64(next)*maxFloat(n.EstimatedCost, 0.000001) > b.MaxCost {
			return fmt.Errorf("adgo: repair root %s exceeded repair cost bound", root)
		}
		if b.MaxDuration > 0 && now.Sub(e.CreatedAt) > b.MaxDuration {
			return fmt.Errorf("adgo: repair root %s exceeded repair duration bound", root)
		}
		if next > 1 && stagnatingAtNode(e.QualityHistory, r.GateNode, b.Epsilon) {
			return fmt.Errorf("adgo: repair root %s is not converging", root)
		}
		e.RevisionCounters[root] = next
	}
	invalidate := setOf(r.InvalidatedOutputs)
	for key := range invalidate {
		delete(e.Data, key)
		delete(e.Artifacts, key)
	}
	affected := setOf(r.AffectedNodes)
	for id := range affected {
		rt := e.Nodes[id]
		if rt == nil {
			continue
		}
		rt.Status = NodePending
		rt.Activated = true
		rt.Outcome = ""
		rt.CompletedAt = time.Time{}
		rt.LastError = ""
		rt.LastFailure = ""
		rt.NotBefore = time.Time{}
		rt.Iterations++
	}
	e.Metrics.Repairs++
	appendHistory(e, "repair_planned", r.GateNode, r.Reason, map[string]any{"roots": r.Roots, "affected": r.AffectedNodes, "preserved": r.PreservedNodes, "expectedCost": r.ExpectedCost, "risk": r.Risk.String()})
	return nil
}

func stagnatingAtNode(history []QualitySnapshot, nodeID string, epsilon float64) bool {
	if epsilon <= 0 {
		return false
	}
	values := make([]float64, 0, 3)
	for i := len(history) - 1; i >= 0 && len(values) < 3; i-- {
		if history[i].NodeID == nodeID {
			values = append(values, history[i].Utility)
		}
	}
	if len(values) < 3 {
		return false
	}
	newest, previous, older := values[0], values[1], values[2]
	return (previous-older) < epsilon && (newest-previous) < epsilon
}

func DetectOscillation(signatures []string) bool {
	n := len(signatures)
	if n < 4 {
		return false
	}
	for period := 2; period <= 3; period++ {
		if n < period*2 {
			continue
		}
		ok := true
		for i := 0; i < period; i++ {
			if signatures[n-1-i] != signatures[n-1-period-i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func QualityUtility(q QualityVector) float64 {
	if len(q) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range q {
		sum += v
	}
	return sum / float64(len(q))
}

// PlanProposal is information produced by an adaptive planner (including an LLM).
// It is inert data until ValidatePlanDelta accepts it.
type PlanProposal struct {
	Reason      string `json:"reason"`
	Nodes       []Node `json:"nodes"`
	AttachAfter string `json:"attachAfter"`
	RejoinAt    string `json:"rejoinAt"`
}
type PlanDeltaPolicy struct {
	AllowedActivities   map[string]bool
	AllowedCapabilities map[string]bool
	AllowedPermissions  map[string]bool
	MaxAddedNodes       int
	MaxRisk             RiskLevel
	RemainingBudget     float64
}
type ValidatedPlanDelta struct {
	Proposal PlanProposal
	Digest   string
}

func ValidatePlanDelta(base *Plan, proposal PlanProposal, policy PlanDeltaPolicy) (ValidatedPlanDelta, error) {
	if base == nil {
		return ValidatedPlanDelta{}, fmt.Errorf("adgo: base plan is required")
	}
	if proposal.AttachAfter == "" || proposal.RejoinAt == "" {
		return ValidatedPlanDelta{}, fmt.Errorf("adgo: attachAfter and rejoinAt are required")
	}
	if _, ok := base.Nodes[proposal.AttachAfter]; !ok {
		return ValidatedPlanDelta{}, fmt.Errorf("adgo: attach node does not exist")
	}
	if _, ok := base.Nodes[proposal.RejoinAt]; !ok {
		return ValidatedPlanDelta{}, fmt.Errorf("adgo: rejoin node does not exist")
	}
	if policy.MaxAddedNodes > 0 && len(proposal.Nodes) > policy.MaxAddedNodes {
		return ValidatedPlanDelta{}, fmt.Errorf("adgo: proposal exceeds max added nodes")
	}
	if math.IsNaN(policy.RemainingBudget) || math.IsInf(policy.RemainingBudget, 0) || policy.RemainingBudget < 0 {
		return ValidatedPlanDelta{}, fmt.Errorf("adgo: invalid remaining budget: %v", policy.RemainingBudget)
	}
	ids := map[string]bool{}
	cost := 0.0
	for _, n := range proposal.Nodes {
		if n.ID == "" || base.Nodes[n.ID].ID != "" || ids[n.ID] {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: invalid or duplicate proposed node %q", n.ID)
		}
		if math.IsNaN(n.EstimatedCost) || math.IsInf(n.EstimatedCost, 0) || n.EstimatedCost < 0 {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: proposed node %s has invalid estimated cost", n.ID)
		}
		if math.IsNaN(n.ExpectedQualityGain) || math.IsInf(n.ExpectedQualityGain, 0) || n.ExpectedQualityGain < 0 {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: proposed node %s has invalid expected quality gain", n.ID)
		}
		if math.IsNaN(n.CriticalPathWeight) || math.IsInf(n.CriticalPathWeight, 0) || n.CriticalPathWeight < 0 {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: proposed node %s has invalid critical path weight", n.ID)
		}
		ids[n.ID] = true
		if n.Risk > policy.MaxRisk {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: proposed node %s exceeds risk policy", n.ID)
		}
		if n.Activity != "" && !policy.AllowedActivities[n.Activity] {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: activity %s is not allowed", n.Activity)
		}
		if n.Capability != "" && !policy.AllowedCapabilities[n.Capability] {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: capability %s is not allowed", n.Capability)
		}
		for _, perm := range n.RequiredPermissions {
			if !policy.AllowedPermissions[perm] {
				return ValidatedPlanDelta{}, fmt.Errorf("adgo: permission %s is not allowed", perm)
			}
		}
		if n.ExternalEffect && (n.Timeout <= 0 || n.IdempotencyKey == "") {
			return ValidatedPlanDelta{}, fmt.Errorf("adgo: external proposed node %s lacks timeout/idempotency", n.ID)
		}
		cost += n.EstimatedCost
	}
	if policy.RemainingBudget > 0 && cost > policy.RemainingBudget {
		return ValidatedPlanDelta{}, ErrBudgetExceeded
	}
	raw, err := json.Marshal(proposal)
	if err != nil {
		return ValidatedPlanDelta{}, fmt.Errorf("adgo: failed to serialize proposal: %w", err)
	}
	sum := sha256.Sum256(raw)
	return ValidatedPlanDelta{Proposal: proposal, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

func keys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// CompileValidatedPlanDelta turns a validated adaptive proposal into an
// immutable child Plan. The parent Plan remains pinned and unchanged; the
// adaptive work therefore executes as a bounded subflow instead of allowing an
// LLM or other planner to mutate the running control graph in place.
func CompileValidatedPlanDelta(base *Plan, delta ValidatedPlanDelta) (*Plan, error) {
	if base == nil {
		return nil, fmt.Errorf("adgo: base plan is required")
	}
	if delta.Digest == "" {
		return nil, fmt.Errorf("adgo: validated delta digest is required")
	}
	initial := make([]string, 0, len(base.InitialData)+len(base.producers))
	for key := range base.InitialData {
		initial = append(initial, key)
	}
	for key := range base.producers {
		initial = append(initial, key)
	}
	sort.Strings(initial)
	permissions := keys(base.AllowedPermissions)
	suffix := delta.Digest
	if len(suffix) > 15 {
		suffix = suffix[len(suffix)-12:]
	}
	def := Definition{
		ID:                 base.ID + ".adaptive." + suffix,
		Version:            base.Version + "+delta",
		Nodes:              append([]Node(nil), delta.Proposal.Nodes...),
		InitialData:        initial,
		AllowedPermissions: permissions,
		GlobalConcurrency:  base.GlobalConcurrency,
		CapabilityLimits:   cloneIntMap(base.CapabilityLimits),
		ActivityLimits:     cloneIntMap(base.ActivityLimits),
		Metadata:           map[string]string{"parentPlan": base.Digest, "delta": delta.Digest, "reason": delta.Proposal.Reason},
	}
	return Compile(def)
}
