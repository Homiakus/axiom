package runtime

import "reflect"

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

// CloneAny returns an isolated copy of maps, slices and arrays, including
// named and typed collections stored behind interface values. Pointer values
// are intentionally treated as opaque application objects.
func CloneAny(value any) any {
	cloned := cloneCollection(reflect.ValueOf(value))
	if !cloned.IsValid() {
		return nil
	}
	return cloned.Interface()
}

func cloneCollection(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneCollection(value.Elem())
		out := reflect.New(value.Type()).Elem()
		out.Set(cloned)
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned := cloneCollection(iter.Value())
			if cloned.Type().AssignableTo(value.Type().Elem()) || value.Type().Elem().Kind() == reflect.Interface {
				out.SetMapIndex(iter.Key(), cloned)
			} else {
				out.SetMapIndex(iter.Key(), iter.Value())
			}
		}
		return out
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		out := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned := cloneCollection(value.Index(i))
			if cloned.Type().AssignableTo(out.Index(i).Type()) {
				out.Index(i).Set(cloned)
			} else {
				out.Index(i).Set(value.Index(i))
			}
		}
		return out
	case reflect.Array:
		out := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned := cloneCollection(value.Index(i))
			if cloned.Type().AssignableTo(out.Index(i).Type()) {
				out.Index(i).Set(cloned)
			} else {
				out.Index(i).Set(value.Index(i))
			}
		}
		return out
	default:
		return value
	}
}

// Backward-compatible unexported aliases for internal runtime use.
func cloneContext(in map[string]map[string]any) map[string]map[string]any { return CloneContext(in) }
func cloneFacts(in map[string]FactValue) map[string]FactValue             { return CloneFacts(in) }
func cloneExecution(in *Execution) *Execution                             { return CloneExecution(in) }
func cloneAnyMap(in map[string]any) map[string]any                        { return CloneAnyMap(in) }
func cloneAny(value any) any                                              { return CloneAny(value) }
