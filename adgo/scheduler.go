package adgo

import (
	"math"
	"sort"
	"time"
)

type Candidate struct {
	Node     Node
	Activity string
	Score    float64
}

type Scheduler interface {
	Select(*Plan, *Execution, []Candidate) []Candidate
}

type UtilityScheduler struct {
	QualityWeight  float64
	CostWeight     float64
	LatencyWeight  float64
	RiskWeight     float64
	DeadlineWeight float64
	BlockedWeight  float64
}

func DefaultScheduler() UtilityScheduler {
	return UtilityScheduler{QualityWeight: 3, CostWeight: 1, LatencyWeight: .05, RiskWeight: .75, DeadlineWeight: 2, BlockedWeight: .25}
}

// Select performs both utility ranking and hard admission. Existing pending or
// running tasks count against every concurrency/resource limit, so a coordinator
// cannot oversubscribe the plan merely by scheduling another super-step while
// workers are still busy. Estimated cost is reserved for active and newly
// selected work, preventing one parallel batch from individually fitting but
// collectively exceeding the execution budget.
func (s UtilityScheduler) Select(p *Plan, e *Execution, in []Candidate) []Candidate {
	// A durable human interrupt is an execution-wide admission barrier for new
	// external work. readyCandidates may have derived candidates from the snapshot
	// that existed immediately before a gate/approval path changed the execution
	// to StatusHuman. Re-checking the committed status here prevents enqueue from
	// overwriting awaiting_human with running and preserves the interrupt until it
	// is explicitly resolved.
	if e != nil && e.Status == StatusHuman {
		return nil
	}

	now := time.Now().UTC()
	out := append([]Candidate(nil), in...)
	for i := range out {
		n := out[i].Node
		blocked := float64(len(p.descendants[n.ID]))
		deadlinePressure := 0.0
		if e.BudgetLimit.MaxDuration > 0 {
			elapsed := now.Sub(e.CreatedAt)
			remaining := e.BudgetLimit.MaxDuration - elapsed
			if remaining <= 0 {
				deadlinePressure = 1
			} else {
				deadlinePressure = 1 - (float64(remaining) / float64(e.BudgetLimit.MaxDuration))
				if deadlinePressure < 0 {
					deadlinePressure = 0
				}
			}
		}
		out[i].Score = n.CriticalPathWeight + s.QualityWeight*n.ExpectedQualityGain + s.BlockedWeight*blocked + s.DeadlineWeight*deadlinePressure - s.CostWeight*n.EstimatedCost - s.LatencyWeight*n.EstimatedLatency.Seconds() - s.RiskWeight*float64(n.Risk)
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := out[i].Score, out[j].Score
		if math.IsNaN(si) && math.IsNaN(sj) {
			return out[i].Node.ID < out[j].Node.ID
		}
		if math.IsNaN(si) {
			return false
		}
		if math.IsNaN(sj) {
			return true
		}
		if math.Abs(si-sj) < 1e-9 {
			return out[i].Node.ID < out[j].Node.ID
		}
		return si > sj
	})

	activityCount := map[string]int{}
	capCount := map[string]int{}
	resources := map[string]struct{}{}
	activeCount := 0
	reservedCost := 0.0
	for _, task := range e.ActiveTasks {
		if task.Status != TaskPending && task.Status != TaskRunning {
			continue
		}
		activeCount++
		activityCount[task.Activity]++
		if node, ok := p.Nodes[task.NodeID]; ok {
			reservedCost += node.EstimatedCost
			if node.Capability != "" {
				capCount[node.Capability]++
			}
			for _, key := range node.ResourceKeys {
				resources[key] = struct{}{}
			}
		}
	}

	limit := len(out)
	if p.GlobalConcurrency > 0 {
		limit = p.GlobalConcurrency - activeCount
		if limit < 0 {
			limit = 0
		}
		if limit > len(out) {
			limit = len(out)
		}
	}
	selected := make([]Candidate, 0, limit)
	selectedCost := 0.0
	for _, c := range out {
		if len(selected) >= limit {
			break
		}
		if max := p.ActivityLimits[c.Activity]; max > 0 && activityCount[c.Activity] >= max {
			continue
		}
		if c.Node.Capability != "" {
			if max := p.CapabilityLimits[c.Node.Capability]; max > 0 && capCount[c.Node.Capability] >= max {
				continue
			}
		}
		conflict := false
		for _, key := range c.Node.ResourceKeys {
			if _, exists := resources[key]; exists {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		if e.BudgetLimit.MaxCost > 0 && e.BudgetUsage.Cost+reservedCost+selectedCost+c.Node.EstimatedCost > e.BudgetLimit.MaxCost {
			continue
		}
		if e.BudgetLimit.MaxDuration > 0 && c.Node.EstimatedLatency > 0 && now.Sub(e.CreatedAt)+c.Node.EstimatedLatency > e.BudgetLimit.MaxDuration {
			continue
		}

		selected = append(selected, c)
		selectedCost += c.Node.EstimatedCost
		activityCount[c.Activity]++
		if c.Node.Capability != "" {
			capCount[c.Node.Capability]++
		}
		for _, key := range c.Node.ResourceKeys {
			resources[key] = struct{}{}
		}
	}
	return selected
}
