package runtime

import "strconv"

func valueOf(value any) Value {
	if integer, ok := signedInteger(value); ok {
		return Value{Kind: ValueInt, I64: integer}
	}
	switch v := value.(type) {
	case nil:
		return Value{Kind: ValueNull}
	case bool:
		return Value{Kind: ValueBool, B: v}
	case float32:
		return Value{Kind: ValueFloat, F64: float64(v)}
	case float64:
		return Value{Kind: ValueFloat, F64: v}
	case string:
		return Value{Kind: ValueString, S: v}
	default:
		return Value{Kind: ValueAny, Any: cloneAny(value)}
	}
}

func (v Value) Interface() any {
	switch v.Kind {
	case ValueNull:
		return nil
	case ValueBool:
		return v.B
	case ValueInt:
		if strconv.IntSize == 32 && (v.I64 < -1<<31 || v.I64 > 1<<31-1) {
			return v.I64
		}
		return int(v.I64)
	case ValueFloat:
		return v.F64
	case ValueString:
		return v.S
	case ValueAny:
		return cloneAny(v.Any)
	default:
		return nil
	}
}

func ensureExecutionState(execution *Execution, fieldCount int, atomCount int) {
	if execution == nil {
		return
	}
	fieldBits := newBitset(fieldCount)
	if len(execution.RuntimeState.Present) != len(fieldBits) {
		next := fieldBits
		next.or(bitset(execution.RuntimeState.Present))
		execution.RuntimeState.Present = []uint64(next)
	}
	if len(execution.RuntimeState.BoolValues) != len(fieldBits) {
		next := fieldBits
		next.or(bitset(execution.RuntimeState.BoolValues))
		execution.RuntimeState.BoolValues = []uint64(next)
	}
	if len(execution.RuntimeState.DirtyFields) != len(fieldBits) {
		next := fieldBits
		next.or(bitset(execution.RuntimeState.DirtyFields))
		execution.RuntimeState.DirtyFields = []uint64(next)
	}
	atomBits := newBitset(atomCount)
	if len(execution.RuntimeState.ActiveAtoms) != len(atomBits) {
		next := atomBits
		next.or(bitset(execution.RuntimeState.ActiveAtoms))
		execution.RuntimeState.ActiveAtoms = []uint64(next)
	}
	if execution.RuntimeState.Values == nil {
		execution.RuntimeState.Values = map[uint32]Value{}
	}
	if execution.RuntimeState.AtomValues == nil {
		execution.RuntimeState.AtomValues = map[uint32]Value{}
	}
	if execution.RuntimeState.FactValues == nil {
		execution.RuntimeState.FactValues = map[uint32]map[string]Value{}
	}
}

func cloneExecutionState(in ExecutionState) ExecutionState {
	out := ExecutionState{
		ActiveAtoms: append([]uint64{}, in.ActiveAtoms...),
		Present:     append([]uint64{}, in.Present...),
		BoolValues:  append([]uint64{}, in.BoolValues...),
		DirtyFields: append([]uint64{}, in.DirtyFields...),
	}
	if in.Values != nil {
		out.Values = make(map[uint32]Value, len(in.Values))
		for key, value := range in.Values {
			out.Values[key] = cloneValue(value)
		}
	}
	if in.AtomValues != nil {
		out.AtomValues = make(map[uint32]Value, len(in.AtomValues))
		for key, value := range in.AtomValues {
			out.AtomValues[key] = cloneValue(value)
		}
	}
	if in.FactValues != nil {
		out.FactValues = make(map[uint32]map[string]Value, len(in.FactValues))
		for atomID, values := range in.FactValues {
			next := make(map[string]Value, len(values))
			for name, value := range values {
				next[name] = cloneValue(value)
			}
			out.FactValues[atomID] = next
		}
	}
	return out
}

func cloneValue(value Value) Value {
	if value.Kind == ValueAny {
		value.Any = cloneAny(value.Any)
	}
	return value
}
