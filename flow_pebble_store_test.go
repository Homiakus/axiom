package axiom

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	pebbledb "github.com/cockroachdb/pebble"
)

func TestPebbleFlowStoreDurableOutboxSurvivesReopen(t *testing.T) {
	path := t.TempDir()
	store, err := OpenPebbleFlowStore(path)
	if err != nil {
		t.Fatal(err)
	}

	fail := true
	var ids []string
	flow := newDurableTestFlow(func(ctx context.Context, _ durableFlowCommand) error {
		id, ok := FlowEffectIDFromContext(ctx)
		if !ok {
			return errors.New("missing durable effect id")
		}
		ids = append(ids, id)
		if fail {
			return errors.New("synthetic delivery failure")
		}
		return nil
	})
	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	err = engine.Execution("pebble-reopen").Dispatch(context.Background(), durableFlowEvent{By: 7})
	var deliveryErr *FlowEffectDeliveryError
	if !errors.As(err, &deliveryErr) {
		t.Fatalf("Dispatch() error = %v, want FlowEffectDeliveryError", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenPebbleFlowStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fail = false
	recovered, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	state, err := recovered.Execution("pebble-reopen").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 7 {
		t.Fatalf("reopened state.Count = %d, want 7", state.Count)
	}
	if err := recovered.Execution("pebble-reopen").DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("effect ids across Pebble reopen = %#v", ids)
	}
	history, err := recovered.Execution("pebble-reopen").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].Type != "EventHandled" || history[1].Type != flowHistoryEffectPending || history[2].Type != flowHistoryEffectCompleted {
		t.Fatalf("reopened history = %#v", history)
	}
}

func TestPebbleFlowStoreRejectsHistorySequenceGapWithoutPartialWrite(t *testing.T) {
	store, err := OpenPebbleFlowStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	err = store.SaveStateAndAppend(context.Background(), "flow", "gap", []byte(`{"count":1}`), []FlowHistoryEntry{{
		Sequence: 2,
		Type:     "EventHandled",
	}})
	if err == nil {
		t.Fatal("SaveStateAndAppend() expected sequence error")
	}
	_, _, found, err := store.LoadState(context.Background(), "flow", "gap")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("invalid append partially persisted state")
	}
}

func TestPebbleFlowStoreEncodingFailureDoesNotPartiallyCommitState(t *testing.T) {
	store, err := OpenPebbleFlowStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	initial := []byte(`{"count":1}`)
	if err := store.SaveStateAndAppend(context.Background(), "flow", "atomic", initial, nil); err != nil {
		t.Fatal(err)
	}
	badEntry := FlowHistoryEntry{Sequence: 1, Type: "EventHandled", Data: func() {}}
	if err := store.SaveStateAndAppend(context.Background(), "flow", "atomic", []byte(`{"count":2}`), []FlowHistoryEntry{badEntry}); err == nil {
		t.Fatal("SaveStateAndAppend() expected JSON encoding error")
	}
	state, length, found, err := store.LoadState(context.Background(), "flow", "atomic")
	if err != nil {
		t.Fatal(err)
	}
	if !found || length != 0 || string(state) != string(initial) {
		t.Fatalf("state after failed atomic append: found=%v length=%d state=%s", found, length, state)
	}
}

func TestPebbleFlowStoreLegacySaveReplacesTrailingHistory(t *testing.T) {
	store, err := OpenPebbleFlowStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	history := []FlowHistoryEntry{
		{Sequence: 1, Type: "EventHandled", Data: map[string]any{"value": 1}},
		{Sequence: 2, Type: "EventHandled", Data: map[string]any{"value": 2}},
	}
	if err := store.Save(context.Background(), "legacy", "replace", []byte(`{"count":2}`), history); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "legacy", "replace", []byte(`{"count":1}`), history[:1]); err != nil {
		t.Fatal(err)
	}
	state, gotHistory, found, err := store.Load(context.Background(), "legacy", "replace")
	if err != nil {
		t.Fatal(err)
	}
	if !found || string(state) != `{"count":1}` || len(gotHistory) != 1 || gotHistory[0].Sequence != 1 {
		t.Fatalf("Load() = found=%v state=%s history=%#v", found, state, gotHistory)
	}
}

func TestPebbleFlowStoreLegacySaveRepairsStaleTrailingHistory(t *testing.T) {
	store, err := OpenPebbleFlowStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	history := []FlowHistoryEntry{
		{Sequence: 1, Type: "EventHandled"},
		{Sequence: 2, Type: "EventHandled"},
	}
	if err := store.Save(context.Background(), "legacy", "repair", []byte(`{"count":2}`), history); err != nil {
		t.Fatal(err)
	}
	stale, err := json.Marshal(FlowHistoryEntry{Sequence: 3, Type: "stale"})
	if err != nil {
		t.Fatal(err)
	}
	batch := store.db.NewBatch()
	if err := batch.Set([]byte(flowHistoryKey("legacy", "repair", 3)), stale, pebbledb.NoSync); err != nil {
		batch.Close()
		t.Fatal(err)
	}
	if err := batch.Commit(pebbledb.Sync); err != nil {
		batch.Close()
		t.Fatal(err)
	}
	if err := batch.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(context.Background(), "legacy", "repair", []byte(`{"count":1}`), history[:1]); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadHistory(context.Background(), "legacy", "repair")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Sequence != 1 {
		t.Fatalf("history after repair = %#v", got)
	}
}

func TestPebbleFlowStorePreservesOutboxPayloadJSON(t *testing.T) {
	store, err := OpenPebbleFlowStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	intent := FlowEffectIntent{ID: "effect-1", Name: "cmd", Payload: json.RawMessage(`{"value":9}`)}
	entry := FlowHistoryEntry{Sequence: 1, Type: flowHistoryEffectPending, Name: "cmd", Data: intent}
	if err := store.SaveStateAndAppend(context.Background(), "payload", "1", []byte(`{}`), []FlowHistoryEntry{entry}); err != nil {
		t.Fatal(err)
	}
	history, err := store.LoadHistory(context.Background(), "payload", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history len = %d", len(history))
	}
	decoded, err := decodeFlowHistoryData[FlowEffectIntent](history[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != intent.ID || decoded.Name != intent.Name || string(decoded.Payload) != string(intent.Payload) {
		t.Fatalf("decoded intent = %#v", decoded)
	}
}

func TestPebbleFlowStoreHonorsCancelledContextBeforeWrite(t *testing.T) {
	store, err := OpenPebbleFlowStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.SaveStateAndAppend(ctx, "flow", "cancelled", []byte(`{}`), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("SaveStateAndAppend() error = %v, want context.Canceled", err)
	}
	_, _, found, err := store.LoadState(context.Background(), "flow", "cancelled")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("cancelled write persisted state")
	}
}
