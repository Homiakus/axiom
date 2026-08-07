package adgo

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Explanation struct {
	ExecutionID string         `json:"executionId"`
	NodeID      string         `json:"nodeId,omitempty"`
	Status      string         `json:"status"`
	Reason      string         `json:"reason"`
	BlockedBy   []string       `json:"blockedBy,omitempty"`
	Evidence    map[string]any `json:"evidence,omitempty"`
}

func Explain(p *Plan, e *Execution, nodeID string) Explanation {
	ex := Explanation{ExecutionID: e.ID, NodeID: nodeID, Evidence: map[string]any{"planDigest": e.PlanDigest, "executionVersion": e.Version}}
	if nodeID == "" {
		ex.Status = string(e.Status)
		ex.Reason = executionReason(e)
		ex.Evidence["budgetUsage"] = e.BudgetUsage
		ex.Evidence["quality"] = e.Quality
		return ex
	}
	n, ok := p.Nodes[nodeID]
	if !ok {
		ex.Status = "unknown"
		ex.Reason = "node is not part of the pinned plan"
		return ex
	}
	rt := e.Nodes[nodeID]
	if rt == nil {
		ex.Status = "missing"
		ex.Reason = "execution has no runtime state for node"
		return ex
	}
	ex.Status = string(rt.Status)
	switch rt.Status {
	case NodeDormant:
		ex.Reason = "node has not been activated by a matching transition"
	case NodePending:
		if !rt.NotBefore.IsZero() && rt.NotBefore.After(time.Now().UTC()) {
			ex.Reason = "retry or timer backoff has not elapsed"
			ex.Evidence["notBefore"] = rt.NotBefore
		} else {
			for _, dep := range n.DependsOn {
				drt := e.Nodes[dep]
				if drt == nil || (drt.Status != NodeCompleted && drt.Status != NodeSkipped) {
					ex.BlockedBy = append(ex.BlockedBy, "node:"+dep)
				}
			}
			for _, key := range n.Requires {
				if _, ok := e.Data[key]; !ok {
					if _, ok := e.Artifacts[key]; !ok {
						ex.BlockedBy = append(ex.BlockedBy, "data:"+key)
					}
				}
			}
			if len(ex.BlockedBy) > 0 {
				sort.Strings(ex.BlockedBy)
				ex.Reason = "dependencies or required data are not satisfied"
			} else if e.StrategyBans[nodeID] {
				ex.Reason = "strategy is banned after oscillation or policy decision"
			} else {
				ex.Reason = "node is ready or waiting for scheduler capacity"
			}
		}
	case NodeRunning:
		ex.Reason = "activity is executing under a durable lease"
	case NodeWaiting:
		ex.Reason = "node is waiting for an external event, human decision, timer, or reconciliation"
		ex.Evidence["waitingFor"] = e.WaitingFor[nodeID]
	case NodeCompleted:
		ex.Reason = "node completed and its outcome was committed"
		ex.Evidence["outcome"] = rt.Outcome
		ex.Evidence["completedAt"] = rt.CompletedAt
	case NodeFailed:
		ex.Reason = "node exhausted its permitted recovery policy"
		ex.Evidence["failureClass"] = rt.LastFailure
		ex.Evidence["error"] = rt.LastError
	default:
		ex.Reason = "runtime state recorded"
	}
	ex.Evidence["attempts"] = rt.Attempts
	ex.Evidence["iterations"] = rt.Iterations
	return ex
}

func executionReason(e *Execution) string {
	switch e.Status {
	case StatusRunning:
		return "execution can continue"
	case StatusWaiting:
		return "execution is durably waiting"
	case StatusHuman:
		return "execution requires a human decision"
	case StatusCompensating:
		return "runtime is applying compensating actions"
	case StatusCompleted:
		return "all activated goals completed"
	case StatusCanceled:
		return "cancellation completed"
	case StatusFailed:
		if e.Failure != "" {
			return e.Failure
		}
		return "execution failed"
	case StatusDeadlocked:
		if e.Failure != "" {
			return e.Failure
		}
		return "no node can make progress"
	default:
		return fmt.Sprintf("execution status is %s", e.Status)
	}
}

func (e Explanation) String() string {
	parts := []string{fmt.Sprintf("%s: %s", e.Status, e.Reason)}
	if len(e.BlockedBy) > 0 {
		parts = append(parts, "blocked by "+strings.Join(e.BlockedBy, ", "))
	}
	return strings.Join(parts, "; ")
}
