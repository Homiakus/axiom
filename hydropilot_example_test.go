package axiom_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/axm"
)

type hydropilotProjectionEvent struct {
	Seq     int            `json:"seq"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}

type hydropilotExecutionProjection struct {
	Schema      string                       `json:"schema"`
	ExecutionID string                       `json:"executionId"`
	Domain      string                       `json:"domain"`
	Status      string                       `json:"status"`
	Context     map[string]map[string]any    `json:"context"`
	History     []hydropilotProjectionEvent  `json:"history"`
}

func projectHydroPilotExecution(snapshot *axiom.ExecutionSnapshot) hydropilotExecutionProjection {
	events := make([]hydropilotProjectionEvent, 0, len(snapshot.History))
	for _, entry := range snapshot.History {
		switch entry.Type {
		case "ExecutionStarted":
			events = append(events, hydropilotProjectionEvent{
				Seq:  len(events) + 1,
				Type: entry.Type,
			})
		case "SignalReceived":
			signal, _ := entry.Payload["signal"].(string)
			payload, _ := entry.Payload["payload"].(map[string]any)
			if payload == nil {
				payload = map[string]any{}
			}
			events = append(events, hydropilotProjectionEvent{
				Seq:  len(events) + 1,
				Type: entry.Type,
				Payload: map[string]any{
					"signal":  signal,
					"payload": payload,
				},
			})
		}
	}

	return hydropilotExecutionProjection{
		Schema:      "axiom.execution-projection/1",
		ExecutionID: snapshot.ExecutionID,
		Domain:      snapshot.Domain,
		Status:      string(snapshot.Status),
		Context:     snapshot.Context,
		History:     events,
	}
}

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

	fixtureBytes, err := os.ReadFile("examples/inventor/hydropilot_execution_projection.json")
	if err != nil {
		t.Fatalf("read execution projection fixture: %v", err)
	}
	var want any
	if err := json.Unmarshal(fixtureBytes, &want); err != nil {
		t.Fatalf("decode execution projection fixture: %v", err)
	}
	projectionBytes, err := json.Marshal(projectHydroPilotExecution(snapshot))
	if err != nil {
		t.Fatalf("marshal execution projection: %v", err)
	}
	var got any
	if err := json.Unmarshal(projectionBytes, &got); err != nil {
		t.Fatalf("decode generated execution projection: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("execution projection drifted\n got: %s\nwant: %s", projectionBytes, fixtureBytes)
	}
}
