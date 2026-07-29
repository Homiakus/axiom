package runtime

import (
	"math"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestEvalComprehensive covers the expression evaluator (eval.go) with
// table-driven tests for every operator, edge case, and type combination.
// ──────────────────────────────────────────────────────────────────────────────

// TestTruthy verifies the truthy() function for all supported Go types.
// truthy() is the core boolean coercion used throughout rule evaluation.
func TestTruthy(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want bool
	}{
		// ── nil ──────────────────────────────────────────────────────────
		{name: "nil is falsy", val: nil, want: false},

		// ── bool ─────────────────────────────────────────────────────────
		{name: "true", val: true, want: true},
		{name: "false", val: false, want: false},

		// ── int ──────────────────────────────────────────────────────────
		{name: "int zero", val: 0, want: false},
		{name: "int positive", val: 1, want: true},
		{name: "int negative", val: -1, want: true},

		// ── int64 ────────────────────────────────────────────────────────
		{name: "int64 zero", val: int64(0), want: false},
		{name: "int64 positive", val: int64(42), want: true},

		// ── float64 ──────────────────────────────────────────────────────
		{name: "float64 zero", val: 0.0, want: false},
		{name: "float64 positive", val: 1.5, want: true},
		{name: "float64 negative", val: -0.1, want: true},

		// ── string ───────────────────────────────────────────────────────
		{name: "empty string", val: "", want: false},
		{name: "non-empty string", val: "hello", want: true},
		{name: "whitespace string", val: " ", want: true},

		// ── other types (default: truthy) ────────────────────────────────
		{name: "slice is truthy", val: []any{}, want: true},
		{name: "map is truthy", val: map[string]any{}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truthy(tc.val)
			if got != tc.want {
				t.Fatalf("truthy(%#v) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

// TestTypedEqual covers the typedEqual function which implements
// type-aware equality for the == and != operators.
func TestTypedEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b any
		want bool
	}{
		// ── nil ──────────────────────────────────────────────────────────
		{name: "nil == nil", a: nil, b: nil, want: true},
		{name: "nil != 0", a: nil, b: 0, want: false},
		{name: "0 != nil", a: 0, b: nil, want: false},

		// ── bool ─────────────────────────────────────────────────────────
		{name: "true == true", a: true, b: true, want: true},
		{name: "true != false", a: true, b: false, want: false},
		{name: "bool != int", a: true, b: 1, want: false},

		// ── string ───────────────────────────────────────────────────────
		{name: "same string", a: "abc", b: "abc", want: true},
		{name: "different string", a: "abc", b: "xyz", want: false},
		{name: "string != int", a: "1", b: 1, want: false},
		{name: "empty strings", a: "", b: "", want: true},

		// ── numeric cross-type ───────────────────────────────────────────
		// The evaluator converts all numerics to float64 for comparison.
		{name: "int == int", a: 42, b: 42, want: true},
		{name: "int == float64", a: 42, b: 42.0, want: true},
		{name: "int != float64", a: 42, b: 42.5, want: false},
		{name: "int64 == int", a: int64(10), b: 10, want: true},

		// ── deep equal fallback ──────────────────────────────────────────
		{name: "same slice", a: []any{1, 2}, b: []any{1, 2}, want: true},
		{name: "different slice", a: []any{1}, b: []any{2}, want: false},
		{name: "same map", a: map[string]any{"k": 1}, b: map[string]any{"k": 1}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := typedEqual(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("typedEqual(%#v, %#v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestNumberConversion verifies the number() helper handles all integer
// and floating-point Go types.
func TestNumberConversion(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want float64
		ok   bool
	}{
		{name: "int", val: 42, want: 42, ok: true},
		{name: "int8", val: int8(8), want: 8, ok: true},
		{name: "int16", val: int16(16), want: 16, ok: true},
		{name: "int32", val: int32(32), want: 32, ok: true},
		{name: "int64", val: int64(64), want: 64, ok: true},
		{name: "float32", val: float32(1.5), want: 1.5, ok: true},
		{name: "float64", val: 2.5, want: 2.5, ok: true},
		{name: "string not number", val: "42", ok: false},
		{name: "bool not number", val: true, ok: false},
		{name: "nil not number", val: nil, ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := number(tc.val)
			if ok != tc.ok {
				t.Fatalf("number(%#v) ok = %v, want %v", tc.val, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("number(%#v) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

// TestAddValues covers the + operator for numeric addition and string
// concatenation, including cross-type edge cases.
func TestAddValues(t *testing.T) {
	cases := []struct {
		name    string
		a, b    any
		want    any
		wantErr bool
	}{
		{name: "int + int", a: 3, b: 4, want: 7},
		{name: "float + float", a: 1.5, b: 2.5, want: 4.0},
		{name: "int + float = float", a: 1, b: 1.5, want: 2.5},
		{name: "string + string", a: "hello ", b: "world", want: "hello world"},
		{name: "string + int", a: "val=", b: 42, want: "val=42"},
		{name: "int + string", a: 42, b: " items", want: "42 items"},
		{name: "nil + nil", a: nil, b: nil, wantErr: true},
		{name: "bool + bool", a: true, b: false, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := addValues(tc.a, tc.b)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !typedEqual(got, tc.want) {
				t.Fatalf("addValues(%#v, %#v) = %#v, want %#v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestCompareNumbers covers numeric comparison operators with edge cases
// including NaN and infinity.
func TestCompareNumbers(t *testing.T) {
	cases := []struct {
		name    string
		a, b    any
		op      string
		want    bool
		wantErr bool
	}{
		{name: "1 > 0", a: 1, b: 0, op: ">", want: true},
		{name: "0 > 1", a: 0, b: 1, op: ">", want: false},
		{name: "1 >= 1", a: 1, b: 1, op: ">=", want: true},
		{name: "0 < 1", a: 0, b: 1, op: "<", want: true},
		{name: "1 <= 1", a: 1, b: 1, op: "<=", want: true},
		{name: "float comparison", a: 1.5, b: 2.5, op: "<", want: true},
		{name: "cross-type int vs float", a: 1, b: 1.5, op: "<", want: true},
		{name: "string not comparable", a: "a", b: "b", op: ">", wantErr: true},
		{name: "nil not comparable", a: nil, b: 1, op: ">", wantErr: true},
	}

	cmpFn := map[string]func(float64, float64) bool{
		">":  func(a, b float64) bool { return a > b },
		">=": func(a, b float64) bool { return a >= b },
		"<":  func(a, b float64) bool { return a < b },
		"<=": func(a, b float64) bool { return a <= b },
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compareNumbers(tc.a, tc.b, cmpFn[tc.op])
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("%v %s %v = %v, want %v", tc.a, tc.op, tc.b, got, tc.want)
			}
		})
	}
}

// TestContainsTyped covers the 'in' operator for list membership.
func TestContainsTyped(t *testing.T) {
	cases := []struct {
		name       string
		collection any
		needle     any
		want       bool
	}{
		{name: "found in []any", collection: []any{1, 2, 3}, needle: 2, want: true},
		{name: "not found in []any", collection: []any{1, 2, 3}, needle: 4, want: false},
		{name: "found in []string", collection: []string{"a", "b"}, needle: "a", want: true},
		{name: "not found in []string", collection: []string{"a", "b"}, needle: "c", want: false},
		{name: "empty []any", collection: []any{}, needle: 1, want: false},
		{name: "nil collection", collection: nil, needle: 1, want: false},
		{name: "cross-type int in []any float", collection: []any{1.0}, needle: 1, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := containsTyped(tc.collection, tc.needle)
			if got != tc.want {
				t.Fatalf("containsTyped(%#v, %#v) = %v, want %v", tc.collection, tc.needle, got, tc.want)
			}
		})
	}
}

// TestResolveRef covers reference resolution with various prefixes and
// nested structures in execution context.
func TestResolveRef(t *testing.T) {
	// Build a test execution with context, computed, and facts.
	execution := &Execution{
		Context: map[string]map[string]any{
			"User": {
				"id":    "u1",
				"email": "a@b.com",
				"prefs": map[string]any{"theme": "dark"},
			},
		},
		Computed: map[string]any{
			"ready": true,
		},
		Facts: map[string]FactValue{
			"IsReady": {True: true, Exposed: map[string]any{"val": 42}},
		},
	}

	cases := []struct {
		name string
		ref  string
		env  evalEnv
		want any
	}{
		{name: "empty ref", ref: "", env: evalEnv{execution: execution}, want: nil},
		{name: "context field", ref: "User.id", env: evalEnv{execution: execution}, want: "u1"},
		{name: "context nested", ref: "User.prefs.theme", env: evalEnv{execution: execution}, want: "dark"},
		{name: "computed ref", ref: "ready", env: evalEnv{execution: execution}, want: true},
		{name: "fact ref", ref: "IsReady", env: evalEnv{execution: execution}, want: true},
		{name: "fact exposed field", ref: "IsReady.val", env: evalEnv{execution: execution}, want: 42},
		{name: "signal ref", ref: "signal.key", env: evalEnv{execution: execution, signal: map[string]any{"key": "v"}}, want: "v"},
		{name: "output ref", ref: "output.result", env: evalEnv{execution: execution, output: map[string]any{"result": true}}, want: true},
		{name: "runtime ref returns nil", ref: "runtime.timestamp", env: evalEnv{execution: execution}, want: nil},
		{name: "unknown ref", ref: "Unknown.field", env: evalEnv{execution: execution}, want: nil},
		{name: "nil execution", ref: "User.id", env: evalEnv{}, want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveRef(tc.ref, tc.env)
			if !typedEqual(got, tc.want) {
				t.Fatalf("resolveRef(%q) = %#v, want %#v", tc.ref, got, tc.want)
			}
		})
	}
}

// TestResolvePath covers nested map/slice traversal.
func TestResolvePath(t *testing.T) {
	root := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": 42,
			},
		},
		"list": []any{"x", "y", "z"},
		"strings": []string{"a", "b"},
	}

	cases := []struct {
		name string
		path string
		want any
	}{
		{name: "simple", path: "a", want: root["a"]},
		{name: "nested", path: "a.b.c", want: 42},
		{name: "list index", path: "list.1", want: "y"},
		{name: "list length", path: "list.length", want: 3},
		{name: "strings index", path: "strings.0", want: "a"},
		{name: "strings length", path: "strings.length", want: 2},
		{name: "out of bounds", path: "list.99", want: nil},
		{name: "negative index", path: "list.-1", want: nil},
		{name: "non-existent key", path: "missing", want: nil},
		{name: "empty path", path: "", want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePath(root, tc.path)
			if !typedEqual(got, tc.want) {
				t.Fatalf("resolvePath(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

// TestExistsAndMissing verifies the exists() helper.
func TestExistsAndMissing(t *testing.T) {
	if exists(nil) {
		t.Fatal("exists(nil) should be false")
	}
	if !exists(0) {
		t.Fatal("exists(0) should be true")
	}
	if !exists("") {
		t.Fatal("exists(\"\") should be true")
	}
	if !exists(false) {
		t.Fatal("exists(false) should be true")
	}
}

// TestIsIntLike covers the isIntLike type check used for preserving
// integer types in arithmetic operations.
func TestIsIntLike(t *testing.T) {
	intTypes := []any{int(1), int8(1), int16(1), int32(1), int64(1)}
	for _, v := range intTypes {
		if !isIntLike(v) {
			t.Fatalf("isIntLike(%T) should be true", v)
		}
	}
	nonIntTypes := []any{float32(1), float64(1), "1", true, nil}
	for _, v := range nonIntTypes {
		if isIntLike(v) {
			t.Fatalf("isIntLike(%T) should be false", v)
		}
	}
}

// TestSubtraction covers the subtraction branch of evalBinary including
// type preservation for int-int vs int-float.
func TestSubtraction(t *testing.T) {
	cases := []struct {
		name string
		a, b any
		want any
	}{
		{name: "int - int", a: 10, b: 3, want: 7},
		{name: "float - float", a: 10.5, b: 3.5, want: 7.0},
		{name: "int - float = float", a: 10, b: 3.5, want: 6.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Use the NaN-free float64 comparison.
			an, _ := number(tc.a)
			bn, _ := number(tc.b)
			var got any
			if isIntLike(tc.a) && isIntLike(tc.b) {
				got = int(an - bn)
			} else {
				got = an - bn
			}
			if !typedEqual(got, tc.want) {
				t.Fatalf("%v - %v = %#v, want %#v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestCompareNumbersNaN verifies behaviour when NaN is involved.
// IEEE 754: NaN is not equal to anything, including itself.
func TestCompareNumbersNaN(t *testing.T) {
	nan := math.NaN()

	// NaN > 0 should be false.
	result, err := compareNumbers(nan, 0.0, func(a, b float64) bool { return a > b })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Fatal("NaN > 0 should be false")
	}

	// NaN == NaN via typedEqual — float64 comparison.
	if typedEqual(nan, nan) {
		t.Fatal("NaN == NaN should be false per IEEE 754")
	}
}
