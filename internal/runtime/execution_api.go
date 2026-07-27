package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// EventNamer overrides the signal name derived from a Go event type.
type EventNamer interface {
	AxiomEventName() string
}

// Explanation is the typed form of the built-in explain query.
type Explanation struct {
	Status            Status
	Facts             map[string]FactValue
	PendingActivities []*ActivityTask
	History           []HistoryEntry
}

// Run is an ergonomic handle for one execution.
type Run struct {
	engine *Engine
	id     string
}

// Execution returns a handle that can dispatch typed events and read state.
func (e *Engine) Execution(id string) *Run {
	return &Run{engine: e, id: id}
}

// ID returns the durable execution identifier.
func (r *Run) ID() string { return r.id }

// Dispatch creates the execution when needed, sends an event and drains
// inline activities until the execution is idle.
func (r *Run) Dispatch(ctx context.Context, event any) error {
	if r == nil || r.engine == nil {
		return fmt.Errorf("axiom: execution handle is not initialized")
	}
	if r.id == "" {
		return fmt.Errorf("axiom: execution id is required")
	}
	name, payload, err := eventPayload(event)
	if err != nil {
		return err
	}
	if _, err := r.engine.store.GetExecution(ctx, r.id); err != nil {
		if !errors.Is(err, ErrExecutionNotFound) {
			return err
		}
		if err := r.engine.Start(ctx, r.id, nil); err != nil {
			return err
		}
	}
	if err := r.engine.Signal(ctx, r.id, name, payload); err != nil {
		return err
	}
	return r.engine.RunUntilIdle(ctx, r.id)
}

// Signal dispatches an explicitly named signal and drains inline work.
func (r *Run) Signal(ctx context.Context, name string, payload map[string]any) error {
	if r == nil || r.engine == nil {
		return fmt.Errorf("axiom: execution handle is not initialized")
	}
	if _, err := r.engine.store.GetExecution(ctx, r.id); err != nil {
		if !errors.Is(err, ErrExecutionNotFound) {
			return err
		}
		if err := r.engine.Start(ctx, r.id, nil); err != nil {
			return err
		}
	}
	if err := r.engine.Signal(ctx, r.id, name, payload); err != nil {
		return err
	}
	return r.engine.RunUntilIdle(ctx, r.id)
}

// State decodes the execution context into target. If the plan contains
// one context and target is a struct, that context is decoded directly.
func (r *Run) State(ctx context.Context, target any) error {
	if target == nil {
		return fmt.Errorf("axiom: state target is required")
	}
	result, err := r.engine.Query(ctx, r.id, "state")
	if err != nil {
		return err
	}
	contexts, ok := result["context"].(map[string]map[string]any)
	if !ok {
		return fmt.Errorf("axiom: unexpected state representation")
	}
	value := any(contexts)
	rv := reflect.ValueOf(target)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() && rv.Elem().Kind() == reflect.Struct && len(contexts) == 1 {
		for _, contextValue := range contexts {
			value = contextValue
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("axiom: decode state: %w", err)
	}
	return nil
}

func (r *Run) Status(ctx context.Context) (Status, error) {
	execution, err := r.engine.store.GetExecution(ctx, r.id)
	if err != nil {
		return "", err
	}
	return execution.Status, nil
}

func (r *Run) History(ctx context.Context) ([]HistoryEntry, error) {
	return r.engine.store.ListHistory(ctx, r.id)
}

func (r *Run) PendingActivities(ctx context.Context) ([]ActivityTask, error) {
	result, err := r.engine.Query(ctx, r.id, "pendingActivities")
	if err != nil {
		return nil, err
	}
	values, ok := result["pendingActivities"].([]ActivityTask)
	if !ok {
		return nil, fmt.Errorf("axiom: unexpected pending activities representation")
	}
	return values, nil
}

func (r *Run) Explain(ctx context.Context) (*Explanation, error) {
	execution, err := r.engine.store.GetExecution(ctx, r.id)
	if err != nil {
		return nil, err
	}
	history, err := r.engine.store.ListHistory(ctx, r.id)
	if err != nil {
		return nil, err
	}
	tasks, err := r.engine.store.ListTasks(ctx, r.id)
	if err != nil {
		return nil, err
	}
	return &Explanation{
		Status:            execution.Status,
		Facts:             cloneFacts(execution.Facts),
		PendingActivities: tasks,
		History:           history,
	}, nil
}

func (r *Run) Cancel(ctx context.Context) error {
	return r.engine.withStoreTransaction(ctx, func() error {
		execution, err := r.engine.store.GetExecution(ctx, r.id)
		if err != nil {
			return err
		}
		execution.Status = StatusCanceled
		execution.UpdatedAt = time.Now().UTC()
		if err := r.engine.store.AppendHistory(ctx, r.id, "ExecutionCanceled", nil); err != nil {
			return err
		}
		return r.engine.store.SaveExecution(ctx, execution)
	})
}

func eventPayload(event any) (string, map[string]any, error) {
	if event == nil {
		return "", nil, fmt.Errorf("axiom: event is required")
	}
	name := ""
	if named, ok := event.(EventNamer); ok {
		name = named.AxiomEventName()
	}
	value := reflect.ValueOf(event)
	typ := value.Type()
	if typ.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", nil, fmt.Errorf("axiom: event pointer is nil")
		}
		if name == "" {
			typ = typ.Elem()
		}
	}
	if name == "" {
		name = typ.Name()
	}
	if name == "" {
		return "", nil, fmt.Errorf("axiom: event type must be named or implement EventNamer")
	}
	if payload, ok := event.(map[string]any); ok {
		return name, payload, nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return "", nil, fmt.Errorf("axiom: encode event: %w", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", nil, fmt.Errorf("axiom: event must encode as an object: %w", err)
	}
	return name, payload, nil
}
