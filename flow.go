package axiom

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/syncx"
)

// Effect is an opaque command emitted by a Go-first reducer.
type Effect struct{ Command any }

func Call(command any) Effect { return Effect{Command: command} }

type FlowResult[S any] struct {
	State   S
	Effects []Effect
}

func Next[S any](state S, effects ...Effect) FlowResult[S] {
	return FlowResult[S]{State: state, Effects: effects}
}

type flowHandler[S any] func(context.Context, S, any) (FlowResult[S], error)
type flowEffectHandler func(context.Context, any) error

// Flow is the file-free typed reducer frontend. Its analysis level is
// opaque because arbitrary Go handlers cannot be statically inspected.
type Flow[S any] struct {
	name     string
	initial  S
	handlers map[reflect.Type]flowHandler[S]
	effects  map[reflect.Type]flowEffectHandler
	claims   []func(S) error
}

func NewFlow[S any](name string, initial S) *Flow[S] {
	return &Flow[S]{
		name:     name,
		initial:  initial,
		handlers: map[reflect.Type]flowHandler[S]{},
		effects:  map[reflect.Type]flowEffectHandler{},
	}
}

func (f *Flow[S]) Name() string            { return f.name }
func (f *Flow[S]) Analysis() AnalysisLevel { return AnalysisOpaque }

func Handle[S, E any](flow *Flow[S], handler func(context.Context, S, E) (FlowResult[S], error)) {
	if flow == nil || handler == nil {
		panic("axiom: flow and handler are required")
	}
	typ := reflect.TypeFor[E]()
	flow.handlers[typ] = func(ctx context.Context, state S, event any) (FlowResult[S], error) {
		typed, ok := event.(E)
		if !ok {
			return FlowResult[S]{}, fmt.Errorf("axiom: event type mismatch: got %T", event)
		}
		return handler(ctx, state, typed)
	}
}

func EffectHandler[S, C any](flow *Flow[S], handler func(context.Context, C) error) {
	if flow == nil || handler == nil {
		panic("axiom: flow and effect handler are required")
	}
	typ := reflect.TypeFor[C]()
	flow.effects[typ] = func(ctx context.Context, command any) error {
		typed, ok := command.(C)
		if !ok {
			return fmt.Errorf("axiom: effect type mismatch: got %T", command)
		}
		return handler(ctx, typed)
	}
}

func AddClaim[S any](flow *Flow[S], claim func(S) error) {
	if flow == nil || claim == nil {
		panic("axiom: flow and claim are required")
	}
	flow.claims = append(flow.claims, claim)
}

type FlowHistoryEntry struct {
	Sequence  int
	Type      string
	Name      string
	Data      any
	CreatedAt time.Time
}

// FlowStore is the compatibility storage contract for typed Go flows.
// Stores that also implement IncrementalFlowStore avoid loading and rewriting
// the complete history on every dispatch.
type FlowStore interface {
	Load(ctx context.Context, flow, id string) (state []byte, history []FlowHistoryEntry, found bool, err error)
	Save(ctx context.Context, flow, id string, state []byte, history []FlowHistoryEntry) error
}

// IncrementalFlowStore is an optional capability for append-only history.
// It preserves FlowStore compatibility while reducing a long-lived execution
// from quadratic history copying to constant work per newly appended entry.
type IncrementalFlowStore interface {
	LoadState(ctx context.Context, flow, id string) (state []byte, historyLength int, found bool, err error)
	SaveStateAndAppend(ctx context.Context, flow, id string, state []byte, entries []FlowHistoryEntry) error
	LoadHistory(ctx context.Context, flow, id string) ([]FlowHistoryEntry, error)
}

type memoryFlowRecord struct {
	state   []byte
	history []FlowHistoryEntry
}

type MemoryFlowStore struct {
	mu      sync.Mutex
	records map[string]memoryFlowRecord
}

func NewMemoryFlowStore() *MemoryFlowStore {
	return &MemoryFlowStore{records: map[string]memoryFlowRecord{}}
}

func (s *MemoryFlowStore) key(flow, id string) string { return flow + "\x00" + id }

func (s *MemoryFlowStore) Load(ctx context.Context, flow, id string) ([]byte, []FlowHistoryEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.records[s.key(flow, id)]
	if !ok {
		return nil, nil, false, nil
	}
	return append([]byte(nil), value.state...), append([]FlowHistoryEntry(nil), value.history...), true, nil
}

func (s *MemoryFlowStore) Save(ctx context.Context, flow, id string, state []byte, history []FlowHistoryEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[s.key(flow, id)] = memoryFlowRecord{
		state:   append([]byte(nil), state...),
		history: append([]FlowHistoryEntry(nil), history...),
	}
	return nil
}

func (s *MemoryFlowStore) LoadState(ctx context.Context, flow, id string) ([]byte, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.records[s.key(flow, id)]
	if !ok {
		return nil, 0, false, nil
	}
	return append([]byte(nil), value.state...), len(value.history), true, nil
}

func (s *MemoryFlowStore) SaveStateAndAppend(ctx context.Context, flow, id string, state []byte, entries []FlowHistoryEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(flow, id)
	value := s.records[key]
	value.state = append(value.state[:0], state...)
	value.history = append(value.history, entries...)
	s.records[key] = value
	return nil
}

func (s *MemoryFlowStore) LoadHistory(ctx context.Context, flow, id string) ([]FlowHistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.records[s.key(flow, id)]
	return append([]FlowHistoryEntry(nil), value.history...), nil
}

type flowConfig struct {
	store          FlowStore
	durableEffects bool
}

type FlowOption func(*flowConfig) error

func WithFlowStore(store FlowStore) FlowOption {
	return func(config *flowConfig) error {
		if store == nil {
			return fmt.Errorf("axiom: flow store must not be nil")
		}
		config.store = store
		return nil
	}
}

// WithDurableFlowEffects enables a transactional outbox for reducer effects.
// The configured store must implement DurableFlowStore and report synchronous
// durability. External effect delivery then happens only after state and
// EffectPending intents are committed. Delivery is at-least-once; use
// FlowEffectIDFromContext for downstream deduplication.
func WithDurableFlowEffects() FlowOption {
	return func(config *flowConfig) error {
		config.durableEffects = true
		return nil
	}
}

type FlowEngine[S any] struct {
	flow           *Flow[S]
	store          FlowStore
	locks          *syncx.KeyedLocker
	durableEffects bool
}

func OpenFlow[S any](flow *Flow[S], opts ...FlowOption) (*FlowEngine[S], error) {
	if flow == nil || flow.name == "" {
		return nil, fmt.Errorf("axiom: named flow is required")
	}
	config := flowConfig{store: NewMemoryFlowStore()}
	for _, opt := range opts {
		if err := opt(&config); err != nil {
			return nil, err
		}
	}
	if config.durableEffects {
		store, ok := config.store.(DurableFlowStore)
		if !ok {
			return nil, fmt.Errorf("axiom: durable flow effects require a DurableFlowStore with atomic state/outbox commits")
		}
		if level := store.Durability(); level != StoreDurabilitySynchronous {
			return nil, fmt.Errorf("axiom: durable flow effects require synchronous store durability; store reports %q", level)
		}
		if err := validateDurableFlowEffectTypes(flow); err != nil {
			return nil, err
		}
	}
	return &FlowEngine[S]{
		flow:           freezeFlow(flow),
		store:          config.store,
		locks:          syncx.NewKeyedLocker(),
		durableEffects: config.durableEffects,
	}, nil
}

func freezeFlow[S any](flow *Flow[S]) *Flow[S] {
	frozen := &Flow[S]{
		name:     flow.name,
		initial:  flow.initial,
		handlers: make(map[reflect.Type]flowHandler[S], len(flow.handlers)),
		effects:  make(map[reflect.Type]flowEffectHandler, len(flow.effects)),
		claims:   append([]func(S) error(nil), flow.claims...),
	}
	for typ, handler := range flow.handlers {
		frozen.handlers[typ] = handler
	}
	for typ, handler := range flow.effects {
		frozen.effects[typ] = handler
	}
	return frozen
}

type FlowExecution[S any] struct {
	engine *FlowEngine[S]
	id     string
}

func (e *FlowEngine[S]) Execution(id string) *FlowExecution[S] {
	return &FlowExecution[S]{engine: e, id: id}
}

func (e *FlowExecution[S]) Dispatch(ctx context.Context, event any) error {
	if e == nil || e.engine == nil || e.id == "" {
		return fmt.Errorf("axiom: valid flow execution is required")
	}
	unlock := e.engine.locks.Lock(e.id)
	defer unlock()

	if e.engine.durableEffects {
		return e.dispatchDurableLocked(ctx, event)
	}

	handler, normalized, err := e.engine.flow.handlerFor(event)
	if err != nil {
		return err
	}
	state, history, historyLength, err := e.loadForDispatch(ctx)
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

	entries := make([]FlowHistoryEntry, 0, 1+len(result.Effects))
	entries = append(entries, FlowHistoryEntry{
		Sequence:  historyLength + 1,
		Type:      "EventHandled",
		Name:      typeName(normalized),
		Data:      normalized,
		CreatedAt: time.Now().UTC(),
	})
	for _, effect := range result.Effects {
		effectHandler, command, err := e.engine.flow.effectFor(effect.Command)
		if err != nil {
			return err
		}
		if err := effectHandler(ctx, command); err != nil {
			return err
		}
		entries = append(entries, FlowHistoryEntry{
			Sequence:  historyLength + len(entries) + 1,
			Type:      "EffectCompleted",
			Name:      typeName(command),
			Data:      command,
			CreatedAt: time.Now().UTC(),
		})
	}

	data, err := json.Marshal(result.State)
	if err != nil {
		return err
	}
	if store, ok := e.engine.store.(IncrementalFlowStore); ok {
		return store.SaveStateAndAppend(ctx, e.engine.flow.name, e.id, data, entries)
	}
	history = append(history, entries...)
	return e.engine.store.Save(ctx, e.engine.flow.name, e.id, data, history)
}

func (e *FlowExecution[S]) State(ctx context.Context) (S, error) {
	unlock := e.engine.locks.Lock(e.id)
	defer unlock()
	state, err := e.loadState(ctx)
	return state, err
}

func (e *FlowExecution[S]) History(ctx context.Context) ([]FlowHistoryEntry, error) {
	unlock := e.engine.locks.Lock(e.id)
	defer unlock()
	if store, ok := e.engine.store.(IncrementalFlowStore); ok {
		return store.LoadHistory(ctx, e.engine.flow.name, e.id)
	}
	_, history, _, err := e.engine.store.Load(ctx, e.engine.flow.name, e.id)
	return history, err
}

func (e *FlowExecution[S]) loadForDispatch(ctx context.Context) (S, []FlowHistoryEntry, int, error) {
	var state S
	if store, ok := e.engine.store.(IncrementalFlowStore); ok {
		data, historyLength, found, err := store.LoadState(ctx, e.engine.flow.name, e.id)
		if err != nil {
			return state, nil, 0, err
		}
		if !found {
			data, err = json.Marshal(e.engine.flow.initial)
			if err != nil {
				return state, nil, 0, err
			}
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return state, nil, 0, err
		}
		return state, nil, historyLength, nil
	}

	data, history, found, err := e.engine.store.Load(ctx, e.engine.flow.name, e.id)
	if err != nil {
		return state, nil, 0, err
	}
	if !found {
		data, err = json.Marshal(e.engine.flow.initial)
		if err != nil {
			return state, nil, 0, err
		}
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, nil, 0, err
	}
	return state, history, len(history), nil
}

func (e *FlowExecution[S]) loadState(ctx context.Context) (S, error) {
	var state S
	if e == nil || e.engine == nil || e.id == "" {
		return state, fmt.Errorf("axiom: valid flow execution is required")
	}
	if store, ok := e.engine.store.(IncrementalFlowStore); ok {
		data, _, found, err := store.LoadState(ctx, e.engine.flow.name, e.id)
		if err != nil {
			return state, err
		}
		if !found {
			return e.engine.flow.initial, nil
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return state, err
		}
		return state, nil
	}

	data, _, found, err := e.engine.store.Load(ctx, e.engine.flow.name, e.id)
	if err != nil {
		return state, err
	}
	if !found {
		return e.engine.flow.initial, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func (f *Flow[S]) handlerFor(event any) (flowHandler[S], any, error) {
	if event == nil {
		return nil, nil, fmt.Errorf("axiom: event is required")
	}
	typ := reflect.TypeOf(event)
	if handler, ok := f.handlers[typ]; ok {
		return handler, event, nil
	}
	value := reflect.ValueOf(event)
	if typ.Kind() == reflect.Pointer && !value.IsNil() {
		if handler, ok := f.handlers[typ.Elem()]; ok {
			return handler, value.Elem().Interface(), nil
		}
	}
	return nil, nil, fmt.Errorf("axiom: no handler registered for %T", event)
}

func (f *Flow[S]) effectFor(command any) (flowEffectHandler, any, error) {
	if command == nil {
		return nil, nil, fmt.Errorf("axiom: effect command is required")
	}
	typ := reflect.TypeOf(command)
	if handler, ok := f.effects[typ]; ok {
		return handler, command, nil
	}
	value := reflect.ValueOf(command)
	if typ.Kind() == reflect.Pointer && !value.IsNil() {
		if handler, ok := f.effects[typ.Elem()]; ok {
			return handler, value.Elem().Interface(), nil
		}
	}
	return nil, nil, fmt.Errorf("axiom: no effect handler registered for %T", command)
}

func typeName(value any) string {
	typ := reflect.TypeOf(value)
	if typ == nil {
		return ""
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.PkgPath() == "" {
		return typ.Name()
	}
	return typ.PkgPath() + "." + typ.Name()
}
