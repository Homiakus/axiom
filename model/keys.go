package model

import (
	"fmt"
	"reflect"
	"strings"
)

// FieldKey is a reusable, typed reference to one direct field of a state or
// event Go type. It lets larger models declare field names once and reuse them
// without repeating string literals throughout rules, claims and activities.
//
// The Owner type prevents applying a key to the wrong state/event type. Value
// is also checked against the selected Go field when the key is used. For
// optional pointer fields, Value may be either the pointer type itself or the
// pointed-to logical value type.
type FieldKey[Owner any, Value any] struct {
	name string
}

// Key creates a reusable typed field key. name may be either the Go field name
// or the serialized axiom/json field name.
//
// Example:
//
//	var orderTotal = model.Key[Order, int]("Total")
//	total := model.StateField(order, orderTotal)
func Key[Owner any, Value any](name string) FieldKey[Owner, Value] {
	return FieldKey[Owner, Value]{name: strings.TrimSpace(name)}
}

// Name returns the configured Go or serialized field name.
func (k FieldKey[Owner, Value]) Name() string { return k.name }

// StateField resolves a reusable field key against a typed state.
func StateField[S any, V any](state TypedState[S], key FieldKey[S, V]) TypedField[V] {
	if state.Ref == nil || state.Ref.definition == nil {
		return TypedField[V]{expr: Ref("__invalid_state_field__." + key.name)}
	}
	declaration := state.Ref.definition.states[state.Ref.index]
	if !validateFieldKey[S, V](state.Ref.definition, declaration, key.name, "state") {
		return TypedField[V]{expr: Ref("__invalid_state_field__." + key.name)}
	}
	return TypedField[V]{expr: state.Ref.Field(key.name)}
}

// EventField resolves a reusable field key against a typed event.
func EventField[E any, V any](event TypedEvent[E], key FieldKey[E, V]) TypedField[V] {
	if event.Ref == nil || event.Ref.definition == nil {
		return TypedField[V]{expr: Ref("signal.__invalid_event_field__." + key.name)}
	}
	declaration := event.Ref.definition.events[event.Ref.index]
	if !validateFieldKey[E, V](event.Ref.definition, declaration, key.name, "event") {
		return TypedField[V]{expr: Ref("signal.__invalid_event_field__." + key.name)}
	}
	return TypedField[V]{expr: event.Ref.Field(key.name)}
}

// StateChanged creates a changed(...) trigger from a reusable field key.
func StateChanged[S any, V any](state TypedState[S], key FieldKey[S, V]) Trigger {
	return OnChanged(StateField(state, key).Expr())
}

// StateDefault sets a default using a reusable field key. The value type is
// checked by Go at the call site and the key is validated against the state
// struct before the model is compiled.
func StateDefault[S any, V any](state TypedState[S], key FieldKey[S, V], value V) TypedState[S] {
	if state.Ref == nil || state.Ref.definition == nil {
		return state
	}
	declaration := state.Ref.definition.states[state.Ref.index]
	if !validateFieldKey[S, V](state.Ref.definition, declaration, key.name, "state") {
		return state
	}
	state.Ref.Default(key.name, value)
	return state
}

func validateFieldKey[Owner any, Value any](definition *Definition, declaration schemaDecl, name, role string) bool {
	if name == "" {
		definition.addBuilderDiagnostic(
			declaration.name,
			role+" field key must not be empty",
			"Create the key with model.Key[Owner, Value](\"GoFieldName\") or the serialized axiom/json field name.",
		)
		return false
	}

	ownerType := reflect.TypeFor[Owner]()
	for ownerType.Kind() == reflect.Pointer {
		ownerType = ownerType.Elem()
	}
	if ownerType.Kind() != reflect.Struct {
		definition.addBuilderDiagnostic(
			declaration.name+"."+name,
			fmt.Sprintf("%s field key owner must be a struct; got %s", role, ownerType),
			"Use the same struct type that was passed to model.Bind or model.EventOf.",
		)
		return false
	}

	for index := 0; index < ownerType.NumField(); index++ {
		field := ownerType.Field(index)
		if !field.IsExported() {
			continue
		}
		serialized := serializedFieldName(field)
		if serialized == "-" {
			continue
		}
		if field.Name != name && serialized != name {
			continue
		}

		valueType := reflect.TypeFor[Value]()
		if fieldKeyTypeMatches(field.Type, valueType) {
			return true
		}
		definition.addBuilderDiagnostic(
			declaration.name+"."+name,
			fmt.Sprintf("typed field key for %s.%s uses %s, but the Go field type is %s", declaration.name, name, valueType, field.Type),
			"Change the Value type in model.Key[Owner, Value] to match the Go field (or its pointed-to type for optional pointer fields).",
		)
		return false
	}

	definition.addBuilderDiagnostic(
		declaration.name+"."+name,
		"unknown "+role+" field "+declaration.name+"."+name,
		"Use the Go field name or its axiom/json serialized name.",
	)
	return false
}

func fieldKeyTypeMatches(fieldType, valueType reflect.Type) bool {
	if fieldType == valueType {
		return true
	}
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	return fieldType == valueType
}

func serializedFieldName(field reflect.StructField) string {
	serialized := field.Tag.Get("axiom")
	if serialized == "" {
		serialized = strings.Split(field.Tag.Get("json"), ",")[0]
	}
	if serialized == "" {
		serialized = lowerFirst(field.Name)
	}
	return serialized
}
