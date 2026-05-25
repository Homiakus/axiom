package triz

import (
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeTRIZSurfaceToAxiomV0(t *testing.T) {
	source := []byte(`system HydroPilotMini

state System:
  running: Bool = false

state Zone[1..4]:
  enabled: Bool = true
  ph: Float?

event PHMeasurementDue(zone: Int)

condition CanMeasure:
  System.running
  Zone[event.zone].enabled

profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent
  audited

function MeasurePH(zone: Int) -> { value: Float, status: Text }

rule MeasurePH when:
  PHMeasurementDue
  CanMeasure
do critical:
  result = MeasurePH(event.zone)
then:
  set Zone[event.zone].ph = result.value

always NoImpossibleState:
  not (System.running == true and System.running == false)

view Dashboard:
  running = System.running
`)

	result, err := Normalize(source)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	normalized := string(result.Source)
	for _, want := range []string{
		"domain HydroPilotMini",
		"context Zone:",
		"signal PHMeasurementDue:",
		"fact CanMeasure when:",
		"policy critical:",
		"activity MeasurePHActivity:",
		"rule MeasurePH:",
		"run: MeasurePHActivity",
		"Zone.ph = output.value",
		"claim NoImpossibleState:",
		"query Dashboard:",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("normalized source missing %q:\n%s", want, normalized)
		}
	}
	if len(result.SourceMap) == 0 {
		t.Fatalf("expected source map entries")
	}
	if len(result.Diagnostics) == 0 {
		t.Fatalf("expected indexed-state diagnostic")
	}
}

func TestLooksLike(t *testing.T) {
	if !LooksLike([]byte("# comment\nsystem Demo\n")) {
		t.Fatalf("TRIZ source was not detected")
	}
	if LooksLike([]byte("domain Demo\n")) {
		t.Fatalf("Axiom v0 source detected as TRIZ")
	}
}

func TestNormalizeReportsTRIZDiagnostics(t *testing.T) {
	source := []byte(`system Unsafe

state System:
  ready: Bool = true

function DosePHDown() -> { ok: Bool }

rule DosePHDown when:
  MissingCondition
do:
  result = DosePHDown()
then:
  set System.ready = result.ok

always UnsupportedAggregate:
  all Zone.lightOn == false
`)
	result, err := Normalize(source)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	codes := map[string]bool{}
	for _, d := range result.Diagnostics {
		codes[d.Code] = true
	}
	for _, want := range []string{"AXT101", "AXT201", "AXT501"} {
		if !codes[want] {
			t.Fatalf("diagnostic %s missing from %#v", want, result.Diagnostics)
		}
	}
}

func TestNormalizeRuleWithoutDoAndProfileFlags(t *testing.T) {
	source := []byte(`system Manual

state Safety:
  estop: Bool = false
  alarm: Text = "none"

event EmergencyStopPressed(reason: Text)

profile local:
  timeout: 1s
  retry: 0
  once

rule EmergencyStop when:
  EmergencyStopPressed
then:
  set Safety.estop = true
  set Safety.alarm = event.reason
`)
	result, err := Normalize(source)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	normalized := string(result.Source)
	for _, want := range []string{
		"policy local:",
		"concurrency: once",
		"idempotency: none",
		"rule EmergencyStop:",
		"on EmergencyStopPressed",
		"Safety.estop = true",
		"Safety.alarm = signal.reason",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("normalized source missing %q:\n%s", want, normalized)
		}
	}
	if strings.Contains(normalized, "run:") {
		t.Fatalf("direct state rule unexpectedly has run field:\n%s", normalized)
	}
}

func TestNormalizeMultilineFunctionReturnObject(t *testing.T) {
	source := []byte(`system Solution

state Solution:
  status: Text = "unknown"
  needPHDown: Bool = false

event SolutionCheckDue(zone: Int)

profile local:
  timeout: 1s
  retry: 0
  once

function CheckSolution(ph: Float, tds: Float) -> {
  status: Text
  needPHDown: Bool
}

rule CheckSolution when:
  SolutionCheckDue
do local:
  result = CheckSolution(ph: 7.4, tds: 900.0)
then:
  set Solution.status = result.status
  set Solution.needPHDown = result.needPHDown
`)
	result, err := Normalize(source)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	normalized := string(result.Source)
	for _, want := range []string{
		"activity CheckSolutionActivity:",
		"ph = 7.4",
		"tds = 900.0",
		"status: String",
		"needPHDown: Bool",
		"effect: local",
		"policy: local",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("normalized source missing %q:\n%s", want, normalized)
		}
	}
}

func TestNormalizeMalformedInputReportsSyntaxDiagnostic(t *testing.T) {
	result, err := Normalize([]byte(`system Broken

state System:
  ready Bool = true
`))
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == "AXT001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected AXT001 syntax diagnostic, got %#v", result.Diagnostics)
	}
}

func BenchmarkNormalizeHydroPilotMini(b *testing.B) {
	source := []byte(`system HydroPilotMini

state System:
  running: Bool = false
  mode: Text = "manual"

state Safety:
  estop: Bool = false
  alarm: Text = "none"

state Zone[1..4]:
  enabled: Bool = true
  ph: Float?
  waterLevel: Text = "unknown"
  lightOn: Bool = false

event PHMeasurementDue(zone: Int)
event LightRequested(zone: Int, enabled: Bool, level: Int)

condition CanUseHardware:
  System.running
  Safety.estop == false

condition CanMeasure:
  CanUseHardware
  Zone[event.zone].enabled

profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent
  audited

function MeasurePH(zone: Int) -> { value: Float, status: Text }
function SetLight(zone: Int, enabled: Bool, level: Int) -> { ok: Bool }

rule MeasurePH when:
  PHMeasurementDue
  CanMeasure
do critical:
  result = MeasurePH(event.zone)
then:
  set Zone[event.zone].ph = result.value

rule ApplyLightRequest when:
  LightRequested
  CanUseHardware
do critical:
  result = SetLight(event.zone, event.enabled, event.level)
then:
  set Zone[event.zone].lightOn = result.ok and event.enabled
`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Normalize(source); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNormalizeSyntheticLargeModel(b *testing.B) {
	var src strings.Builder
	src.WriteString("system Synthetic\n\nstate System:\n  ready: Bool = true\n\n")
	src.WriteString("profile local:\n  timeout: 1s\n  retry: 0\n  once\n\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&src, "event Event%d(value: Int)\n", i)
	}
	src.WriteString("\nfunction Touch(value: Int) -> { ok: Bool }\n\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&src, "rule Rule%d when:\n  Event%d\ndo local:\n  result = Touch(event.value)\nthen:\n  set System.ready = result.ok\n\n", i, i)
	}
	source := []byte(src.String())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Normalize(source); err != nil {
			b.Fatal(err)
		}
	}
}
