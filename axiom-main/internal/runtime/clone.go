package runtime

// CloneContext returns a deep copy of a context map (ContextName -> FieldName -> Value).
func CloneContext(in map[string]map[string]any) map[string]map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(in))
	for key, value := range in {
		out[key] = CloneAnyMap(value)
	}
	return out
}

// CloneFacts returns a deep copy of a facts map.
func CloneFacts(in map[string]FactValue) map[string]FactValue {
	if in == nil {
		return nil
	}
	out := make(map[string]FactValue, len(in))
	for key, value := range in {
		out[key] = FactValue{True: value.True, Exposed: CloneAnyMap(value.Exposed)}
	}
	return out
}

// CloneExecution returns a deep copy of an Execution.
func CloneExecution(in *Execution) *Execution {
	if in == nil {
		return nil
	}
	return &Execution{
		ID:              in.ID,
		Domain:          in.Domain,
		Status:          in.Status,
		Context:         cloneContext(in.Context),
		Computed:        cloneAnyMap(in.Computed),
		Facts:           cloneFacts(in.Facts),
		RuntimeState:    cloneExecutionState(in.RuntimeState),
		ModuleHash:      in.ModuleHash,
		CompilerVersion: in.CompilerVersion,
		PlanVersion:     in.PlanVersion,
		Version:         in.Version,
		CreatedAt:       in.CreatedAt,
		UpdatedAt:       in.UpdatedAt,
	}
}

// CloneAnyMap returns a deep copy of a map[string]any.
func CloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = CloneAny(value)
	}
	return out
}

// CloneAny returns a deep copy of an arbitrary value, handling nested maps and slices.
func CloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return CloneAnyMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = CloneAny(item)
		}
		return out
	default:
		return v
	}
}

// Backward-compatible unexported aliases for internal runtime use.
func cloneContext(in map[string]map[string]any) map[string]map[string]any { return CloneContext(in) }
func cloneFacts(in map[string]FactValue) map[string]FactValue             { return CloneFacts(in) }
func cloneExecution(in *Execution) *Execution                             { return CloneExecution(in) }
func cloneAnyMap(in map[string]any) map[string]any                        { return CloneAnyMap(in) }
func cloneAny(value any) any                                              { return CloneAny(value) }
