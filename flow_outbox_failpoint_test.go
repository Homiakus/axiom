package axiom

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDurableFlowFailpointStagesDeterministicOrder(t *testing.T) {
	store := newDurableFlowTestStore()
	flow := newDurableTestFlow(func(context.Context, durableFlowCommand) error { return nil })
	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}

	var events []flowDurableFailpointEvent
	ctx := withDurableFlowFailpoint(context.Background(), func(event flowDurableFailpointEvent) error {
		events = append(events, event)
		return nil
	})
	if err := engine.Execution("failpoint-order").Dispatch(ctx, durableFlowEvent{By: 2}); err != nil {
		t.Fatal(err)
	}

	wantStages := []flowDurableFailpointStage{
		flowFailpointBeforeStateIntentCommit,
		flowFailpointAfterStateIntentCommit,
		flowFailpointBeforeEffectDelivery,
		flowFailpointAfterEffectDelivery,
		flowFailpointBeforeAcknowledgeCommit,
		flowFailpointAfterAcknowledgeCommit,
	}
	gotStages := make([]flowDurableFailpointStage, len(events))
	for i, event := range events {
		gotStages[i] = event.Stage
		if event.Flow != "durable-test" || event.ExecutionID != "failpoint-order" {
			t.Fatalf("event[%d] identity = %#v", i, event)
		}
	}
	if !reflect.DeepEqual(gotStages, wantStages) {
		t.Fatalf("failpoint stages = %#v, want %#v", gotStages, wantStages)
	}

	wantSequences := []int{1, 1, 2, 2, 3, 3}
	for i, want := range wantSequences {
		if events[i].HistorySequence != want {
			t.Fatalf("event[%d] history sequence = %d, want %d", i, events[i].HistorySequence, want)
		}
		if i < 2 {
			if events[i].EffectID != "" || events[i].EffectName != "" {
				t.Fatalf("event[%d] unexpectedly carries effect identity: %#v", i, events[i])
			}
			continue
		}
		if events[i].EffectID == "" || events[i].EffectName == "" {
			t.Fatalf("event[%d] missing effect identity: %#v", i, events[i])
		}
		if events[i].EffectID != events[2].EffectID || events[i].EffectName != events[2].EffectName {
			t.Fatalf("event[%d] effect identity drifted: %#v vs %#v", i, events[i], events[2])
		}
	}
}

func TestDurableFlowFailpointBoundaryState(t *testing.T) {
	crashErr := errors.New("synthetic durable crash")
	tests := []struct {
		name             string
		stage            flowDurableFailpointStage
		wantFound        bool
		wantHistory      int
		wantEffectCalls  int
		wantCompletedAck bool
	}{
		{name: "before state intent commit", stage: flowFailpointBeforeStateIntentCommit, wantFound: false, wantHistory: 0, wantEffectCalls: 0},
		{name: "after state intent commit", stage: flowFailpointAfterStateIntentCommit, wantFound: true, wantHistory: 2, wantEffectCalls: 0},
		{name: "before effect delivery", stage: flowFailpointBeforeEffectDelivery, wantFound: true, wantHistory: 2, wantEffectCalls: 0},
		{name: "after effect delivery", stage: flowFailpointAfterEffectDelivery, wantFound: true, wantHistory: 2, wantEffectCalls: 1},
		{name: "before acknowledge commit", stage: flowFailpointBeforeAcknowledgeCommit, wantFound: true, wantHistory: 2, wantEffectCalls: 1},
		{name: "after acknowledge commit", stage: flowFailpointAfterAcknowledgeCommit, wantFound: true, wantHistory: 3, wantEffectCalls: 1, wantCompletedAck: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newDurableFlowTestStore()
			effectCalls := 0
			flow := newDurableTestFlow(func(context.Context, durableFlowCommand) error {
				effectCalls++
				return nil
			})
			engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
			if err != nil {
				t.Fatal(err)
			}

			ctx := withDurableFlowFailpoint(context.Background(), func(event flowDurableFailpointEvent) error {
				if event.Stage == tt.stage {
					return crashErr
				}
				return nil
			})
			err = engine.Execution("failpoint-boundary").Dispatch(ctx, durableFlowEvent{By: 7})
			if !errors.Is(err, crashErr) {
				t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
			}

			stateBytes, historyLength, found, err := store.LoadState(context.Background(), "durable-test", "failpoint-boundary")
			if err != nil {
				t.Fatal(err)
			}
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if historyLength != tt.wantHistory {
				t.Fatalf("history length metadata = %d, want %d", historyLength, tt.wantHistory)
			}
			if tt.wantFound && len(stateBytes) == 0 {
				t.Fatal("committed state is empty")
			}
			if effectCalls != tt.wantEffectCalls {
				t.Fatalf("effect calls = %d, want %d", effectCalls, tt.wantEffectCalls)
			}

			history, err := store.LoadHistory(context.Background(), "durable-test", "failpoint-boundary")
			if err != nil {
				t.Fatal(err)
			}
			if len(history) != tt.wantHistory {
				t.Fatalf("history entries = %d, want %d", len(history), tt.wantHistory)
			}
			completed := false
			for _, entry := range history {
				if entry.Type == flowHistoryEffectCompleted {
					completed = true
				}
			}
			if completed != tt.wantCompletedAck {
				t.Fatalf("completed acknowledgement = %v, want %v", completed, tt.wantCompletedAck)
			}
		})
	}
}

func TestDurableFlowFailpointPebbleReopenAfterStateIntentCommit(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenPebbleFlowStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	var deliveredIDs []string
	flow := newDurableTestFlow(func(ctx context.Context, _ durableFlowCommand) error {
		id, ok := FlowEffectIDFromContext(ctx)
		if !ok {
			return errors.New("missing durable effect id")
		}
		deliveredIDs = append(deliveredIDs, id)
		return nil
	})
	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}

	crashErr := errors.New("crash after state and intent commit")
	ctx := withDurableFlowFailpoint(context.Background(), func(event flowDurableFailpointEvent) error {
		if event.Stage == flowFailpointAfterStateIntentCommit {
			return crashErr
		}
		return nil
	})
	err = engine.Execution("pebble-reopen").Dispatch(ctx, durableFlowEvent{By: 4})
	if !errors.Is(err, crashErr) {
		t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
	}
	if len(deliveredIDs) != 0 {
		t.Fatalf("effect delivered before simulated crash: %#v", deliveredIDs)
	}

	history, err := store.LoadHistory(context.Background(), "durable-test", "pebble-reopen")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].Type != flowHistoryEffectPending {
		t.Fatalf("history before reopen = %#v", history)
	}
	pending, err := decodeFlowHistoryData[FlowEffectIntent](history[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := OpenPebbleFlowStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopenedStore.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()
	recovered, err := OpenFlow(flow, WithFlowStore(reopenedStore), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Execution("pebble-reopen").DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deliveredIDs) != 1 || deliveredIDs[0] != pending.ID {
		t.Fatalf("delivered IDs after reopen = %#v, want [%q]", deliveredIDs, pending.ID)
	}

	state, err := recovered.Execution("pebble-reopen").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 4 {
		t.Fatalf("state.Count = %d, want 4", state.Count)
	}
	history, err = recovered.Execution("pebble-reopen").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[2].Type != flowHistoryEffectCompleted {
		t.Fatalf("history after reopen = %#v", history)
	}
}
