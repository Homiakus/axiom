package axiom

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

type durableFlowState struct {
	Count int `json:"count"`
}

type durableFlowEvent struct {
	By int `json:"by"`
}

type durableFlowCommand struct {
	Value int `json:"value"`
}

type durableFlowTestStore struct {
	*incrementalTestStore
	mu                 sync.Mutex
	durability         StoreDurability
	failCompletionOnce bool
}

func newDurableFlowTestStore() *durableFlowTestStore {
	return &durableFlowTestStore{
		incrementalTestStore: &incrementalTestStore{},
		durability:           StoreDurabilitySynchronous,
	}
}

func (*durableFlowTestStore) AtomicFlowCommit() {}

func (s *durableFlowTestStore) Durability() StoreDurability {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.durability
}

func (s *durableFlowTestStore) SaveStateAndAppend(ctx context.Context, flow, id string, state []byte, entries []FlowHistoryEntry) error {
	s.mu.Lock()
	fail := false
	if s.failCompletionOnce {
		for _, entry := range entries {
			if entry.Type == flowHistoryEffectCompleted {
				s.failCompletionOnce = false
				fail = true
				break
			}
		}
	}
	s.mu.Unlock()
	if fail {
		return errors.New("synthetic completion commit failure")
	}
	return s.incrementalTestStore.SaveStateAndAppend(ctx, flow, id, state, entries)
}

func newDurableTestFlow(effect func(context.Context, durableFlowCommand) error) *Flow[durableFlowState] {
	flow := NewFlow("durable-test", durableFlowState{})
	Handle(flow, func(_ context.Context, state durableFlowState, event durableFlowEvent) (FlowResult[durableFlowState], error) {
		state.Count += event.By
		return Next(state, Call(durableFlowCommand{Value: state.Count})), nil
	})
	EffectHandler(flow, effect)
	return flow
}

func TestDurableFlowRejectsDefaultMemoryStore(t *testing.T) {
	flow := newDurableTestFlow(func(context.Context, durableFlowCommand) error { return nil })
	_, err := OpenFlow(flow, WithDurableFlowEffects())
	if err == nil {
		t.Fatal("OpenFlow() expected DurableFlowStore error")
	}
}

func TestDurableFlowRejectsNonSynchronousStore(t *testing.T) {
	store := newDurableFlowTestStore()
	store.durability = StoreDurabilityBuffered
	flow := newDurableTestFlow(func(context.Context, durableFlowCommand) error { return nil })
	_, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err == nil {
		t.Fatal("OpenFlow() expected synchronous durability error")
	}
}

func TestDurableFlowRejectsUnnamedEffectType(t *testing.T) {
	flow := NewFlow("unnamed-effect", durableFlowState{})
	EffectHandler(flow, func(context.Context, map[string]string) error { return nil })
	store := newDurableFlowTestStore()
	_, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err == nil {
		t.Fatal("OpenFlow() expected named effect type error")
	}
}

func TestDurableFlowCommitsIntentBeforeExternalEffect(t *testing.T) {
	store := newDurableFlowTestStore()
	var seenID string
	var sawCommittedState bool
	var sawPending bool
	flow := newDurableTestFlow(func(ctx context.Context, command durableFlowCommand) error {
		id, ok := FlowEffectIDFromContext(ctx)
		if !ok {
			return errors.New("missing durable effect id")
		}
		seenID = id
		stateBytes, _, found, err := store.LoadState(context.Background(), "durable-test", "exec-1")
		if err != nil || !found {
			return errors.New("state was not committed before effect")
		}
		var state durableFlowState
		if err := json.Unmarshal(stateBytes, &state); err != nil {
			return err
		}
		sawCommittedState = state.Count == command.Value
		history, err := store.LoadHistory(context.Background(), "durable-test", "exec-1")
		if err != nil {
			return err
		}
		sawPending = len(history) == 2 && history[0].Type == "EventHandled" && history[1].Type == flowHistoryEffectPending
		return nil
	})

	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Execution("exec-1").Dispatch(context.Background(), durableFlowEvent{By: 3}); err != nil {
		t.Fatal(err)
	}
	if seenID == "" || !sawCommittedState || !sawPending {
		t.Fatalf("effect observed id=%q committed=%v pending=%v", seenID, sawCommittedState, sawPending)
	}
	history, err := engine.Execution("exec-1").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[2].Type != flowHistoryEffectCompleted {
		t.Fatalf("history = %#v", history)
	}
}

func TestDurableFlowDeliveryFailureLeavesCommittedStateAndRecoversAfterRestart(t *testing.T) {
	store := newDurableFlowTestStore()
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
	err = engine.Execution("exec-2").Dispatch(context.Background(), durableFlowEvent{By: 5})
	var deliveryErr *FlowEffectDeliveryError
	if !errors.As(err, &deliveryErr) || !deliveryErr.StateCommitted() {
		t.Fatalf("Dispatch() error = %v, want committed FlowEffectDeliveryError", err)
	}
	state, err := engine.Execution("exec-2").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != 5 {
		t.Fatalf("state.Count = %d, want 5", state.Count)
	}
	history, err := engine.Execution("exec-2").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].Type != flowHistoryEffectPending {
		t.Fatalf("history after failed delivery = %#v", history)
	}

	fail = false
	recovered, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Execution("exec-2").DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("effect ids across restart = %#v", ids)
	}
	history, err = recovered.Execution("exec-2").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[2].Type != flowHistoryEffectCompleted {
		t.Fatalf("history after recovery = %#v", history)
	}
}

func TestDurableFlowAcknowledgeFailureRetriesWithSameEffectID(t *testing.T) {
	store := newDurableFlowTestStore()
	store.failCompletionOnce = true
	var ids []string
	calls := 0
	flow := newDurableTestFlow(func(ctx context.Context, _ durableFlowCommand) error {
		calls++
		id, ok := FlowEffectIDFromContext(ctx)
		if !ok {
			return errors.New("missing durable effect id")
		}
		ids = append(ids, id)
		return nil
	})

	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	err = engine.Execution("exec-3").Dispatch(context.Background(), durableFlowEvent{By: 1})
	var acknowledgeErr *FlowEffectAcknowledgeError
	if !errors.As(err, &acknowledgeErr) || !acknowledgeErr.StateCommitted() {
		t.Fatalf("Dispatch() error = %v, want committed FlowEffectAcknowledgeError", err)
	}
	if calls != 1 {
		t.Fatalf("effect calls after ack failure = %d, want 1", calls)
	}
	history, err := engine.Execution("exec-3").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history after ack failure = %#v", history)
	}

	recovered, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Execution("exec-3").DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("calls=%d ids=%#v", calls, ids)
	}
	history, err = recovered.Execution("exec-3").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[2].Type != flowHistoryEffectCompleted {
		t.Fatalf("history after ack retry = %#v", history)
	}
}
