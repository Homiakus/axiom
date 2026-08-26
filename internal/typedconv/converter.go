package typedconv

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

var (
	inputPlanCache  sync.Map // reflect.Type -> *inputPlan
	outputPlanCache sync.Map // reflect.Type -> *outputPlan
)

type fieldSetter func(target reflect.Value, raw any) error

type fieldInputPlan struct {
	keys       []string
	setter     fieldSetter
	index      []int
	isEmbedded bool
	childPlan  *inputPlan
}

type inputPlan struct {
	targetType reflect.Type
	isPointer  bool
	elemType   reflect.Type
	isMap      bool
	mapKeyType reflect.Type
	mapValType reflect.Type
	fields     []fieldInputPlan
	customJSON bool
}

type fieldGetter func(src reflect.Value) (any, bool)

type fieldOutputPlan struct {
	key       string
	getter    fieldGetter
	index     []int
	childPlan *outputPlan
}

type outputPlan struct {
	srcType   reflect.Type
	isPointer bool
	elemType  reflect.Type
	isMap     bool
	fields    []fieldOutputPlan
}

// CompileInput builds or retrieves a cached compiled input conversion plan for type T.
func CompileInput[T any]() (func(map[string]any) (T, error), error) {
	typ := reflect.TypeFor[T]()
	plan, err := getInputPlan(typ)
	if err != nil {
		return nil, err
	}

	return func(input map[string]any) (T, error) {
		var zero T
		val, err := plan.convert(input)
		if err != nil {
			return zero, err
		}
		return val.Interface().(T), nil
	}, nil
}

// CompileOutput builds or retrieves a cached compiled output extraction plan for type T.
func CompileOutput[T any]() (func(T) (map[string]any, error), error) {
	typ := reflect.TypeFor[T]()
	plan, err := getOutputPlan(typ)
	if err != nil {
		return nil, err
	}

	return func(val T) (map[string]any, error) {
		return plan.convert(reflect.ValueOf(val)), nil
	}, nil
}

func getInputPlan(typ reflect.Type) (*inputPlan, error) {
	if cached, ok := inputPlanCache.Load(typ); ok {
		return cached.(*inputPlan), nil
	}

	plan := &inputPlan{targetType: typ}
	curr := typ
	if curr.Kind() == reflect.Pointer {
		plan.isPointer = true
		plan.elemType = curr.Elem()
		curr = plan.elemType
	}

	unmarshalerType := reflect.TypeFor[json.Unmarshaler]()
	if typ.Implements(unmarshalerType) || reflect.PointerTo(typ).Implements(unmarshalerType) {
		plan.customJSON = true
		inputPlanCache.Store(typ, plan)
		return plan, nil
	}

	if curr.Kind() == reflect.Map {
		if curr.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("axiom: typed activity input map key must be string, got %s", curr.Key())
		}
		plan.isMap = true
		plan.mapKeyType = curr.Key()
		plan.mapValType = curr.Elem()
		inputPlanCache.Store(typ, plan)
		return plan, nil
	}

	if curr.Kind() != reflect.Struct {
		return nil, fmt.Errorf("axiom: typed activity input must be struct or map[string]T, got %s", typ)
	}

	fields, err := buildInputFields(curr, nil)
	if err != nil {
		return nil, err
	}
	plan.fields = fields
	inputPlanCache.Store(typ, plan)
	return plan, nil
}

func buildInputFields(structType reflect.Type, baseIndex []int) ([]fieldInputPlan, error) {
	var result []fieldInputPlan

	for i := 0; i < structType.NumField(); i++ {
		sf := structType.Field(i)
		if !sf.IsExported() && !sf.Anonymous {
			continue
		}

		fieldIdx := append(append([]int(nil), baseIndex...), i)

		// Handle anonymous embedded struct without explicit tag
		axiomTag := sf.Tag.Get("axiom")
		jsonTag := sf.Tag.Get("json")
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct && axiomTag == "" && (jsonTag == "" || jsonTag == ",inline") {
			embeddedFields, err := buildInputFields(sf.Type, fieldIdx)
			if err != nil {
				return nil, err
			}
			result = append(result, embeddedFields...)
			continue
		}

		if axiomTag == "-" || jsonTag == "-" {
			continue
		}

		tagKey := parseTagKey(sf)
		if tagKey == "-" {
			continue
		}

		// Candidate keys for matching input
		keys := []string{tagKey}
		lowerKey := toLowerCamel(sf.Name)
		if lowerKey != tagKey && lowerKey != "-" {
			keys = append(keys, lowerKey)
		}
		if sf.Name != tagKey && sf.Name != lowerKey {
			keys = append(keys, sf.Name)
		}

		setter, childPlan, err := buildFieldSetter(sf.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", sf.Name, err)
		}

		result = append(result, fieldInputPlan{
			keys:      keys,
			setter:    setter,
			index:     fieldIdx,
			childPlan: childPlan,
		})
	}

	return result, nil
}

func parseTagKey(sf reflect.StructField) string {
	tag := sf.Tag.Get("axiom")
	if tag != "" {
		parts := strings.Split(tag, ",")
		return parts[0]
	}
	tag = sf.Tag.Get("json")
	if tag != "" {
		parts := strings.Split(tag, ",")
		return parts[0]
	}
	return toLowerCamel(sf.Name)
}

func toLowerCamel(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func buildFieldSetter(targetType reflect.Type) (fieldSetter, *inputPlan, error) {
	curr := targetType
	isPtr := false
	if curr.Kind() == reflect.Pointer {
		isPtr = true
		curr = curr.Elem()
	}

	switch curr.Kind() {
	case reflect.String:
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			var s string
			switch v := raw.(type) {
			case string:
				s = v
			case fmt.Stringer:
				s = v.String()
			default:
				s = fmt.Sprintf("%v", v)
			}
			if isPtr {
				target.Set(reflect.ValueOf(&s))
			} else {
				target.SetString(s)
			}
			return nil
		}, nil, nil

	case reflect.Bool:
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			var b bool
			switch v := raw.(type) {
			case bool:
				b = v
			case string:
				b = (v == "true" || v == "1")
			case int, int64, float64:
				b = (v != 0)
			default:
				return fmt.Errorf("cannot convert %T to bool", raw)
			}
			if isPtr {
				target.Set(reflect.ValueOf(&b))
			} else {
				target.SetBool(b)
			}
			return nil
		}, nil, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			n, err := coerceToInt64(raw)
			if err != nil {
				return err
			}
			if isPtr {
				ptr := reflect.New(curr)
				ptr.Elem().SetInt(n)
				target.Set(ptr)
			} else {
				target.SetInt(n)
			}
			return nil
		}, nil, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			n, err := coerceToUint64(raw)
			if err != nil {
				return err
			}
			if isPtr {
				ptr := reflect.New(curr)
				ptr.Elem().SetUint(n)
				target.Set(ptr)
			} else {
				target.SetUint(n)
			}
			return nil
		}, nil, nil

	case reflect.Float32, reflect.Float64:
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			f, err := coerceToFloat64(raw)
			if err != nil {
				return err
			}
			if isPtr {
				ptr := reflect.New(curr)
				ptr.Elem().SetFloat(f)
				target.Set(ptr)
			} else {
				target.SetFloat(f)
			}
			return nil
		}, nil, nil

	case reflect.Struct:
		childPlan, err := getInputPlan(targetType)
		if err != nil {
			return nil, nil, err
		}
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			m, ok := raw.(map[string]any)
			if !ok {
				// Fallback to JSON conversion if not a map[string]any
				data, err := json.Marshal(raw)
				if err != nil {
					return err
				}
				ptr := reflect.New(curr)
				if err := json.Unmarshal(data, ptr.Interface()); err != nil {
					return err
				}
				if isPtr {
					target.Set(ptr)
				} else {
					target.Set(ptr.Elem())
				}
				return nil
			}
			converted, err := childPlan.convert(m)
			if err != nil {
				return err
			}
			target.Set(converted)
			return nil
		}, childPlan, nil

	case reflect.Map:
		elemType := curr.Elem()
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			m, ok := raw.(map[string]any)
			if !ok {
				data, err := json.Marshal(raw)
				if err != nil {
					return err
				}
				ptr := reflect.New(curr)
				if err := json.Unmarshal(data, ptr.Interface()); err != nil {
					return err
				}
				if isPtr {
					target.Set(ptr)
				} else {
					target.Set(ptr.Elem())
				}
				return nil
			}
			resMap := reflect.MakeMapWithSize(curr, len(m))
			for k, v := range m {
				elemVal, err := convertValue(v, elemType)
				if err != nil {
					return err
				}
				resMap.SetMapIndex(reflect.ValueOf(k), elemVal)
			}
			if isPtr {
				ptr := reflect.New(curr)
				ptr.Elem().Set(resMap)
				target.Set(ptr)
			} else {
				target.Set(resMap)
			}
			return nil
		}, nil, nil

	case reflect.Slice:
		elemType := curr.Elem()
		return func(target reflect.Value, raw any) error {
			if raw == nil {
				return nil
			}
			s, ok := raw.([]any)
			if !ok {
				data, err := json.Marshal(raw)
				if err != nil {
					return err
				}
				ptr := reflect.New(curr)
				if err := json.Unmarshal(data, ptr.Interface()); err != nil {
					return err
				}
				if isPtr {
					target.Set(ptr)
				} else {
					target.Set(ptr.Elem())
				}
				return nil
			}
			resSlice := reflect.MakeSlice(curr, len(s), len(s))
			for idx, item := range s {
				elemVal, err := convertValue(item, elemType)
				if err != nil {
					return err
				}
				resSlice.Index(idx).Set(elemVal)
			}
			if isPtr {
				ptr := reflect.New(curr)
				ptr.Elem().Set(resSlice)
				target.Set(ptr)
			} else {
				target.Set(resSlice)
			}
			return nil
		}, nil, nil

	default: // reflect.Interface or any
		return func(target reflect.Value, raw any) error {
			if raw != nil {
				target.Set(reflect.ValueOf(raw))
			}
			return nil
		}, nil, nil
	}
}

func convertValue(raw any, targetType reflect.Type) (reflect.Value, error) {
	if raw == nil {
		return reflect.Zero(targetType), nil
	}
	rawVal := reflect.ValueOf(raw)
	if rawVal.Type().AssignableTo(targetType) {
		return rawVal, nil
	}
	if rawVal.Type().ConvertibleTo(targetType) {
		return rawVal.Convert(targetType), nil
	}

	// Fallback conversion for numbers
	switch targetType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := coerceToInt64(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		res := reflect.New(targetType).Elem()
		res.SetInt(n)
		return res, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := coerceToUint64(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		res := reflect.New(targetType).Elem()
		res.SetUint(n)
		return res, nil
	case reflect.Float32, reflect.Float64:
		f, err := coerceToFloat64(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		res := reflect.New(targetType).Elem()
		res.SetFloat(f)
		return res, nil
	case reflect.String:
		res := reflect.New(targetType).Elem()
		res.SetString(fmt.Sprintf("%v", raw))
		return res, nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return reflect.Value{}, err
	}
	ptr := reflect.New(targetType)
	if err := json.Unmarshal(data, ptr.Interface()); err != nil {
		return reflect.Value{}, err
	}
	return ptr.Elem(), nil
}

func coerceToInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case int32:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case uint:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("cannot coerce %T (%v) to int64", raw, raw)
	}
}

func coerceToUint64(raw any) (uint64, error) {
	switch v := raw.(type) {
	case uint:
		return uint64(v), nil
	case uint64:
		return v, nil
	case uint32:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case int:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case float64:
		return uint64(v), nil
	case json.Number:
		n, err := v.Int64()
		return uint64(n), err
	case string:
		return strconv.ParseUint(v, 10, 64)
	default:
		return 0, fmt.Errorf("cannot coerce %T (%v) to uint64", raw, raw)
	}
}

func coerceToFloat64(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("cannot coerce %T (%v) to float64", raw, raw)
	}
}

func (p *inputPlan) convert(input map[string]any) (reflect.Value, error) {
	if p.customJSON {
		data, err := json.Marshal(input)
		if err != nil {
			return reflect.Value{}, err
		}
		ptr := reflect.New(p.targetType)
		if err := json.Unmarshal(data, ptr.Interface()); err != nil {
			return reflect.Value{}, err
		}
		return ptr.Elem(), nil
	}

	if p.isMap {
		if input == nil {
			if p.isPointer {
				return reflect.Zero(p.targetType), nil
			}
			return reflect.MakeMap(p.targetType), nil
		}
		targetMapType := p.targetType
		if p.isPointer {
			targetMapType = p.elemType
		}
		resMap := reflect.MakeMapWithSize(targetMapType, len(input))
		for k, v := range input {
			elemVal, err := convertValue(v, p.mapValType)
			if err != nil {
				return reflect.Value{}, err
			}
			resMap.SetMapIndex(reflect.ValueOf(k), elemVal)
		}
		if p.isPointer {
			ptr := reflect.New(targetMapType)
			ptr.Elem().Set(resMap)
			return ptr, nil
		}
		return resMap, nil
	}

	var root reflect.Value
	if p.isPointer {
		root = reflect.New(p.elemType)
	} else {
		root = reflect.New(p.targetType).Elem()
	}

	val := root
	if p.isPointer {
		val = root.Elem()
	}

	for _, f := range p.fields {
		var rawVal any
		var found bool
		for _, key := range f.keys {
			if v, ok := input[key]; ok {
				rawVal = v
				found = true
				break
			}
		}
		if !found {
			continue
		}

		fieldVal := val.FieldByIndex(f.index)
		if err := f.setter(fieldVal, rawVal); err != nil {
			return reflect.Value{}, fmt.Errorf("key %s: %w", f.keys[0], err)
		}
	}

	return root, nil
}

func getOutputPlan(typ reflect.Type) (*outputPlan, error) {
	if cached, ok := outputPlanCache.Load(typ); ok {
		return cached.(*outputPlan), nil
	}

	plan := &outputPlan{srcType: typ}
	curr := typ
	if curr.Kind() == reflect.Pointer {
		plan.isPointer = true
		plan.elemType = curr.Elem()
		curr = plan.elemType
	}

	if curr.Kind() == reflect.Map {
		if curr.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("axiom: typed activity output map key must be string, got %s", curr.Key())
		}
		plan.isMap = true
		outputPlanCache.Store(typ, plan)
		return plan, nil
	}

	if curr.Kind() != reflect.Struct {
		return nil, fmt.Errorf("axiom: typed activity output must be struct or map[string]T, got %s", typ)
	}

	fields, err := buildOutputFields(curr, nil)
	if err != nil {
		return nil, err
	}
	plan.fields = fields
	outputPlanCache.Store(typ, plan)
	return plan, nil
}

func buildOutputFields(structType reflect.Type, baseIndex []int) ([]fieldOutputPlan, error) {
	var result []fieldOutputPlan

	for i := 0; i < structType.NumField(); i++ {
		sf := structType.Field(i)
		if !sf.IsExported() && !sf.Anonymous {
			continue
		}

		fieldIdx := append(append([]int(nil), baseIndex...), i)

		axiomTag := sf.Tag.Get("axiom")
		jsonTag := sf.Tag.Get("json")
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct && axiomTag == "" && (jsonTag == "" || jsonTag == ",inline") {
			embeddedFields, err := buildOutputFields(sf.Type, fieldIdx)
			if err != nil {
				return nil, err
			}
			result = append(result, embeddedFields...)
			continue
		}

		if axiomTag == "-" || jsonTag == "-" {
			continue
		}

		key := parseTagKey(sf)
		if key == "-" {
			continue
		}

		getter := func(src reflect.Value) (any, bool) {
			fieldVal := src.FieldByIndex(fieldIdx)
			return fieldVal.Interface(), true
		}

		result = append(result, fieldOutputPlan{
			key:    key,
			getter: getter,
			index:  fieldIdx,
		})
	}

	return result, nil
}

func (p *outputPlan) convert(src reflect.Value) map[string]any {
	if !src.IsValid() {
		return nil
	}

	if p.isPointer {
		if src.IsNil() {
			return nil
		}
		src = src.Elem()
	}

	if p.isMap {
		if src.IsNil() {
			return nil
		}
		out := make(map[string]any, src.Len())
		iter := src.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = iter.Value().Interface()
		}
		return out
	}

	out := make(map[string]any, len(p.fields))
	for _, f := range p.fields {
		val, ok := f.getter(src)
		if ok {
			out[f.key] = val
		}
	}
	return out
}
