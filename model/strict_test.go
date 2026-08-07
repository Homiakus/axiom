package model

import "testing"

func TestTypedFieldStrictLiteralHelpers(t *testing.T) {
	field := Field[int](Ref("Counter.value"))

	tests := map[string]Expr{
		"equal":            field.Equal(7),
		"not equal":        field.NotEqual(7),
		"greater":          field.GreaterThan(7),
		"greater or equal": field.GreaterOrEqual(7),
		"less":             field.LessThan(7),
		"less or equal":    field.LessOrEqual(7),
		"plus":             field.Plus(7),
		"minus":            field.Minus(7),
		"times":            field.Times(7),
		"divided by":       field.DividedBy(7),
		"modulo":           field.Modulo(7),
	}

	want := map[string]string{
		"equal":            "(Counter.value == 7)",
		"not equal":        "(Counter.value != 7)",
		"greater":          "(Counter.value > 7)",
		"greater or equal": "(Counter.value >= 7)",
		"less":             "(Counter.value < 7)",
		"less or equal":    "(Counter.value <= 7)",
		"plus":             "(Counter.value + 7)",
		"minus":            "(Counter.value - 7)",
		"times":            "(Counter.value * 7)",
		"divided by":       "(Counter.value / 7)",
		"modulo":           "(Counter.value % 7)",
	}

	for name, expression := range tests {
		if got := expression.String(); got != want[name] {
			t.Fatalf("%s: got %q, want %q", name, got, want[name])
		}
	}
}

func TestTypedFieldStrictFieldHelpers(t *testing.T) {
	left := Field[int](Ref("Counter.left"))
	right := Field[int](Ref("Counter.right"))

	tests := map[string]Expr{
		"equal":            left.EqualField(right),
		"not equal":        left.NotEqualField(right),
		"greater":          left.GreaterThanField(right),
		"greater or equal": left.GreaterOrEqualField(right),
		"less":             left.LessThanField(right),
		"less or equal":    left.LessOrEqualField(right),
		"plus":             left.PlusField(right),
		"minus":            left.MinusField(right),
		"times":            left.TimesField(right),
		"divided by":       left.DividedByField(right),
		"modulo":           left.ModuloField(right),
	}

	want := map[string]string{
		"equal":            "(Counter.left == Counter.right)",
		"not equal":        "(Counter.left != Counter.right)",
		"greater":          "(Counter.left > Counter.right)",
		"greater or equal": "(Counter.left >= Counter.right)",
		"less":             "(Counter.left < Counter.right)",
		"less or equal":    "(Counter.left <= Counter.right)",
		"plus":             "(Counter.left + Counter.right)",
		"minus":            "(Counter.left - Counter.right)",
		"times":            "(Counter.left * Counter.right)",
		"divided by":       "(Counter.left / Counter.right)",
		"modulo":           "(Counter.left % Counter.right)",
	}

	for name, expression := range tests {
		if got := expression.String(); got != want[name] {
			t.Fatalf("%s: got %q, want %q", name, got, want[name])
		}
	}
}
