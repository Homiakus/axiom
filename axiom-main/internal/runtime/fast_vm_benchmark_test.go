package runtime

import (
	"fmt"
	"strings"
	"testing"

	"axiom/internal/compiler"
)

func BenchmarkFastDirtyDispatch10(b *testing.B)   { benchmarkFastDirtyDispatch(b, 10) }
func BenchmarkFastDirtyDispatch100(b *testing.B)  { benchmarkFastDirtyDispatch(b, 100) }
func BenchmarkFastDirtyDispatch1000(b *testing.B) { benchmarkFastDirtyDispatch(b, 1000) }
func BenchmarkFastDirtyDispatch100000(b *testing.B) {
	benchmarkFastDirtyDispatch(b, 100000)
}
func BenchmarkFastDirtyDispatch100000Across100ParamsChange1(b *testing.B) {
	benchmarkFastDirtyDispatchAcrossParams(b, 100000, 100, 20, 1)
}
func BenchmarkFastDirtyDispatch100000Across100ParamsChange20(b *testing.B) {
	benchmarkFastDirtyDispatchAcrossParams(b, 100000, 100, 20, 20)
}

func benchmarkFastDirtyDispatch(b *testing.B, rules int) {
	source := buildFastBenchmarkModule(rules)
	module, err := compiler.Compile([]byte(source))
	if err != nil {
		b.Fatalf("Compile() error = %v", err)
	}
	plan := compileFastPlan(module, false)
	execution := &Execution{
		ID:       "bench-1",
		Domain:   "Bench",
		Context:  map[string]map[string]any{"Input": {"value": 0}},
		Computed: map[string]any{},
		Facts:    map[string]FactValue{},
	}
	if _, err := plan.recompute(execution, nil); err != nil {
		b.Fatalf("initial recompute() error = %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		execution.Context["Input"]["value"] = i
		changedAtoms, err := plan.recompute(execution, map[string]struct{}{"Input.value": {}})
		if err != nil {
			b.Fatalf("recompute() error = %v", err)
		}
		_ = plan.rulesForChanged([]string{"Input.value"}, changedAtoms)
	}
}

func buildFastBenchmarkModule(rules int) string {
	var b strings.Builder
	b.WriteString(`domain Bench

context Input:
  value: Int = 0

computed ready: Bool =
  Input.value >= 0

fact Ready when:
  ready

`)
	for i := 0; i < rules; i++ {
		fmt.Fprintf(&b, `rule rule%d:
  on changed(Input.value)
  when:
    Input.value >= 0
  require:
    Ready
  write:
    Input.value = Input.value

`, i)
	}
	return b.String()
}

func benchmarkFastDirtyDispatchAcrossParams(b *testing.B, rules int, params int, triggersPerRule int, changedParams int) {
	source := buildFastBenchmarkModuleAcrossParams(rules, params, triggersPerRule)
	module, err := compiler.Compile([]byte(source))
	if err != nil {
		b.Fatalf("Compile() error = %v", err)
	}
	plan := compileFastPlan(module, false)
	fields := make(map[string]any, params)
	for i := 0; i < params; i++ {
		fields[fmt.Sprintf("p%d", i)] = 0
	}
	execution := &Execution{
		ID:       "bench-real-1",
		Domain:   "BenchReal",
		Context:  map[string]map[string]any{"Input": fields},
		Computed: map[string]any{},
		Facts:    map[string]FactValue{},
	}
	if _, err := plan.recompute(execution, nil); err != nil {
		b.Fatalf("initial recompute() error = %v", err)
	}
	changed := make([]string, changedParams)
	changedSet := make(map[string]struct{}, changedParams)
	for i := 0; i < changedParams; i++ {
		changed[i] = fmt.Sprintf("Input.p%d", i)
		changedSet[changed[i]] = struct{}{}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < changedParams; j++ {
			execution.Context["Input"][fmt.Sprintf("p%d", j)] = i
		}
		changedAtoms, err := plan.recompute(execution, changedSet)
		if err != nil {
			b.Fatalf("recompute() error = %v", err)
		}
		_ = plan.rulesForChanged(changed, changedAtoms)
	}
}

func buildFastBenchmarkModuleAcrossParams(rules int, params int, triggersPerRule int) string {
	var b strings.Builder
	b.WriteString("domain BenchReal\n\ncontext Input:\n")
	for i := 0; i < params; i++ {
		fmt.Fprintf(&b, "  p%d: Int = 0\n", i)
	}
	b.WriteString("\ncomputed ready: Bool =\n  Input.p0 >= 0\n\nfact Ready when:\n  ready\n\n")
	for i := 0; i < rules; i++ {
		fmt.Fprintf(&b, "rule rule%d:\n  on:\n", i)
		for j := 0; j < triggersPerRule; j++ {
			param := (i + j) % params
			fmt.Fprintf(&b, "    changed(Input.p%d)\n", param)
		}
		param := i % params
		fmt.Fprintf(&b, `  when:
    Input.p%d >= 0
  require:
    Ready
  write:
    Input.p%d = Input.p%d

`, param, param, param)
	}
	return b.String()
}
