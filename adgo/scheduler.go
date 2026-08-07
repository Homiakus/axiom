package adgo

import (
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

func (s UtilityScheduler) Select(p *Plan, e *Execution, in []Candidate) []Candidate {
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
		if out[i].Score == out[j].Score {
			return out[i].Node.ID < out[j].Node.ID
		}
		return out[i].Score > out[j].Score
	})
	limit := p.GlobalConcurrency
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	selected := make([]Candidate, 0, limit)
	activityCount := map[string]int{}
	capCount := map[string]int{}
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
		for _, picked := range selected {
			if overlap(c.Node.ResourceKeys, picked.Node.ResourceKeys) {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		selected = append(selected, c)
		activityCount[c.Activity]++
		if c.Node.Capability != "" {
			capCount[c.Node.Capability]++
		}
	}
	return selected
}
