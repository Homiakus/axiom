package axiom_test

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
)

type snapshotState struct {
	Value int `json:"value"`
}

type snapshotSet struct {
	Value int `json:"value"`
}

func TestExecutionSnapshotIsReadOnlyAndIsolated(t *testing.T) {
	definition := model.New("SnapshotExport")
	state := model.Bind[snapshotState](definition, "State")
	setValue := model.EventOf[snapshotSet](definition)
	definition.Rule("set").
		On(setValue.Trigger()).
		Set(state.Int("Value"), setValue.Int("Value"))

	engine, err := axiom.Open(definition)
	if err != nil {
		t.Fatalf("open definition: %v", err)
	}

	run := engine.Execution("snapshot-1")
	ctx := context.Background()
	if err := run.Dispatch(ctx, snapshotSet{Value: 7}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	snapshot, err := run.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.ExecutionID != "snapshot-1" {
		t.Fatalf("execution id = %q, want snapshot-1", snapshot.ExecutionID)
	}
	if snapshot.Domain != "SnapshotExport" {
		t.Fatalf("domain = %q, want SnapshotExport", snapshot.Domain)
	}
	if len(snapshot.History) == 0 {
		t.Fatal("expected persisted history in snapshot")
	}
	if got := snapshot.Context["State"]["value"]; got != 7 {
		t.Fatalf("snapshot State.value = %#v, want 7", got)
	}

	// Mutating exported data must not mutate the durable execution.
	snapshot.Context["State"]["value"] = 99
	if snapshot.History[0].Payload == nil {
		snapshot.History[0].Payload = map[string]any{}
	}
	snapshot.History[0].Payload["mutated"] = true

	fresh, err := run.Snapshot(ctx)
	if err != nil {
		t.Fatalf("fresh snapshot: %v", err)
	}
	if got := fresh.Context["State"]["value"]; got != 7 {
		t.Fatalf("durable State.value changed through snapshot: %#v", got)
	}
	if _, exists := fresh.History[0].Payload["mutated"]; exists {
		t.Fatal("durable history payload changed through snapshot")
	}
}
