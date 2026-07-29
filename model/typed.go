package model

import (
	"reflect"
)

// Exprable is an interface for types that can produce a model.Expr.
type Exprable interface {
	Expr() Expr
}

// TypedField is a type-safe wrapper over a field expression supporting fluent operators.
type TypedField[T any] struct {
	expr Expr
}

func Field[T any](expr Expr) TypedField[T] {
	return TypedField[T]{expr: expr}
}

func (f TypedField[T]) Expr() Expr { return f.expr }

func (f TypedField[T]) EQ(val any) Expr  { return Eq(f.expr, toExpr(val)) }
func (f TypedField[T]) NE(val any) Expr  { return Ne(f.expr, toExpr(val)) }
func (f TypedField[T]) GT(val any) Expr  { return GT(f.expr, toExpr(val)) }
func (f TypedField[T]) GTE(val any) Expr { return GTE(f.expr, toExpr(val)) }
func (f TypedField[T]) LT(val any) Expr  { return LT(f.expr, toExpr(val)) }
func (f TypedField[T]) LTE(val any) Expr { return LTE(f.expr, toExpr(val)) }

func (f TypedField[T]) Add(val any) Expr { return Add(f.expr, toExpr(val)) }
func (f TypedField[T]) Sub(val any) Expr { return Sub(f.expr, toExpr(val)) }
func (f TypedField[T]) Mul(val any) Expr { return Mul(f.expr, toExpr(val)) }
func (f TypedField[T]) Div(val any) Expr { return Div(f.expr, toExpr(val)) }
func (f TypedField[T]) Mod(val any) Expr { return Mod(f.expr, toExpr(val)) }

// TypedState wraps a StateRef with type-safe field accessor helpers.
type TypedState[S any] struct {
	Ref *StateRef[S]
}

// Bind creates a typed state declaration and returns a TypedState accessor.
func Bind[S any](d *Definition, name string) TypedState[S] {
	ref := State[S](d, name)
	return TypedState[S]{Ref: ref}
}

func (s TypedState[S]) Default(name string, value any) TypedState[S] {
	s.Ref.Default(name, value)
	return s
}

func (s TypedState[S]) Field(name string) Expr {
	return s.Ref.Field(name)
}

func (s TypedState[S]) Int(name string) TypedField[int] {
	return TypedField[int]{expr: s.Ref.Field(name)}
}

func (s TypedState[S]) String(name string) TypedField[string] {
	return TypedField[string]{expr: s.Ref.Field(name)}
}

func (s TypedState[S]) Bool(name string) TypedField[bool] {
	return TypedField[bool]{expr: s.Ref.Field(name)}
}

func (s TypedState[S]) Float(name string) TypedField[float64] {
	return TypedField[float64]{expr: s.Ref.Field(name)}
}

// TypedEvent wraps an EventRef with type-safe field accessor helpers.
type TypedEvent[E any] struct {
	Ref *EventRef[E]
}

// EventOf registers an event schema by automatically inferring the event name from Go type E.
func EventOf[E any](d *Definition) TypedEvent[E] {
	typ := reflect.TypeFor[E]()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	name := typ.Name()
	return EventNamed[E](d, name)
}

// EventNamed registers an event schema with an explicit event name.
func EventNamed[E any](d *Definition, name string) TypedEvent[E] {
	ref := Event[E](d, name)
	return TypedEvent[E]{Ref: ref}
}

func (e TypedEvent[E]) Trigger() Trigger {
	return e.Ref.Trigger()
}

func (e TypedEvent[E]) Field(name string) Expr {
	return e.Ref.Field(name)
}

func (e TypedEvent[E]) Int(name string) TypedField[int] {
	return TypedField[int]{expr: e.Ref.Field(name)}
}

func (e TypedEvent[E]) String(name string) TypedField[string] {
	return TypedField[string]{expr: e.Ref.Field(name)}
}

func (e TypedEvent[E]) Bool(name string) TypedField[bool] {
	return TypedField[bool]{expr: e.Ref.Field(name)}
}

func (e TypedEvent[E]) Float(name string) TypedField[float64] {
	return TypedField[float64]{expr: e.Ref.Field(name)}
}

// OutputRef references an activity output property.
func OutputRef(name string) Expr {
	return Ref("output." + name)
}

func OutputInt(name string) TypedField[int] {
	return TypedField[int]{expr: OutputRef(name)}
}

func OutputBool(name string) TypedField[bool] {
	return TypedField[bool]{expr: OutputRef(name)}
}

func OutputString(name string) TypedField[string] {
	return TypedField[string]{expr: OutputRef(name)}
}

func toExpr(val any) Expr {
	if val == nil {
		return Lit(nil)
	}
	if e, ok := val.(Expr); ok {
		return e
	}
	if e, ok := val.(Exprable); ok {
		return e.Expr()
	}
	return Lit(val)
}
