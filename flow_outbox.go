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

// FlowOutboxStatus is a bounded-cardinality diagnostic snapshot of one durable
// Flow execution. It intentionally exposes aggregate counts and the oldest
// pending history position rather than per-effect labels.
type FlowOutboxStatus struct {
	HistoryLength         int
	Pending               int
	Completed             int
	OldestPendingSequence int
	OldestPendingAt       time.Time
}

// HasPending reports whether the durable outbox contains recoverable work.
func (s FlowOutboxStatus) HasPending() bool { return s.Pending > 0 }

// FlowOutboxDrainResult describes the durable work performed by one bounded
// drain call. Attempted counts external handler invocations; Acknowledged counts
// completion markers durably appended. Remaining is the number of pending
// intents still unacknowledged when the call returned.
type FlowOutboxDrainResult struct {
	Attempted     int
	Acknowledged int
	Remaining    int
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

func (e *FlowEffectDeliveryError) Unwrap() error        { return e.Err }
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

func (e *FlowEffectAcknowledgeError) Unwrap() error        { return e.Err }
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
	if err := e.hitDurableFlowFailpoint(ctx, flowFailpointBeforeStateIntentCommit, eventSequence, nil); err != nil {
		return err
	}
	if err := store.SaveStateAndAppend(ctx, e.engine.flow.name, e.id, data, entries); err != nil {
		return err
	}
	if err := e.hitDurableFlowFailpoint(ctx, flowFailpointAfterStateIntentCommit, eventSequence, nil); err != nil {
		return err
	}
	return e.drainDurableEffectsLocked(ctx, store)
}

// OutboxStatus returns a read-only aggregate snapshot of the durable outbox for
// this execution. It does not invoke effect handlers or modify durable state.
func (e *FlowExecution[S]) OutboxStatus(ctx context.Context) (FlowOutboxStatus, error) {
	if err := e.validateDurableOutboxExecution(); err != nil {
		return FlowOutboxStatus{}, err
	}
	unlock := e.engine.locks.Lock(e.id)
	defer unlock()
	view, err := e.loadDurableOutboxLocked(ctx, e.engine.store.(DurableFlowStore))
	if err != nil {
		return FlowOutboxStatus{}, err
	}
	return view.status(), nil
}

// DrainEffects retries every currently pending durable outbox item. Delivery is
// at-least-once; use FlowEffectIDFromContext as the downstream idempotency key
// when exactly-once business effects matter.
func (e *FlowExecution[S]) DrainEffects(ctx context.Context) error {
	if err := e.validateDurableOutboxExecution(); err != nil {
		return err
	}
	unlock := e.engine.locks.Lock(e.id)
	defer unlock()
	return e.drainDurableEffectsLocked(ctx, e.engine.store.(DurableFlowStore))
}

// DrainEffectsLimit retries at most maxDeliveries pending durable effects in
// strict outbox order. maxDeliveries must be positive. A successful partial
// drain is not an error; inspect Remaining to schedule another bounded call.
// The result is also returned alongside delivery/acknowledgement errors so the
// caller can account for work already attempted or durably acknowledged.
func (e *FlowExecution[S]) DrainEffectsLimit(ctx context.Context, maxDeliveries int) (FlowOutboxDrainResult, error) {
	if err := e.validateDurableOutboxExecution(); err != nil {
		return FlowOutboxDrainResult{}, err
	}
	if maxDeliveries <= 0 {
		return FlowOutboxDrainResult{}, fmt.Errorf("axiom: durable flow drain limit must be positive")
	}
	unlock := e.engine.locks.Lock(e.id)
	defer unlock()
	return e.drainDurableEffectsLockedLimit(ctx, e.engine.store.(DurableFlowStore), maxDeliveries)
}

func (e *FlowExecution[S]) validateDurableOutboxExecution() error {
	if e == nil || e.engine == nil || e.id == "" {
		return fmt.Errorf("axiom: valid flow execution is required")
	}
	if !e.engine.durableEffects {
		return fmt.Errorf("axiom: flow was not opened with WithDurableFlowEffects")
	}
	return nil
}

type durableFlowPending struct {
	entry  FlowHistoryEntry
	intent FlowEffectIntent
}

type durableFlowOutboxView struct {
	state         []byte
	historyLength int
	completed     map[string]struct{}
	pending       []durableFlowPending
}

func (v durableFlowOutboxView) status() FlowOutboxStatus {
	status := FlowOutboxStatus{
		HistoryLength: v.historyLength,
		Pending:       len(v.pending),
		Completed:     len(v.completed),
	}
	if len(v.pending) > 0 {
		status.OldestPendingSequence = v.pending[0].entry.Sequence
		status.OldestPendingAt = v.pending[0].entry.CreatedAt
	}
	return status
}

func (e *FlowExecution[S]) loadDurableOutboxLocked(ctx context.Context, store DurableFlowStore) (durableFlowOutboxView, error) {
	state, historyLength, found, err := store.LoadState(ctx, e.engine.flow.name, e.id)
	if err != nil {
		return durableFlowOutboxView{}, err
	}
	if !found {
		return durableFlowOutboxView{completed: map[string]struct{}{}}, nil
	}
	history, err := store.LoadHistory(ctx, e.engine.flow.name, e.id)
	if err != nil {
		return durableFlowOutboxView{}, err
	}
	if historyLength != len(history) {
		return durableFlowOutboxView{}, fmt.Errorf("axiom: durable flow store history length mismatch: state reports %d, history has %d", historyLength, len(history))
	}

	completed := make(map[string]struct{})
	for _, entry := range history {
		if entry.Type != flowHistoryEffectCompleted {
			continue
		}
		completion, err := decodeFlowHistoryData[FlowEffectCompletion](entry.Data)
		if err != nil {
			return durableFlowOutboxView{}, fmt.Errorf("axiom: decode durable flow effect completion at sequence %d: %w", entry.Sequence, err)
		}
		completed[completion.ID] = struct{}{}
	}

	pending := make([]durableFlowPending, 0)
	for _, entry := range history {
		if entry.Type != flowHistoryEffectPending {
			continue
		}
		intent, err := decodeFlowHistoryData[FlowEffectIntent](entry.Data)
		if err != nil {
			return durableFlowOutboxView{}, fmt.Errorf("axiom: decode durable flow effect intent at sequence %d: %w", entry.Sequence, err)
		}
		if _, ok := completed[intent.ID]; ok {
			continue
		}
		pending = append(pending, durableFlowPending{entry: entry, intent: intent})
	}
	return durableFlowOutboxView{
		state:         state,
		historyLength: historyLength,
		completed:     completed,
		pending:       pending,
	}, nil
}

func (e *FlowExecution[S]) drainDurableEffectsLocked(ctx context.Context, store DurableFlowStore) error {
	_, err := e.drainDurableEffectsLockedLimit(ctx, store, 0)
	return err
}

// maxDeliveries == 0 is the internal compatibility sentinel for drain-all.
// Public DrainEffectsLimit rejects non-positive limits.
func (e *FlowExecution[S]) drainDurableEffectsLockedLimit(
	ctx context.Context,
	store DurableFlowStore,
	maxDeliveries int,
) (FlowOutboxDrainResult, error) {
	view, err := e.loadDurableOutboxLocked(ctx, store)
	if err != nil {
		return FlowOutboxDrainResult{}, err
	}
	result := FlowOutboxDrainResult{Remaining: len(view.pending)}
	if len(view.pending) == 0 {
		return result, nil
	}

	historyLength := view.historyLength
	for _, pending := range view.pending {
		if maxDeliveries > 0 && result.Attempted >= maxDeliveries {
			return result, nil
		}
		entry := pending.entry
		intent := pending.intent
		handler, command, err := e.engine.flow.effectForDurableIntent(intent)
		if err != nil {
			return result, err
		}
		if err := e.hitDurableFlowFailpoint(ctx, flowFailpointBeforeEffectDelivery, entry.Sequence, &intent); err != nil {
			return result, err
		}
		effectCtx := context.WithValue(ctx, flowEffectIDContextKey{}, intent.ID)
		result.Attempted++
		if err := handler(effectCtx, command); err != nil {
			return result, &FlowEffectDeliveryError{EffectID: intent.ID, Name: intent.Name, Err: err}
		}
		if err := e.hitDurableFlowFailpoint(ctx, flowFailpointAfterEffectDelivery, entry.Sequence, &intent); err != nil {
			return result, err
		}
		completion := FlowHistoryEntry{
			Sequence:  historyLength + 1,
			Type:      flowHistoryEffectCompleted,
			Name:      intent.Name,
			Data:      FlowEffectCompletion{ID: intent.ID},
			CreatedAt: time.Now().UTC(),
		}
		if err := e.hitDurableFlowFailpoint(ctx, flowFailpointBeforeAcknowledgeCommit, completion.Sequence, &intent); err != nil {
			return result, err
		}
		if err := store.SaveStateAndAppend(ctx, e.engine.flow.name, e.id, view.state, []FlowHistoryEntry{completion}); err != nil {
			return result, &FlowEffectAcknowledgeError{EffectID: intent.ID, Name: intent.Name, Err: err}
		}
		historyLength++
		view.completed[intent.ID] = struct{}{}
		result.Acknowledged++
		result.Remaining--
		if err := e.hitDurableFlowFailpoint(ctx, flowFailpointAfterAcknowledgeCommit, completion.Sequence, &intent); err != nil {
			return result, err
		}
	}
	return result, nil
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
