package runtime

import (
	"math"
	"strings"
	"testing"

	"github.com/Homiakus/axiom/internal/lang"
)

func TestLargeSignedIntegersStayExactAcrossSlowAndFastEvaluation(t *testing.T) {
	const large int64 = 9007199254740993 // 2^53 + 1; not exactly representable as float64.
	env := evalEnv{execution: &Execution{Context: map[string]map[string]any{
		"State": {"value": large},
	}}}

	for _, tt := range []struct {
		source string
		want   any
	}{
		{source: "State.value == 9007199254740993", want: true},
		{source: "State.value > 9007199254740992", want: true},
		{source: "State.value + 1", want: int64(9007199254740994)},
		{source: "State.value - 2", want: int64(9007199254740991)},
	} {
		t.Run(tt.source, func(t *testing.T) {
			expr, err := lang.ParseExpr(tt.source)
			if err != nil {
				t.Fatal(err)
			}
			slow, err := evalExpr(expr, env)
			if err != nil {
				t.Fatalf("slow evaluator: %v", err)
			}
			if !typedEqual(slow, tt.want) {
				t.Fatalf("slow result = %#v, want %#v", slow, tt.want)
			}

			fast, err := (exprCompiler{}).compile(expr).eval(env)
			if err != nil {
				t.Fatalf("fast evaluator: %v", err)
			}
			if !typedEqual(fast, tt.want) {
				t.Fatalf("fast result = %#v, want %#v", fast, tt.want)
			}
		})
	}
}

func TestSignedIntegerOverflowReturnsExplicitErrors(t *testing.T) {
	tests := []struct {
		name string
		fn   func() (any, error)
	}{
		{name: "add", fn: func() (any, error) { return addValues(int64(math.MaxInt64), int64(1)) }},
		{name: "subtract", fn: func() (any, error) { return subtractValues(int64(math.MinInt64), int64(1)) }},
		{name: "multiply", fn: func() (any, error) { return multiplyValues(int64(math.MaxInt64), int64(2)) }},
		{name: "divide", fn: func() (any, error) { return divideValues(int64(math.MinInt64), int64(-1)) }},
		{name: "negate", fn: func() (any, error) { return negateValue(int64(math.MinInt64)) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.fn()
			if err == nil || !strings.Contains(err.Error(), "overflow") {
				t.Fatalf("error = %v, want explicit overflow error", err)
			}
		})
	}
}
