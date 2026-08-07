package adgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const executionControlNode = "$execution"

type HumanDecision string

const (
	HumanApprove HumanDecision = "approve"
	HumanEdit    HumanDecision = "edit"
	HumanReject  HumanDecision = "reject"
	HumanRetry   HumanDecision = "retry"
	HumanConfirm HumanDecision = "confirm"
	HumanAbort   HumanDecision = "abort"
)

// HumanResolution is the durable command used to resolve approvals, repair
// escalations, ambiguous side effects and explicit NodeHuman interrupts.
type HumanResolution struct {
	Decision HumanDecision  `json:"decision"`
	Actor    string         `json:"actor,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Patch    map[string]any `json:"patch,omitempty"`
	Payload  any            `json:"payload,omitempty"`
}

func (e *Engine) Pause(ctx context.Context, executionID, reason string) (*Execution, error) {
	return e.mutate(ctx, executionID, func(x *Execution) error {
		if terminal(x.Status) {
			return nil
		}
		if executionPaused(x) {
			return nil
		}
		x.WaitingFor[executionControlNode] = "ResumeRequested"
		x.Status = StatusWaiting
		appendHistory(x, "execution_paused", executionControlNode, reason, nil)
		return nil
	})
}

func (e *Engine) Resume(ctx context.Context, executionID, actor string) (*Execution, error) {
	return e.mutate(ctx, executionID, func(x *Execution) error {
		if !executionPaused(x) {
			return nil
		}
		delete(x.WaitingFor, executionControlNode)
		if !terminal(x.Status) {
			x.Status = StatusRunning
		}
		appendHistory(x, "execution_resumed", executionControlNode, "execution resumed", map[string]any{"actor": actor})
		return nil
	})
}

func (e *Engine) Cancel(ctx context.Context, executionID, reason string) (*Execution, error) {
	return e.mutate(ctx, executionID, func(x *Execution) error {
		if terminal(x.Status) {
			return nil
		}
		x.CancelRequested = true
		appendHistory(x, "cancellation_requested", executionControlNode, reason, nil)
		return nil
	})
}

func (e *Engine) UpdateBudget(ctx context.Context, executionID string, budget BudgetLimit, actor string) (*Execution, error) {
	return e.mutate(ctx, executionID, func(x *Execution) error {
		x.BudgetLimit = budget
		appendHistory(x, "budget_updated", executionControlNode, "budget policy updated", map[string]any{"actor": actor})
		return nil
	})
}

// PatchData applies operator-supplied durable facts. Reserved __adgo: keys are
// protected so callers cannot forge internal engine bookkeeping.
func (e *Engine) PatchData(ctx context.Context, executionID string, patch map[string]any, actor string) (*Execution, error) {
	return e.mutate(ctx, executionID, func(x *Execution) error {
		for key, value := range patch {
			if strings.HasPrefix(key, "__adgo:") {
				return fmt.Errorf("adgo: reserved data key %q", key)
			}
			if err := SetData(x, key, value); err != nil {
				return err
			}
		}
		appendHistory(x, "data_patched", executionControlNode, "durable facts patched", map[string]any{"actor": actor, "keys": sortedKeys(patch)})
		return nil
	})
}

func (e *Engine) Signal(ctx context.Context, executionID string, event Event) error {
	return e.runtime.Signal(ctx, executionID, event)
}

// SignalAndAdvance writes the signal first and then performs a coordinator tick,
// reducing wake-up latency while retaining durable inbox semantics.
func (e *Engine) SignalAndAdvance(ctx context.Context, executionID string, event Event) (AdvanceResult, error) {
	if err := e.Signal(ctx, executionID, event); err != nil {
		return AdvanceResult{}, err
	}
	return e.Advance(ctx, executionID)
}

func (e *Engine) ResolveHuman(ctx context.Context, executionID, nodeID string, resolution HumanResolution) (*Execution, error) {
	if strings.TrimSpace(nodeID) == "" {
		return nil, fmt.Errorf("adgo: human resolution node id is required")
	}
	if resolution.Decision == "" {
		return nil, fmt.Errorf("adgo: human resolution decision is required")
	}
	now := time.Now().UTC()
	return e.mutate(ctx, executionID, func(x *Execution) error {
		rt := x.Nodes[nodeID]
		if rt == nil || rt.Status != NodeWaiting {
			return fmt.Errorf("%w: node %s is not waiting", ErrStaleTask, nodeID)
		}
		expected := x.WaitingFor[nodeID]
		if expected == "" {
			return fmt.Errorf("adgo: node %s has no durable interrupt", nodeID)
		}
		for key, value := range resolution.Patch {
			if strings.HasPrefix(key, "__adgo:") {
				return fmt.Errorf("adgo: reserved data key %q", key)
			}
			if err := SetData(x, key, value); err != nil {
				return err
			}
		}
		if resolution.Payload != nil {
			if err := SetData(x, "human:"+nodeID+":payload", resolution.Payload); err != nil {
				return err
			}
		}

		historyData := map[string]any{"decision": resolution.Decision, "actor": resolution.Actor, "expected": expected}
		if resolution.Reason != "" {
			historyData["reason"] = resolution.Reason
		}

		switch {
		case strings.HasPrefix(expected, "Approve:"):
			switch resolution.Decision {
			case HumanApprove, HumanEdit, HumanRetry:
				if err := SetData(x, "approval:"+nodeID, true); err != nil {
					return err
				}
				rt.Status = NodePending
				rt.NotBefore = time.Time{}
				delete(x.WaitingFor, nodeID)
				x.Status = StatusRunning
				x.Metrics.HumanInterventions++
				appendHistory(x, "activity_approved", nodeID, "human approved or edited high-risk activity", historyData)
				return nil
			case HumanReject, HumanAbort:
				return rejectWaitingNode(e.plan, x, nodeID, resolution.Reason, historyData)
			default:
				return fmt.Errorf("adgo: decision %q is invalid for approval interrupt", resolution.Decision)
			}

		case strings.HasPrefix(expected, "Reconcile:"):
			switch resolution.Decision {
			case HumanRetry, HumanEdit:
				rt.Status = NodePending
				rt.NotBefore = time.Time{}
				delete(x.WaitingFor, nodeID)
				x.Status = StatusRunning
				appendHistory(x, "side_effect_retry", nodeID, "operator requested idempotent retry after reconciliation", historyData)
				return nil
			case HumanConfirm, HumanApprove:
				rt.Status = NodeCompleted
				rt.Outcome = OutcomeCompleted
				rt.CompletedAt = now
				delete(x.WaitingFor, nodeID)
				x.Status = StatusRunning
				activateNext(e.plan, x, nodeID, OutcomeCompleted)
				appendHistory(x, "side_effect_confirmed", nodeID, "operator confirmed external effect already completed", historyData)
				return nil
			case HumanAbort, HumanReject:
				rt.Status = NodeFailed
				rt.LastError = resolution.Reason
				x.Status = StatusFailed
				x.Failure = "ambiguous side effect rejected by operator"
				delete(x.WaitingFor, nodeID)
				appendHistory(x, "side_effect_aborted", nodeID, x.Failure, historyData)
				return nil
			default:
				return fmt.Errorf("adgo: decision %q is invalid for reconciliation interrupt", resolution.Decision)
			}

		case expected == "HumanRepairDecision" || expected == "OperatorRecoveryDecision":
			switch resolution.Decision {
			case HumanApprove, HumanRetry, HumanEdit:
				rt.Status = NodePending
				rt.NotBefore = time.Time{}
				delete(x.WaitingFor, nodeID)
				x.Status = StatusRunning
				x.Metrics.HumanInterventions++
				appendHistory(x, "operator_retry", nodeID, "operator released quarantined work", historyData)
				return nil
			case HumanReject, HumanAbort:
				rt.Status = NodeFailed
				delete(x.WaitingFor, nodeID)
				x.Status = StatusFailed
				x.Failure = "operator rejected repair/recovery continuation"
				appendHistory(x, "operator_rejected", nodeID, x.Failure, historyData)
				return nil
			default:
				return fmt.Errorf("adgo: decision %q is invalid for recovery interrupt", resolution.Decision)
			}
		}

		node, ok := e.plan.Nodes[nodeID]
		if !ok || node.Kind != NodeHuman {
			return fmt.Errorf("adgo: interrupt %q for node %s cannot be resolved with HumanResolution", expected, nodeID)
		}
		outcome := OutcomePass
		switch resolution.Decision {
		case HumanApprove, HumanEdit, HumanConfirm:
			outcome = OutcomePass
		case HumanReject:
			outcome = OutcomeRejected
		case HumanAbort:
			outcome = OutcomeCanceled
		case HumanRetry:
			rt.Status = NodePending
			delete(x.WaitingFor, nodeID)
			x.Status = StatusRunning
			appendHistory(x, "human_retry", nodeID, "human requested node retry", historyData)
			return nil
		default:
			return fmt.Errorf("adgo: unsupported human decision %q", resolution.Decision)
		}
		delete(x.WaitingFor, nodeID)
		x.Status = StatusRunning
		x.Metrics.HumanInterventions++
		if err := completeNode(e.plan, x, nodeID, outcome, ActivityResult{}); err != nil {
			return err
		}
		appendHistory(x, "human_resolved", nodeID, "human interrupt resolved", historyData)
		return nil
	})
}

func rejectWaitingNode(plan *Plan, execution *Execution, nodeID, reason string, historyData map[string]any) error {
	rt := execution.Nodes[nodeID]
	delete(execution.WaitingFor, nodeID)
	if hasOutcomeTransition(plan, nodeID, OutcomeRejected) {
		rt.Status = NodeCompleted
		rt.Outcome = OutcomeRejected
		rt.CompletedAt = time.Now().UTC()
		execution.Status = StatusRunning
		activateNext(plan, execution, nodeID, OutcomeRejected)
		appendHistory(execution, "activity_rejected", nodeID, reason, historyData)
		return nil
	}
	rt.Status = NodeFailed
	rt.Outcome = OutcomeRejected
	execution.Status = StatusFailed
	if strings.TrimSpace(reason) == "" {
		reason = "human rejected high-risk activity"
	}
	execution.Failure = reason
	appendHistory(execution, "activity_rejected", nodeID, reason, historyData)
	return nil
}

func hasOutcomeTransition(plan *Plan, nodeID string, outcome Outcome) bool {
	for _, transition := range plan.outbound[nodeID] {
		if transition.Outcome == outcome {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

var _ = errors.Is
