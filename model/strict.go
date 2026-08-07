package model

// Equal compares a typed field with a Go value of the same type.
//
// Prefer Equal over EQ when comparing a field with a literal value: the Go
// compiler then catches accidental cross-type comparisons before Axiom builds
// the expression.
func (f TypedField[T]) Equal(value T) Expr {
	return Eq(f.expr, Lit(value))
}

// NotEqual compares a typed field with a Go value of the same type.
func (f TypedField[T]) NotEqual(value T) Expr {
	return Ne(f.expr, Lit(value))
}

// GreaterThan compares a typed field with a Go value of the same type.
func (f TypedField[T]) GreaterThan(value T) Expr {
	return GT(f.expr, Lit(value))
}

// GreaterOrEqual compares a typed field with a Go value of the same type.
func (f TypedField[T]) GreaterOrEqual(value T) Expr {
	return GTE(f.expr, Lit(value))
}

// LessThan compares a typed field with a Go value of the same type.
func (f TypedField[T]) LessThan(value T) Expr {
	return LT(f.expr, Lit(value))
}

// LessOrEqual compares a typed field with a Go value of the same type.
func (f TypedField[T]) LessOrEqual(value T) Expr {
	return LTE(f.expr, Lit(value))
}

// Plus adds a Go value of the same type to a typed field expression.
func (f TypedField[T]) Plus(value T) Expr {
	return Add(f.expr, Lit(value))
}

// Minus subtracts a Go value of the same type from a typed field expression.
func (f TypedField[T]) Minus(value T) Expr {
	return Sub(f.expr, Lit(value))
}

// Times multiplies a typed field expression by a Go value of the same type.
func (f TypedField[T]) Times(value T) Expr {
	return Mul(f.expr, Lit(value))
}

// DividedBy divides a typed field expression by a Go value of the same type.
func (f TypedField[T]) DividedBy(value T) Expr {
	return Div(f.expr, Lit(value))
}

// Modulo applies the modulo operator with a Go value of the same type.
func (f TypedField[T]) Modulo(value T) Expr {
	return Mod(f.expr, Lit(value))
}

// EqualField compares two typed fields with the same Go type.
func (f TypedField[T]) EqualField(other TypedField[T]) Expr {
	return Eq(f.expr, other.expr)
}

// NotEqualField compares two typed fields with the same Go type.
func (f TypedField[T]) NotEqualField(other TypedField[T]) Expr {
	return Ne(f.expr, other.expr)
}
