package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
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

func (r *Run) lock() (func(), error) {
	if r == nil || r.engine == nil {
		return nil, fmt.Errorf("axiom: execution handle is not initialized")
	}
	if r.id == "" {
		return nil, fmt.Errorf("axiom: execution id is required")
	}
	if r.engine.executionLocks == nil {
		return nil, fmt.Errorf("axiom: execution lock registry is not initialized")
	}
	return r.engine.executionLocks.Lock(r.id), nil
}

// Dispatch creates the execution when needed, sends an event and drains
// inline activities, including durable retries, until the execution is idle.
func (r *Run) Dispatch(ctx context.Context, event any) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
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
	return drainUntilIdle(ctx, r.engine, r.id)
}

// Signal dispatches an explicitly named signal and drains inline work,
// including any durable retries due before the caller's context ends.
func (r *Run) Signal(ctx context.Context, name string, payload map[string]any) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
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
	return drainUntilIdle(ctx, r.engine, r.id)
}

// Patch applies field changes to the execution context and drains inline work,
// including durable retries.
func (r *Run) Patch(ctx context.Context, patch map[string]any) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := r.engine.store.GetExecution(ctx, r.id); err != nil {
		if !errors.Is(err, ErrExecutionNotFound) {
			return err
		}
		if err := r.engine.Start(ctx, r.id, nil); err != nil {
			return err
		}
	}
	if err := r.engine.Patch(ctx, r.id, patch); err != nil {
		return err
	}
	return drainUntilIdle(ctx, r.engine, r.id)
}

// State decodes the execution context into target. If the plan contains
// one context and target is a struct, that context is decoded directly.
func (r *Run) State(ctx context.Context, target any) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
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
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	execution, err := r.engine.store.GetExecution(ctx, r.id)
	if err != nil {
		return "", err
	}
	return execution.Status, nil
}

func (r *Run) History(ctx context.Context) ([]HistoryEntry, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return r.engine.store.ListHistory(ctx, r.id)
}

func (r *Run) PendingActivities(ctx context.Context) ([]ActivityTask, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
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
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
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
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return r.engine.withStoreTransaction(ctx, func(working *Engine) error {
		execution, err := working.store.GetExecution(ctx, r.id)
		if err != nil {
			return err
		}
		execution.Status = StatusCanceled
		execution.UpdatedAt = time.Now().UTC()
		if err := working.store.AppendHistory(ctx, r.id, "ExecutionCanceled", nil); err != nil {
			return err
		}
		return working.store.SaveExecution(ctx, execution)
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	payload := map[string]any{}
	if err := decoder.Decode(&payload); err != nil {
		return "", nil, fmt.Errorf("axiom: event must encode as an object: %w", err)
	}
	normalized, err := normalizeJSONNumbers(payload)
	if err != nil {
		return "", nil, fmt.Errorf("axiom: normalize event numbers: %w", err)
	}
	return name, normalized.(map[string]any), nil
}

func normalizeJSONNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		text := typed.String()
		if strings.ContainsAny(text, ".eE") {
			parsed, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, err
			}
			return parsed, nil
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, err
		}
		if strconv.IntSize == 64 || (parsed >= -1<<31 && parsed <= 1<<31-1) {
			return int(parsed), nil
		}
		return parsed, nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized, err := normalizeJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}
