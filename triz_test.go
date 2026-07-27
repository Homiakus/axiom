package axiom_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Homiakus/axiom"
)

const trizSmokeSource = `system Switcher

state System:
  running: Bool = false

event Start(zone: Int)

profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent

function TurnOn(zone: Int) -> { ok: Bool }

rule ApplyTurnOn when:
  Start
do critical:
  result = TurnOn(event.zone)
then:
  set System.running = result.ok

always RunningIsBool:
  not (System.running == true and System.running == false)

view Dashboard:
  running = System.running
`

func TestNormalizeTRIZPublicAPI(t *testing.T) {
	result, err := axiom.NormalizeTRIZ([]byte(trizSmokeSource), axiom.WithSourceName("switcher.axm"))
	if err != nil {
		t.Fatalf("NormalizeTRIZ() error = %v", err)
	}
	if result.Module == nil {
		t.Fatalf("expected compiled module")
	}
	if result.Module.Domain != "Switcher" {
		t.Fatalf("domain = %q, want Switcher", result.Module.Domain)
	}
	if len(result.SourceMap) == 0 {
		t.Fatalf("expected source map")
	}
	if _, ok := result.Module.Activities["TurnOn"]; !ok {
		t.Fatalf("normalized activity TurnOn not found")
	}
}

func TestCompileAnyKeepsAxiomV0Compatibility(t *testing.T) {
	source := []byte(`domain Legacy

signal Started

context System:
  ready: Bool = false

rule start:
  on Started
  write:
    System.ready = true
`)
	module, err := axiom.CompileAny(source, axiom.WithSourceName("legacy.axm"))
	if err != nil {
		t.Fatalf("CompileAny(v0) error = %v", err)
	}
	if module.Domain != "Legacy" {
		t.Fatalf("domain = %q, want Legacy", module.Domain)
	}
	if _, ok := module.Rules["start"]; !ok {
		t.Fatalf("v0 rule not found after CompileAny")
	}
}

func TestHydroPilotMiniFixtureNormalizesAndCompiles(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "triz", "hydropilot_mini.axm")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	result, err := axiom.NormalizeTRIZ(source, axiom.WithSourceName(path))
	if err != nil {
		t.Fatalf("NormalizeTRIZ() error = %v", err)
	}
	if result.Module == nil {
		t.Fatalf("expected compiled module")
	}
	if result.Module.Domain != "HydroPilotMini" {
		t.Fatalf("domain = %q, want HydroPilotMini", result.Module.Domain)
	}
	if len(result.Module.Rules) < 3 {
		t.Fatalf("rules = %d, want at least 3", len(result.Module.Rules))
	}
}

func BenchmarkCompileAnyTRIZSmoke(b *testing.B) {
	source := []byte(trizSmokeSource)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := axiom.CompileAny(source, axiom.WithSourceName("switcher.axm")); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCompileAnyTRIZRuntimeSmoke(t *testing.T) {
	module, err := axiom.CompileAny([]byte(trizSmokeSource), axiom.WithSourceName("switcher.axm"))
	if err != nil {
		t.Fatalf("CompileAny() error = %v", err)
	}
	var gotZone any
	engine, err := axiom.New(module, axiom.Act("TurnOn", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
		gotZone = input["zone"]
		return axiom.Output{"ok": true}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := engine.Start(context.Background(), "exec-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(context.Background(), "exec-1", "Start", axiom.Input{"zone": 2}); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if err := engine.RunUntilIdle(context.Background(), "exec-1"); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
	if gotZone != 2 {
		t.Fatalf("activity zone input = %#v, want 2", gotZone)
	}
	state, err := engine.Query(context.Background(), "exec-1", "Dashboard")
	if err != nil {
		t.Fatalf("Dashboard query error = %v", err)
	}
	if state["running"] != true {
		t.Fatalf("running = %#v, want true", state["running"])
	}
	explain, err := engine.Query(context.Background(), "exec-1", "explain")
	if err != nil {
		t.Fatalf("explain query error = %v", err)
	}
	if explain["history"] == nil || explain["facts"] == nil {
		t.Fatalf("explain query missing machine-readable history/facts: %#v", explain)
	}
}
