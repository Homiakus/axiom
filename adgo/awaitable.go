package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type Awaitable struct {
	ID          string `json:"id"`
	ExecutionID string `json:"executionId"`
	NodeID      string `json:"nodeId"`
	EventType   string `json:"eventType"`
}

// Awaitable returns a stable callback token for a node that is durably waiting
// for an external event. The token includes the current node revision so a late
// callback from an obsolete repair iteration cannot accidentally resume newer
// work.
func (e *Engine) Awaitable(ctx context.Context, executionID, nodeID string) (Awaitable, error) {
	execution, err := e.store.Load(ctx, executionID)
	if err != nil {
		return Awaitable{}, err
	}
	expected := execution.WaitingFor[nodeID]
	runtime := execution.Nodes[nodeID]
	if expected == "" || runtime == nil || runtime.Status != NodeWaiting {
		return Awaitable{}, fmt.Errorf("adgo: node %s is not waiting for an external event", nodeID)
	}
	revision := execution.RevisionCounters[nodeID]
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d|%s", executionID, nodeID, expected, revision, execution.PlanDigest)))
	return Awaitable{
		ID:          "awake-" + hex.EncodeToString(sum[:16]),
		ExecutionID: executionID,
		NodeID:      nodeID,
		EventType:   expected,
	}, nil
}

// ResolveAwaitable durably stores callback data before writing the inbox event.
// If the process crashes between those operations, retrying with the same token
// is safe: the data patch is idempotent and the event id is stable.
func (e *Engine) ResolveAwaitable(ctx context.Context, awaitable Awaitable, payload any) error {
	current, err := e.Awaitable(ctx, awaitable.ExecutionID, awaitable.NodeID)
	if err != nil {
		return err
	}
	if current.ID != awaitable.ID || current.EventType != awaitable.EventType {
		return ErrStaleTask
	}
	if _, err := e.mutate(ctx, awaitable.ExecutionID, func(x *Execution) error {
		if x.WaitingFor[awaitable.NodeID] != awaitable.EventType {
			return ErrStaleTask
		}
		if err := SetData(x, "awaitable:"+awaitable.NodeID, payload); err != nil {
			return err
		}
		appendHistory(x, "awaitable_resolved", awaitable.NodeID, "external callback payload committed", map[string]any{"awaitable": awaitable.ID})
		return nil
	}); err != nil {
		return err
	}
	return e.runtime.Signal(ctx, awaitable.ExecutionID, Event{
		ID:         "event-" + awaitable.ID,
		Type:       awaitable.EventType,
		TargetNode: awaitable.NodeID,
		At:         time.Now().UTC(),
	})
}

func (e *Engine) RejectAwaitable(ctx context.Context, awaitable Awaitable, reason string) (*Execution, error) {
	current, err := e.Awaitable(ctx, awaitable.ExecutionID, awaitable.NodeID)
	if err != nil {
		return nil, err
	}
	if current.ID != awaitable.ID {
		return nil, ErrStaleTask
	}
	return e.mutate(ctx, awaitable.ExecutionID, func(x *Execution) error {
		if x.WaitingFor[awaitable.NodeID] != awaitable.EventType {
			return ErrStaleTask
		}
		runtime := x.Nodes[awaitable.NodeID]
		runtime.Status = NodeFailed
		runtime.LastError = reason
		delete(x.WaitingFor, awaitable.NodeID)
		x.Status = StatusFailed
		x.Failure = reason
		appendHistory(x, "awaitable_rejected", awaitable.NodeID, reason, map[string]any{"awaitable": awaitable.ID})
		return nil
	})
}
