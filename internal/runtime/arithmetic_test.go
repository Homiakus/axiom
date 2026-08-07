package runtime

import (
	"strings"
	"testing"

	"github.com/Homiakus/axiom/internal/lang"
)

func arithmeticTestEnv() evalEnv {
	return evalEnv{execution: &Execution{Context: map[string]map[string]any{
		"State": {"value": 3},
	}}}
}

func TestArithmeticSlowAndFastRuntimeParity(t *testing.T) {
	tests := []struct {
		source string
		want   any
	}{
		{source: "2 + 3 * 4", want: 14},
		{source: "(2 + 3) * 4", want: 20},
		{source: "9 / 2", want: 4},
		{source: "9.0 / 2", want: 4.5},
		{source: "10 % 4", want: 2},
		{source: "5.5 % 2", want: 1.5},
		{source: "8 / 2 % 3", want: 1},
		{source: "-State.value * 2", want: -6},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			expr, err := lang.ParseExpr(tt.source)
			if err != nil {
				t.Fatal(err)
			}
			env := arithmeticTestEnv()

			slow, err := evalExpr(expr, env)
			if err != nil {
				t.Fatalf("slow evaluator: %v", err)
			}
			if !typedEqual(slow, tt.want) {
				t.Fatalf("slow result = %#v, want %#v", slow, tt.want)
			}

			program := (exprCompiler{}).compile(expr)
			fast, err := program.eval(env)
			if err != nil {
				t.Fatalf("fast evaluator: %v", err)
			}
			if !typedEqual(fast, tt.want) {
				t.Fatalf("fast result = %#v, want %#v", fast, tt.want)
			}
		})
	}
}

func TestArithmeticZeroDivisionErrorsMatch(t *testing.T) {
	for _, source := range []string{"5 / 0", "5 % 0"} {
		t.Run(source, func(t *testing.T) {
			expr, err := lang.ParseExpr(source)
			if err != nil {
				t.Fatal(err)
			}

			_, slowErr := evalExpr(expr, arithmeticTestEnv())
			if slowErr == nil || !strings.Contains(slowErr.Error(), "zero") {
				t.Fatalf("slow error = %v, want zero error", slowErr)
			}

			program := (exprCompiler{}).compile(expr)
			_, fastErr := program.eval(arithmeticTestEnv())
			if fastErr == nil || !strings.Contains(fastErr.Error(), "zero") {
				t.Fatalf("fast error = %v, want zero error", fastErr)
			}
		})
	}
}
