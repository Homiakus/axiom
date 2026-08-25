package axiom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

const (
	flowHistoryEffectPending   = "EffectPending"
	flowHistoryEffectCompleted = "EffectCompleted"
)

// DurableFlowStore is the storage capability required by
// WithDurableFlowEffects. SaveStateAndAppend must atomically commit the state
// bytes and every supplied history entry as one durable unit.
type DurableFlowStore interface {
	FlowStore
	IncrementalFlowStore
	DurabilityProvider
	AtomicFlowCommit()
}

// FlowEffectIntent is the durable outbox representation of an emitted effect.
// ID is deterministic for a flow execution, handled-event sequence, and effect
// position. Payload contains the canonical JSON command to deliver.
type FlowEffectIntent struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// FlowEffectCompletion records the durable acknowledgement for an outbox item.
type FlowEffectCompletion struct {
	ID string `json:"id"`
}

// FlowEffectDeliveryError means reducer state and the effect intent are already
// committed, but the external handler failed. Retrying the business event would
// apply the reducer again; call DrainEffects to retry only the pending effect.
type FlowEffectDeliveryError struct {
	EffectID string
	Name     string
	Err      error
}

func (e *FlowEffectDeliveryError) Error() string {
	return fmt.Sprintf("axiom: durable flow effect %s (%s) delivery failed after state commit: %v", e.EffectID, e.Name, e.Err)
}

func (e *FlowEffectDeliveryError) Unwrap() error { return e.Err }
func (e *FlowEffectDeliveryError) StateCommitted() bool { return true }

// FlowEffectAcknowledgeError means the effect handler returned success but its
// durable completion marker could not be committed. The effect may be delivered
// again; handlers should deduplicate using FlowEffectIDFromContext.
type FlowEffectAcknowledgeError struct {
	EffectID string
	Name     string
	Err      error
}

func (e *FlowEffectAcknowledgeError) Error() string {
	return fmt.Sprintf("axiom: durable flow effect %s (%s) acknowledgement failed; delivery may repeat: %v", e.EffectID, e.Name, e.Err)
}

func (e *FlowEffectAcknowledgeError) Unwrap() error { return e.Err }
func (e *FlowEffectAcknowledgeError) StateCommitted() bool { return true }

type flowEffectIDContextKey struct{}

// FlowEffectIDFromContext returns the stable idempotency key of a durable Flow
// effect delivery. It is present while an EffectHandler is invoked by a Flow
// opened with WithDurableFlowEffects.
func FlowEffectIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(flowEffectIDContextKey{}).(string)
	return id, ok && id != ""
}

func durableFlowEffectID(flow, executionID string, eventSequence, effectIndex int) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(flow))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(executionID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strconv.Itoa(eventSequence)))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(strconv.Itoa(effectIndex)))
	return hex.EncodeToString(hash.Sum(nil))
}

func validateDurableFlowEffectTypes[S any](flow *Flow[S]) error {
	seen := make(map[string]reflect.Type, len(flow.effects))
	for typ := range flow.effects {
		name := durableEffectTypeName(typ)
		if name == "" {
			return fmt.Errorf("axiom: durable flow effect type %s must be a named Go type", typ)
		}
		if previous, ok := seen[name]; ok && previous != typ {
			return fmt.Errorf("axiom: durable flow effect type name %q is ambiguous between %s and %s", name, previous, typ)
		}
		seen[name] = typ
	}
	return nil
}

func durableEffectTypeName(typ reflect.Type) string {
	if typ == nil {
		return ""
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Name() == "" {
		return ""
	}
	if typ.PkgPath() == "" {
		return typ.Name()
	}
	return typ.PkgPath() + "." + typ.Name()
}

func (e *FlowExecution[S]) dispatchDurableLocked(ctx context.Context, event any) error {
	store := e.engine.store.(DurableFlowStore)

	// Preserve delivery order across crashes: previously committed effects must
	// be acknowledged before a later business event is reduced.
	if err := e.drainDurableEffectsLocked(ctx, store); err != nil {
		return err
	}

	handler, normalized, err := e.engine.flow.handlerFor(event)
	if err != nil {
		return err
	}
	state, _, historyLength, err := e.loadForDispatch(ctx)
	if err != nil {
		return err
	}
	result, err := handler(ctx, state, normalized)
	if err != nil {
		return err
	}
	for _, claim := range e.engine.flow.claims {
		if err := claim(result.State); err != nil {
			return fmt.Errorf("axiom: claim failed: %w", err)
		}
	}

	eventSequence := historyLength + 1
	entries := make([]FlowHistoryEntry, 0, 1+len(result.Effects))
	entries = append(entries, FlowHistoryEntry{
		Sequence:  eventSequence,
		Type:      "EventHandled",
		Name:      typeName(normalized),
		Data:      normalized,
		CreatedAt: time.Now().UTC(),
	})
	for index, effect := range result.Effects {
		_, command, err := e.engine.flow.effectFor(effect.Command)
		if err != nil {
			return err
		}
		name := typeName(command)
		if name == "" {
			return fmt.Errorf("axiom: durable flow effect command %T must be a named Go type", command)
		}
		payload, err := json.Marshal(command)
		if err != nil {
			return fmt.Errorf("axiom: encode durable flow effect %s: %w", name, err)
		}
		intent := FlowEffectIntent{
			ID:      durableFlowEffectID(e.engine.flow.name, e.id, eventSequence, index),
			Name:    name,
			Payload: append(json.RawMessage(nil), payload...),
		}
		entries = append(entries, FlowHistoryEntry{
			Sequence:  historyLength + len(entries) + 1,
			Type:      flowHistoryEffectPending,
			Name:      name,
			Data:      intent,
			CreatedAt: time.Now().UTC(),
		})
	}

	data, err := json.Marshal(result.State)
	if err != nil {
		return err
	}
	// Transactional-outbox boundary: state and effect intents become durable
	// before any external effect is allowed to run.
	if err := store.SaveStateAndAppend(ctx, e.engine.flow.name, e.id, data, entries); err != nil {
		return err
	}
	return e.drainDurableEffectsLocked(ctx, store)
}

// DrainEffects retries durable outbox items that have no EffectCompleted
// acknowledgement. Delivery is at-least-once; use FlowEffectIDFromContext as
// the downstream idempotency key when exactly-once business effects matter.
func (e *FlowExecution[S]) DrainEffects(ctx context.Context) error {
	if e == nil || e.engine == nil || e.id == "" {
		return fmt.Errorf("axiom: valid flow execution is required")
	}
	if !e.engine.durableEffects {
		return fmt.Errorf("axiom: flow was not opened with WithDurableFlowEffects")
	}
	unlock := e.engine.locks.Lock(e.id)
	defer unlock()
	return e.drainDurableEffectsLocked(ctx, e.engine.store.(DurableFlowStore))
}

func (e *FlowExecution[S]) drainDurableEffectsLocked(ctx context.Context, store DurableFlowStore) error {
	state, historyLength, found, err := store.LoadState(ctx, e.engine.flow.name, e.id)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	history, err := store.LoadHistory(ctx, e.engine.flow.name, e.id)
	if err != nil {
		return err
	}
	if historyLength != len(history) {
		return fmt.Errorf("axiom: durable flow store history length mismatch: state reports %d, history has %d", historyLength, len(history))
	}

	completed := make(map[string]struct{})
	for _, entry := range history {
		if entry.Type != flowHistoryEffectCompleted {
			continue
		}
		completion, err := decodeFlowHistoryData[FlowEffectCompletion](entry.Data)
		if err != nil {
			return fmt.Errorf("axiom: decode durable flow effect completion at sequence %d: %w", entry.Sequence, err)
		}
		completed[completion.ID] = struct{}{}
	}

	for _, entry := range history {
		if entry.Type != flowHistoryEffectPending {
			continue
		}
		intent, err := decodeFlowHistoryData[FlowEffectIntent](entry.Data)
		if err != nil {
			return fmt.Errorf("axiom: decode durable flow effect intent at sequence %d: %w", entry.Sequence, err)
		}
		if _, ok := completed[intent.ID]; ok {
			continue
		}
		handler, command, err := e.engine.flow.effectForDurableIntent(intent)
		if err != nil {
			return err
		}
		effectCtx := context.WithValue(ctx, flowEffectIDContextKey{}, intent.ID)
		if err := handler(effectCtx, command); err != nil {
			return &FlowEffectDeliveryError{EffectID: intent.ID, Name: intent.Name, Err: err}
		}
		completion := FlowHistoryEntry{
			Sequence:  historyLength + 1,
			Type:      flowHistoryEffectCompleted,
			Name:      intent.Name,
			Data:      FlowEffectCompletion{ID: intent.ID},
			CreatedAt: time.Now().UTC(),
		}
		if err := store.SaveStateAndAppend(ctx, e.engine.flow.name, e.id, state, []FlowHistoryEntry{completion}); err != nil {
			return &FlowEffectAcknowledgeError{EffectID: intent.ID, Name: intent.Name, Err: err}
		}
		historyLength++
		completed[intent.ID] = struct{}{}
	}
	return nil
}

func (f *Flow[S]) effectForDurableIntent(intent FlowEffectIntent) (flowEffectHandler, any, error) {
	for typ, handler := range f.effects {
		if durableEffectTypeName(typ) != intent.Name {
			continue
		}
		var command any
		if typ.Kind() == reflect.Pointer {
			value := reflect.New(typ.Elem())
			if err := json.Unmarshal(intent.Payload, value.Interface()); err != nil {
				return nil, nil, fmt.Errorf("axiom: decode durable flow effect %s: %w", intent.Name, err)
			}
			command = value.Interface()
		} else {
			value := reflect.New(typ)
			if err := json.Unmarshal(intent.Payload, value.Interface()); err != nil {
				return nil, nil, fmt.Errorf("axiom: decode durable flow effect %s: %w", intent.Name, err)
			}
			command = value.Elem().Interface()
		}
		return handler, command, nil
	}
	return nil, nil, fmt.Errorf("axiom: no effect handler registered for durable command %s", intent.Name)
}

func decodeFlowHistoryData[T any](data any) (T, error) {
	var out T
	encoded, err := json.Marshal(data)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return out, err
	}
	return out, nil
}
