package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"axiom/pkg/axiom"
)

type RuntimeSession struct {
	module      *axiom.Module
	store       axiom.Store
	engine      *axiom.Engine
	executionID string
}

func NewRuntimeSession(module *axiom.Module) *RuntimeSession {
	store := axiom.NewMemoryStore()
	return &RuntimeSession{
		module: module,
		store:  store,
		engine: axiom.NewEngine(module, store, nil),
	}
}

func (s *RuntimeSession) Started() bool {
	return s != nil && s.executionID != ""
}

func (s *RuntimeSession) ExecutionID() string {
	if s == nil {
		return ""
	}
	return s.executionID
}

func (s *RuntimeSession) StartExecution(ctx context.Context, executionID string, initialJSON string) (string, error) {
	if s == nil || s.engine == nil {
		return "", fmt.Errorf("runtime session is not ready")
	}
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return "", fmt.Errorf("execution id is required")
	}
	initial, err := parseOptionalJSONObject(initialJSON)
	if err != nil {
		return "", err
	}
	if err := s.engine.Start(ctx, executionID, initial); err != nil {
		return "", err
	}
	s.executionID = executionID
	state, err := s.engine.Query(ctx, executionID, "state")
	if err != nil {
		return "", err
	}
	return prettyJSON(state), nil
}

func (s *RuntimeSession) SendSignal(ctx context.Context, signalName string, payloadJSON string) (string, error) {
	if err := s.requireExecution(); err != nil {
		return "", err
	}
	signalName = strings.TrimSpace(signalName)
	if signalName == "" {
		return "", fmt.Errorf("signal name is required")
	}
	payload, err := parseOptionalJSONObject(payloadJSON)
	if err != nil {
		return "", err
	}
	if err := s.engine.Signal(ctx, s.executionID, signalName, payload); err != nil {
		return "", err
	}
	return s.Query(ctx, "history")
}

func (s *RuntimeSession) PatchContext(ctx context.Context, patchJSON string) (string, error) {
	if err := s.requireExecution(); err != nil {
		return "", err
	}
	patch, err := parseRequiredJSONObject(patchJSON)
	if err != nil {
		return "", err
	}
	if err := s.engine.Patch(ctx, s.executionID, patch); err != nil {
		return "", err
	}
	return s.Query(ctx, "state")
}

func (s *RuntimeSession) RunUntilIdle(ctx context.Context) (string, error) {
	if err := s.requireExecution(); err != nil {
		return "", err
	}
	if err := s.engine.RunUntilIdle(ctx, s.executionID); err != nil {
		return "", err
	}
	return s.Query(ctx, "pendingActivities")
}

func (s *RuntimeSession) Query(ctx context.Context, queryName string) (string, error) {
	if err := s.requireExecution(); err != nil {
		return "", err
	}
	queryName = strings.TrimSpace(queryName)
	if queryName == "" {
		return "", fmt.Errorf("query name is required")
	}
	result, err := s.engine.Query(ctx, s.executionID, queryName)
	if err != nil {
		return "", err
	}
	return prettyJSON(result), nil
}

func (s *RuntimeSession) SignalNames() []string {
	if s == nil || s.module == nil {
		return nil
	}
	names := make([]string, 0, len(s.module.Signals))
	for name := range s.module.Signals {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *RuntimeSession) QueryNames() []string {
	names := []string{"facts", "history", "pendingActivities", "state"}
	if s == nil || s.module == nil {
		return names
	}
	for name := range s.module.Queries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *RuntimeSession) requireExecution() error {
	if s == nil || s.engine == nil {
		return fmt.Errorf("runtime session is not ready")
	}
	if s.executionID == "" {
		return fmt.Errorf("execution is not started")
	}
	return nil
}

func parseOptionalJSONObject(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	return parseJSONObject(raw)
}

func parseRequiredJSONObject(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("JSON object is required")
	}
	return parseJSONObject(raw)
}

func parseJSONObject(raw string) (map[string]any, error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("invalid JSON: multiple values")
	}
	if value == nil {
		return nil, nil
	}
	normalized, err := normalizeJSONValue(value)
	if err != nil {
		return nil, err
	}
	value = normalized
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON value must be an object")
	}
	return object, nil
}

func normalizeJSONValue(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			normalized, err := normalizeJSONValue(item)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			normalized, err := normalizeJSONValue(item)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	case json.Number:
		text := v.String()
		if strings.ContainsAny(text, ".eE") {
			number, err := v.Float64()
			if err != nil {
				return nil, fmt.Errorf("invalid JSON number %q: %w", text, err)
			}
			return number, nil
		}
		number, err := v.Int64()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON integer %q: %w", text, err)
		}
		if number >= math.MinInt && number <= math.MaxInt {
			return int(number), nil
		}
		return number, nil
	default:
		return value, nil
	}
}

func prettyJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}
