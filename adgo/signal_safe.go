package adgo

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type SignalPolicy struct {
	AllowBroadcast bool
	PersistPayload bool
}

// SignalDeterministic resolves an untargeted external event only when the target
// is unambiguous. This avoids map-iteration-dependent delivery when multiple
// nodes wait for the same event type. Optional payload persistence commits data
// before the inbox event, making callback retries crash-safe.
func (e *Engine) SignalDeterministic(ctx context.Context, executionID string, event Event, policy SignalPolicy) error {
	execution, err := e.store.Load(ctx, executionID)
	if err != nil {
		return err
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}

	targets := []string{}
	if event.TargetNode != "" {
		if execution.WaitingFor[event.TargetNode] != event.Type {
			return fmt.Errorf("adgo: node %s is not waiting for event %s", event.TargetNode, event.Type)
		}
		targets = append(targets, event.TargetNode)
	} else {
		for nodeID, expected := range execution.WaitingFor {
			if nodeID == executionControlNode || expected != event.Type {
				continue
			}
			targets = append(targets, nodeID)
		}
		sort.Strings(targets)
		if len(targets) == 0 {
			// System events such as cancellation/budget changes are intentionally
			// not tied to a waiting node and remain valid through the base inbox.
			return e.runtime.Signal(ctx, executionID, event)
		}
		if len(targets) > 1 && !policy.AllowBroadcast {
			return fmt.Errorf("adgo: event %q is ambiguous across waiting nodes %v; set TargetNode or AllowBroadcast", event.Type, targets)
		}
	}

	var payload any
	if policy.PersistPayload && len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return fmt.Errorf("adgo: decode signal payload: %w", err)
		}
	}

	for index, target := range targets {
		if policy.PersistPayload && len(event.Payload) > 0 {
			if _, err := e.mutate(ctx, executionID, func(x *Execution) error {
				if x.WaitingFor[target] != event.Type {
					return ErrStaleTask
				}
				if err := SetData(x, "event:"+target+":"+event.Type, payload); err != nil {
					return err
				}
				appendHistory(x, "event_payload_committed", target, "external event payload committed", map[string]any{"event": event.Type})
				return nil
			}); err != nil {
				return err
			}
		}
		copy := event
		copy.TargetNode = target
		if copy.ID == "" {
			copy.ID = eventID(copy)
		}
		if len(targets) > 1 {
			copy.ID = fmt.Sprintf("%s-%d", copy.ID, index+1)
		}
		if err := e.runtime.Signal(ctx, executionID, copy); err != nil {
			return err
		}
	}
	return nil
}
