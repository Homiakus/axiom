package runtime

import (
	"testing"

	"github.com/Homiakus/axiom/internal/compiler"
	"github.com/Homiakus/axiom/internal/lang"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestExprVMComprehensive tests the fast instruction-based expression VM
// (expr_vm.go) directly to verify opcodes, evaluation accuracy, and fallbacks.
// ──────────────────────────────────────────────────────────────────────────────

func TestExprVMCompileAndEval(t *testing.T) {
	// Compile a small module to get a valid IDs table.
	module, err := compiler.Compile([]byte(`domain VMTest

context User:
  age: Int = 20
  active: Bool = true
  score: Float = 95.5
  name: String = "Alice"
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	cases := []struct {
		name string
		src  string
		want any
	}{
		{name: "literal bool true", src: "true", want: true},
		{name: "literal bool false", src: "false", want: false},
		{name: "literal int", src: "42", want: 42},
		{name: "literal string", src: `"hello"`, want: "hello"},
		{name: "field ref int", src: "User.age", want: 20},
		{name: "field ref bool", src: "User.active", want: true},
		{name: "binary eq true", src: "User.age == 20", want: true},
		{name: "binary eq false", src: "User.age == 30", want: false},
		{name: "binary gt true", src: "User.age > 18", want: true},
		{name: "binary gte true", src: "User.age >= 20", want: true},
		{name: "binary lt true", src: "User.age < 100", want: true},
		{name: "binary lte true", src: "User.age <= 20", want: true},
		{name: "binary and true", src: "User.active and User.age > 18", want: true},
		{name: "binary or true", src: "User.active or User.age == 0", want: true},
		{name: "unary not", src: "not User.active", want: false},
		{name: "unary exists true", src: "User.age exists", want: true},
		{name: "in list true", src: `User.name in ["Alice", "Bob"]`, want: true},
		{name: "in list false", src: `User.name in ["Charlie"]`, want: false},
		{name: "addition", src: "User.age + 5", want: 25},
		{name: "subtraction", src: "User.age - 5", want: 15},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			astExpr, err := lang.ParseExpr(tc.src)
			if err != nil {
				t.Fatalf("ParseExpr(%q) error = %v", tc.src, err)
			}

			// Compile AST to exprProgram instructions.
			prog := compileTestExprProgram(astExpr, module.IDs.FieldIDs)

			// Set up execution state matching the module defaults.
			exec := &Execution{
				Context: map[string]map[string]any{
					"User": {
						"age":    20,
						"active": true,
						"score":  95.5,
						"name":   "Alice",
					},
				},
			}
			ensureExecutionState(exec, len(module.IDs.Fields), 10)
			syncExecutionValuesFromContext(exec, module)

			// Evaluate via VM.
			got, err := prog.eval(evalEnv{execution: exec})
			if err != nil {
				t.Fatalf("prog.eval(%q) error = %v", tc.src, err)
			}
			if !typedEqual(got, tc.want) {
				t.Fatalf("prog.eval(%q) = %#v (%T), want %#v (%T)", tc.src, got, got, tc.want, tc.want)
			}
		})
	}
}

// TestExprVMHasInstructions verifies that compiled exprProgram contains instructions.
func TestExprVMHasInstructions(t *testing.T) {
	module, err := compiler.Compile([]byte(`domain PureTest

context S:
  v: Int = 0
`))
	if err != nil {
		t.Fatal(err)
	}

	pureExpr, _ := lang.ParseExpr("S.v > 0")
	progPure := compileTestExprProgram(pureExpr, module.IDs.FieldIDs)
	if len(progPure.instrs) == 0 {
		t.Fatal("S.v > 0 should compile to non-empty instructions")
	}
}

func compileTestExprProgram(expr *lang.Expr, fieldIDs map[string]compiler.FieldID) *exprProgram {
	m := make(map[string]int, len(fieldIDs))
	for k, v := range fieldIDs {
		m[k] = int(v)
	}
	return exprCompiler{fieldIDs: m}.compile(expr)
}

// syncExecutionValuesFromContext populates exec.RuntimeState.Values from
// exec.Context for testing expr_vm directly.
func syncExecutionValuesFromContext(exec *Execution, module *compiler.Module) {
	for name, fieldID := range module.IDs.FieldIDs {
		parts := splitFieldName(name)
		if len(parts) == 2 {
			if ctxMap, ok := exec.Context[parts[0]]; ok {
				if val, ok := ctxMap[parts[1]]; ok {
					exec.RuntimeState.Values[uint32(fieldID)] = valueOf(val)
					bitset(exec.RuntimeState.Present).set(int(fieldID))
				}
			}
		}
	}
}

func splitFieldName(name string) []string {
	idx := -1
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{name}
	}
	return []string{name[:idx], name[idx+1:]}
}
