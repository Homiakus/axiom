package runtime

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"axiom/internal/compiler"
	"axiom/internal/diag"
	"axiom/internal/lang"
)

func TestFastPlanDirtyPropagationOnlyTouchesDependentAtoms(t *testing.T) {
	module, err := compiler.Compile([]byte(`
domain Fast

context A:
  value: Int = 0

context B:
  value: Int = 0

computed aReady: Bool =
  A.value > 0

computed bReady: Bool =
  B.value > 0

fact AReady when:
  aReady

fact BReady when:
  bReady
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	plan := compileFastPlan(module, false)
	execution := &Execution{
		ID:       "fast-1",
		Domain:   "Fast",
		Context:  map[string]map[string]any{"A": {"value": 0}, "B": {"value": 0}},
		Computed: map[string]any{},
		Facts:    map[string]FactValue{},
	}
	if _, err := plan.recompute(execution, nil); err != nil {
		t.Fatalf("initial recompute() error = %v", err)
	}
	execution.Context["A"]["value"] = 1
	changed, err := plan.recompute(execution, map[string]struct{}{"A.value": {}})
	if err != nil {
		t.Fatalf("dirty recompute() error = %v", err)
	}
	if !containsString(changed, "aReady") || !containsString(changed, "AReady") {
		t.Fatalf("changed atoms = %#v, want aReady and AReady", changed)
	}
	if containsString(changed, "bReady") || containsString(changed, "BReady") {
		t.Fatalf("changed atoms = %#v, unrelated B atoms should not change", changed)
	}
}

func TestFastStateLivesOnExecutionClone(t *testing.T) {
	module, err := compiler.Compile([]byte(`
domain FastState

context User:
  id: String?

computed ready: Bool =
  User.id exists

fact Ready when:
  ready
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	plan := compileFastPlan(module, false)
	execution := &Execution{
		ID:       "fast-state-1",
		Domain:   "FastState",
		Context:  map[string]map[string]any{"User": {"id": "u1"}},
		Computed: map[string]any{},
		Facts:    map[string]FactValue{},
	}
	if _, err := plan.recompute(execution, nil); err != nil {
		t.Fatalf("recompute() error = %v", err)
	}
	readyID, ok := plan.atomID("Ready")
	if !ok {
		t.Fatalf("Ready atom not found")
	}
	if !bitset(execution.RuntimeState.ActiveAtoms).has(readyID) {
		t.Fatalf("execution active atoms = %#v, Ready should be active", execution.RuntimeState.ActiveAtoms)
	}
	cloned := cloneExecution(execution)
	if !bitset(cloned.RuntimeState.ActiveAtoms).has(readyID) {
		t.Fatalf("cloned active atoms = %#v, Ready should survive clone", cloned.RuntimeState.ActiveAtoms)
	}
	recompiled := compileFastPlan(module, false)
	if !recompiled.state(cloned).has(readyID) {
		t.Fatalf("recompiled plan did not read atom state from execution")
	}
}

func TestBitsetForEachDoesNotAllocate(t *testing.T) {
	b := newBitset(512)
	for i := 0; i < 512; i += 3 {
		b.set(i)
	}
	var sum int
	allocs := testing.AllocsPerRun(1000, func() {
		b.forEach(func(id int) {
			sum += id
		})
	})
	if allocs != 0 {
		t.Fatalf("ForEach allocations = %v, want 0; sum=%d", allocs, sum)
	}
}

func TestStrictFastPlanRejectsDNFExplosion(t *testing.T) {
	var b strings.Builder
	b.WriteString("domain DNF\n\ncontext Input:\n  value: Int = 0\n\nclaim tooWide:\n  always:\n    ")
	for i := 0; i < MaxFastClauses+1; i++ {
		if i > 0 {
			b.WriteString(" or ")
		}
		fmt.Fprintf(&b, "Input.value == %d", i)
	}
	b.WriteString("\n")
	module, err := compiler.Compile([]byte(b.String()))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	plan := compileFastPlan(module, true)
	err = plan.strictError()
	if err == nil {
		t.Fatalf("strictError() expected DNF diagnostic")
	}
	var diagnostics diag.Errors
	if !errors.As(err, &diagnostics) {
		t.Fatalf("strictError() type = %T, want diag.Errors", err)
	}
	if diagnostics[len(diagnostics)-1].Code != "AX702" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestExpressionVMMatchesEvaluator(t *testing.T) {
	fieldIDs := map[string]int{"User.id": 0, "User.age": 1}
	compiler := exprCompiler{fieldIDs: fieldIDs, atomIDs: map[string]int{}}
	execution := &Execution{
		Context: map[string]map[string]any{"User": {"id": "u1", "age": 42}},
	}
	dirty := Dirty{Fields: newBitset(2)}
	dirty.Fields.set(1)
	env := evalEnv{execution: execution, changed: map[string]struct{}{"User.age": {}}, dirty: dirty, fieldIDs: fieldIDs}
	cases := []string{
		`User.id == "u1"`,
		`User.age >= 40`,
		`User.age + 1 == 43`,
		`User.id in ["u0", "u1"]`,
		`User.id exists and changed(User.age)`,
		`hash(User.id, User.age) == hash(User.id, User.age)`,
	}
	for _, source := range cases {
		expr, err := lang.ParseExpr(source)
		if err != nil {
			t.Fatalf("ParseExpr(%q) error = %v", source, err)
		}
		want, err := evalExpr(expr, env)
		if err != nil {
			t.Fatalf("evalExpr(%q) error = %v", source, err)
		}
		got, err := compiler.compile(expr).eval(env)
		if err != nil {
			t.Fatalf("vm(%q) error = %v", source, err)
		}
		if !typedEqual(got, want) {
			t.Fatalf("vm(%q) = %#v, want %#v", source, got, want)
		}
	}
}

func TestStrictFastPlanRejectsDynamicRefComparison(t *testing.T) {
	module, err := compiler.Compile([]byte(`
domain Strict

context User:
  id: String?
  email: String?

claim same:
  always:
    User.id == User.email
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	plan := compileFastPlan(module, true)
	if err := plan.strictError(); err == nil {
		t.Fatalf("strictError() expected unsupported expression")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
