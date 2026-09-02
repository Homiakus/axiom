package axiom_test

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom/axm"
)

func TestHydroPilotExampleCompilesExecutesAndSnapshots(t *testing.T) {
	plan, err := axm.Load("examples/inventor/hydropilot_cycle.axm")
	if err != nil {
		t.Fatalf("load HydroPilot AXM: %v", err)
	}
	if plan.Name != "HydroPilotCycle" {
		t.Fatalf("plan name = %q, want HydroPilotCycle", plan.Name)
	}

	engine, err := plan.New()
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	ctx := context.Background()
	run := engine.Execution("tank-1-cycle-1")
	if err := run.Signal(ctx, "StartCycle", map[string]any{"tankId": "tank-1"}); err != nil {
		t.Fatalf("start cycle: %v", err)
	}
	if err := run.Signal(ctx, "InitialMeasured", nil); err != nil {
		t.Fatalf("initial measured: %v", err)
	}
	if err := run.Signal(ctx, "DoseCalculated", nil); err != nil {
		t.Fatalf("dose calculated: %v", err)
	}
	if err := run.Signal(ctx, "Fault", map[string]any{"reason": "sensor-invalid"}); err != nil {
		t.Fatalf("fault: %v", err)
	}

	snapshot, err := run.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if got := snapshot.Context["Cycle"]["phase"]; got != "safe_stop" {
		t.Fatalf("phase = %#v, want safe_stop", got)
	}
	if got := snapshot.Context["Cycle"]["tankId"]; got != "tank-1" {
		t.Fatalf("tankId = %#v, want tank-1", got)
	}
	if got := snapshot.Context["Cycle"]["faultReason"]; got != "sensor-invalid" {
		t.Fatalf("faultReason = %#v, want sensor-invalid", got)
	}
	if len(snapshot.History) < 4 {
		t.Fatalf("history length = %d, expected persisted lifecycle history", len(snapshot.History))
	}
}
