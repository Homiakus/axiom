package adgo

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ChildHandle struct {
	ExecutionID string `json:"executionId"`
	PlanDigest  string `json:"planDigest"`
	ParentID    string `json:"parentId"`
	ParentNode  string `json:"parentNode"`
	ItemID      string `json:"itemId,omitempty"`
}

type ChildOptions struct {
	Initial map[string]any
	Budget  BudgetLimit
}

// ChildExecutionID returns the canonical deterministic identity for a child
// workflow. Parent redelivery with the same logical item therefore resumes the
// same child instead of creating duplicate work.
func ChildExecutionID(parentID, parentNode, itemID string) string {
	if itemID == "" {
		itemID = "child"
	}
	return parentID + "/" + safeName(parentNode) + "/" + safeName(itemID)
}

// StartChild creates or resumes a deterministic child execution. The ID is a
// function of parent execution, parent node and logical item id, so at-least-once
// parent activity delivery cannot duplicate the child workflow.
func (h *Host) StartChild(ctx context.Context, parentID, parentNode, itemID string, ref PlanRef, options ChildOptions) (ChildHandle, *Execution, error) {
	if parentID == "" || parentNode == "" {
		return ChildHandle{}, nil, fmt.Errorf("adgo: parent execution and node are required")
	}
	if itemID == "" {
		itemID = "child"
	}
	engine, err := h.Engine(ref)
	if err != nil {
		return ChildHandle{}, nil, err
	}
	childID := ChildExecutionID(parentID, parentNode, itemID)
	initial := make(map[string]any, len(options.Initial)+3)
	for key, value := range options.Initial {
		initial[key] = value
	}
	initial["__adgo:parentExecution"] = parentID
	initial["__adgo:parentNode"] = parentNode
	initial["__adgo:childItem"] = itemID
	if _, err := engine.StartOrLoad(ctx, childID, initial, options.Budget); err != nil {
		return ChildHandle{}, nil, err
	}
	if _, err := engine.Advance(ctx, childID); err != nil && !errors.Is(err, ErrDeadlock) {
		return ChildHandle{}, nil, err
	}
	execution, err := h.store.Load(ctx, childID)
	if err != nil {
		return ChildHandle{}, nil, err
	}
	return ChildHandle{ExecutionID: childID, PlanDigest: engine.plan.Digest, ParentID: parentID, ParentNode: parentNode, ItemID: itemID}, execution, nil
}

func (h *Host) Child(ctx context.Context, handle ChildHandle) (*Execution, error) {
	execution, err := h.store.Load(ctx, handle.ExecutionID)
	if err != nil {
		return nil, err
	}
	if handle.PlanDigest != "" && execution.PlanDigest != handle.PlanDigest {
		return nil, ErrStaleTask
	}
	return execution, nil
}

// WaitChild waits only on committed child state. It never re-runs completed
// child activities; any coordinator/worker process may progress the child.
func (h *Host) WaitChild(ctx context.Context, handle ChildHandle, poll time.Duration) (*Execution, error) {
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	for {
		execution, err := h.Child(ctx, handle)
		if err != nil {
			return nil, err
		}
		if terminal(execution.Status) || execution.Status == StatusHuman {
			return execution, nil
		}
		if _, err := h.Advance(ctx, handle.ExecutionID); err != nil && !errors.Is(err, ErrDeadlock) {
			return nil, err
		}
		if err := sleepContext(ctx, poll); err != nil {
			return nil, err
		}
	}
}

// StartChildren creates a bounded deterministic fan-out without blocking on the
// children. This is the production counterpart to embedded RunFanout.
func (h *Host) StartChildren(ctx context.Context, parentID, parentNode string, ref PlanRef, items []FanoutItem, maxFanout int, budget BudgetLimit) ([]ChildHandle, error) {
	if maxFanout <= 0 {
		return nil, fmt.Errorf("adgo: maxFanout must be > 0")
	}
	if len(items) > maxFanout {
		return nil, fmt.Errorf("adgo: fanout %d exceeds maxFanout %d", len(items), maxFanout)
	}
	handles := make([]ChildHandle, 0, len(items))
	for _, item := range items {
		if item.ID == "" {
			return nil, fmt.Errorf("adgo: fanout item id is required")
		}
		handle, _, err := h.StartChild(ctx, parentID, parentNode, item.ID, ref, ChildOptions{Initial: item.Initial, Budget: budget})
		if err != nil {
			return handles, err
		}
		handles = append(handles, handle)
	}
	return handles, nil
}

func (h *Host) InspectChildren(ctx context.Context, handles []ChildHandle, join JoinSpec) (FanoutResult, error) {
	result := FanoutResult{Children: make([]ChildResult, 0, len(handles))}
	for _, handle := range handles {
		execution, err := h.Child(ctx, handle)
		if err != nil {
			return result, err
		}
		child := ChildResult{ID: handle.ItemID, Status: execution.Status, Error: execution.Failure}
		result.Children = append(result.Children, child)
		switch execution.Status {
		case StatusCompleted:
			result.Completed++
		case StatusWaiting, StatusHuman, StatusRunning, StatusCompensating:
			result.Waiting++
		case StatusFailed, StatusDeadlocked, StatusCanceled:
			result.Failed++
		}
	}
	result.Satisfied = fanoutJoinSatisfied(result, len(handles), join)
	return result, nil
}
