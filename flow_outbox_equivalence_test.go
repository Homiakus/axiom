package axiom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

type t032State struct {
	Total               int `json:"total"`
	ReducerApplications int `json:"reducerApplications"`
}

type t032Event struct {
	By      int `json:"by"`
	Effects int `json:"effects"`
}

type t032Command struct {
	Event int `json:"event"`
	Slot  int `json:"slot"`
	Value int `json:"value"`
}

type t032ReducerCounter struct {
	mu    sync.Mutex
	count int
}

func (c *t032ReducerCounter) increment() {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
}

func (c *t032ReducerCounter) value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

type t032Sink struct {
	mu           sync.Mutex
	attempts     []string
	applications map[string]t032Command
}

func newT032Sink() *t032Sink {
	return &t032Sink{applications: make(map[string]t032Command)}
}

func (s *t032Sink) handle(ctx context.Context, command t032Command) error {
	id, ok := FlowEffectIDFromContext(ctx)
	if !ok {
		return errors.New("missing durable effect id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, id)
	if _, exists := s.applications[id]; !exists {
		s.applications[id] = command
	}
	return nil
}

func (s *t032Sink) snapshot() ([]string, map[string]t032Command) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempts := append([]string(nil), s.attempts...)
	applications := make(map[string]t032Command, len(s.applications))
	for id, command := range s.applications {
		applications[id] = command
	}
	return attempts, applications
}

func newT032Flow(sink *t032Sink, reducers *t032ReducerCounter) *Flow[t032State] {
	flow := NewFlow("t032-equivalence", t032State{})
	Handle(flow, func(_ context.Context, state t032State, event t032Event) (FlowResult[t032State], error) {
		reducers.increment()
		state.Total += event.By
		state.ReducerApplications++
		effects := make([]Effect, 0, event.Effects)
		for slot := 0; slot < event.Effects; slot++ {
			effects = append(effects, Call(t032Command{
				Event: state.ReducerApplications,
				Slot:  slot,
				Value: state.Total,
			}))
		}
		return FlowResult[t032State]{State: state, Effects: effects}, nil
	})
	EffectHandler(flow, sink.handle)
	return flow
}

func TestDurableFlowStoreCapabilityClassification(t *testing.T) {
	memory := NewMemoryFlowStore()
	if got := memory.Durability(); got != StoreDurabilityEphemeral {
		t.Fatalf("MemoryFlowStore durability = %q, want %q", got, StoreDurabilityEphemeral)
	}
	if _, ok := any(memory).(DurableFlowStore); ok {
		t.Fatal("MemoryFlowStore unexpectedly implements DurableFlowStore")
	}

	pebble := t032OpenPebble(t, t.TempDir())
	defer t032ClosePebble(t, pebble)
	if got := pebble.Durability(); got != StoreDurabilitySynchronous {
		t.Fatalf("PebbleFlowStore durability = %q, want %q", got, StoreDurabilitySynchronous)
	}
	if _, ok := any(pebble).(DurableFlowStore); !ok {
		t.Fatal("PebbleFlowStore does not implement DurableFlowStore")
	}
}

func TestDurableFlowOutboxModelCompletionIsMonotonic(t *testing.T) {
	store := newDurableFlowTestStore()
	sink := newT032Sink()
	reducers := &t032ReducerCounter{}
	flow := newT032Flow(sink, reducers)
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("model-monotonic")

	crashErr := errors.New("synthetic interruption after effect delivery")
	ctx := t032FailOnOccurrence(context.Background(), flowFailpointAfterEffectDelivery, 1, crashErr)
	err := execution.Dispatch(ctx, t032Event{By: 11, Effects: 1})
	if !errors.Is(err, crashErr) {
		t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
	}
	if reducers.value() != 1 {
		t.Fatalf("reducer invocations after committed interruption = %d, want 1", reducers.value())
	}

	if err := execution.DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	stateBefore, historyBefore := t032StateAndHistory(t, execution)
	attemptsBefore, applicationsBefore := sink.snapshot()
	if len(attemptsBefore) != 2 || len(applicationsBefore) != 1 {
		t.Fatalf("after recovery attempts=%v applications=%v, want 2 attempts and 1 application", attemptsBefore, applicationsBefore)
	}

	for i := 0; i < 5; i++ {
		if err := execution.DrainEffects(context.Background()); err != nil {
			t.Fatalf("DrainEffects() repetition %d: %v", i, err)
		}
	}
	stateAfter, historyAfter := t032StateAndHistory(t, execution)
	attemptsAfter, applicationsAfter := sink.snapshot()
	if !reflect.DeepEqual(stateAfter, stateBefore) {
		t.Fatalf("state changed after completed drains: got %#v want %#v", stateAfter, stateBefore)
	}
	if !reflect.DeepEqual(t032NormalizeHistory(t, historyAfter), t032NormalizeHistory(t, historyBefore)) {
		t.Fatalf("history changed after completed drains: got %#v want %#v", historyAfter, historyBefore)
	}
	if !reflect.DeepEqual(attemptsAfter, attemptsBefore) {
		t.Fatalf("completed effect resurrected: attempts got %v want %v", attemptsAfter, attemptsBefore)
	}
	if !reflect.DeepEqual(applicationsAfter, applicationsBefore) {
		t.Fatalf("business applications changed after completed drains: got %v want %v", applicationsAfter, applicationsBefore)
	}
	if reducers.value() != 1 {
		t.Fatalf("reducer re-applied during recovery/drain: invocations=%d want 1", reducers.value())
	}
}

type t032CrashScenario struct {
	stage      flowDurableFailpointStage
	occurrence int
}

func TestDurableFlowPebbleCrashEquivalenceProperties(t *testing.T) {
	events := []t032Event{
		{By: 2, Effects: 1},
		{By: 3, Effects: 2},
		{By: 5, Effects: 0},
		{By: 7, Effects: 3},
	}
	reference := t032RunUninterrupted(t, events)

	scenarios := []t032CrashScenario{
		{stage: flowFailpointBeforeStateIntentCommit, occurrence: 1},
		{stage: flowFailpointAfterStateIntentCommit, occurrence: 1},
		{stage: flowFailpointBeforeEffectDelivery, occurrence: 1},
		{stage: flowFailpointBeforeEffectDelivery, occurrence: 2},
		{stage: flowFailpointAfterEffectDelivery, occurrence: 1},
		{stage: flowFailpointAfterEffectDelivery, occurrence: 2},
		{stage: flowFailpointBeforeAcknowledgeCommit, occurrence: 1},
		{stage: flowFailpointBeforeAcknowledgeCommit, occurrence: 2},
		{stage: flowFailpointAfterAcknowledgeCommit, occurrence: 1},
		{stage: flowFailpointAfterAcknowledgeCommit, occurrence: 2},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(fmt.Sprintf("%s-occurrence-%d", scenario.stage, scenario.occurrence), func(t *testing.T) {
			outcome := t032RunInterrupted(t, events, scenario)
			t032AssertEquivalentOutcome(t, reference, outcome, scenario)
		})
	}
}

type t032HistoryRecord struct {
	Sequence int
	Type     string
	Name     string
	EffectID string
	Payload  string
}

type t032Outcome struct {
	state          t032State
	history        []t032HistoryRecord
	attempts       []string
	applications   map[string]t032Command
	reducerCalls   int
	postCommitOnly bool
}

func t032RunUninterrupted(t *testing.T, events []t032Event) t032Outcome {
	t.Helper()
	dir := t.TempDir()
	sink := newT032Sink()
	reducers := &t032ReducerCounter{}
	flow := newT032Flow(sink, reducers)
	store := t032OpenPebble(t, dir)
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("equivalence-exec")
	for index, event := range events {
		if err := execution.Dispatch(context.Background(), event); err != nil {
			t.Fatalf("reference Dispatch(%d): %v", index, err)
		}
	}
	if err := execution.DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, history := t032StateAndHistory(t, execution)
	attempts, applications := sink.snapshot()
	t032ClosePebble(t, store)
	return t032Outcome{
		state:        state,
		history:      t032NormalizeHistory(t, history),
		attempts:     attempts,
		applications: applications,
		reducerCalls: reducers.value(),
	}
}

func t032RunInterrupted(t *testing.T, events []t032Event, scenario t032CrashScenario) t032Outcome {
	t.Helper()
	if len(events) < 3 {
		t.Fatal("T-032 interruption trace requires at least three events")
	}
	dir := t.TempDir()
	sink := newT032Sink()
	reducers := &t032ReducerCounter{}
	flow := newT032Flow(sink, reducers)
	store := t032OpenPebble(t, dir)
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("equivalence-exec")

	if err := execution.Dispatch(context.Background(), events[0]); err != nil {
		t.Fatalf("initial Dispatch(): %v", err)
	}
	crashErr := fmt.Errorf("synthetic crash at %s occurrence %d", scenario.stage, scenario.occurrence)
	ctx := t032FailOnOccurrence(context.Background(), scenario.stage, scenario.occurrence, crashErr)
	err := execution.Dispatch(ctx, events[1])
	if !errors.Is(err, crashErr) {
		t.Fatalf("interrupted Dispatch() error = %v, want %v", err, crashErr)
	}
	t032ClosePebble(t, store)

	store = t032OpenPebble(t, dir)
	engine = t032OpenEngine(t, flow, store)
	execution = engine.Execution("equivalence-exec")
	preCommit := scenario.stage == flowFailpointBeforeStateIntentCommit
	if preCommit {
		if err := execution.Dispatch(context.Background(), events[1]); err != nil {
			t.Fatalf("re-dispatch after pre-commit crash: %v", err)
		}
	} else {
		if err := execution.DrainEffects(context.Background()); err != nil {
			t.Fatalf("DrainEffects() after persisted crash: %v", err)
		}
	}

	for index := 2; index < len(events); index++ {
		if err := execution.Dispatch(context.Background(), events[index]); err != nil {
			t.Fatalf("post-recovery Dispatch(%d): %v", index, err)
		}
	}
	if err := execution.DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}

	state, history := t032StateAndHistory(t, execution)
	normalized := t032NormalizeHistory(t, history)
	attemptsBefore, applicationsBefore := sink.snapshot()
	reducerCallsBefore := reducers.value()
	t032ClosePebble(t, store)

	// Completion is monotonic across arbitrarily repeated reopen/drain cycles:
	// once the final trace has no pending effects, recovery must remain a no-op.
	for cycle := 0; cycle < 3; cycle++ {
		store = t032OpenPebble(t, dir)
		engine = t032OpenEngine(t, flow, store)
		execution = engine.Execution("equivalence-exec")
		if err := execution.DrainEffects(context.Background()); err != nil {
			t.Fatalf("post-completion DrainEffects() cycle %d: %v", cycle, err)
		}
		cycleState, cycleHistory := t032StateAndHistory(t, execution)
		if !reflect.DeepEqual(cycleState, state) {
			t.Fatalf("state resurrected/changed on cycle %d: got %#v want %#v", cycle, cycleState, state)
		}
		if !reflect.DeepEqual(t032NormalizeHistory(t, cycleHistory), normalized) {
			t.Fatalf("history resurrected/changed on cycle %d", cycle)
		}
		t032ClosePebble(t, store)
	}
	attemptsAfter, applicationsAfter := sink.snapshot()
	if !reflect.DeepEqual(attemptsAfter, attemptsBefore) {
		t.Fatalf("completed effect redelivered after reopen cycles: before=%v after=%v", attemptsBefore, attemptsAfter)
	}
	if !reflect.DeepEqual(applicationsAfter, applicationsBefore) {
		t.Fatalf("business applications changed after reopen cycles: before=%v after=%v", applicationsBefore, applicationsAfter)
	}
	if reducers.value() != reducerCallsBefore {
		t.Fatalf("reducer re-applied during post-completion recovery: before=%d after=%d", reducerCallsBefore, reducers.value())
	}

	return t032Outcome{
		state:          state,
		history:        normalized,
		attempts:       attemptsAfter,
		applications:   applicationsAfter,
		reducerCalls:   reducers.value(),
		postCommitOnly: !preCommit,
	}
}

func t032AssertEquivalentOutcome(t *testing.T, reference, interrupted t032Outcome, scenario t032CrashScenario) {
	t.Helper()
	if !reflect.DeepEqual(interrupted.state, reference.state) {
		t.Fatalf("state differs after %s/%d: got %#v want %#v", scenario.stage, scenario.occurrence, interrupted.state, reference.state)
	}
	if !reflect.DeepEqual(interrupted.history, reference.history) {
		t.Fatalf("normalized durable history differs after %s/%d:\n got %#v\nwant %#v", scenario.stage, scenario.occurrence, interrupted.history, reference.history)
	}
	if !reflect.DeepEqual(interrupted.applications, reference.applications) {
		t.Fatalf("idempotent business applications differ after %s/%d:\n got %v\nwant %v", scenario.stage, scenario.occurrence, interrupted.applications, reference.applications)
	}
	if len(interrupted.attempts) < len(reference.attempts) {
		t.Fatalf("delivery attempts after %s/%d = %d, below uninterrupted %d", scenario.stage, scenario.occurrence, len(interrupted.attempts), len(reference.attempts))
	}
	for index, id := range interrupted.attempts {
		if _, ok := reference.applications[id]; !ok {
			t.Fatalf("attempt[%d] uses unstable/unknown effect id %q after %s/%d", index, id, scenario.stage, scenario.occurrence)
		}
	}
	wantReducerCalls := reference.reducerCalls
	if scenario.stage == flowFailpointBeforeStateIntentCommit {
		// The reducer may execute before a failed pre-commit boundary, but that
		// speculative result is not durable and the business event must be
		// re-dispatched. Every persisted/post-commit recovery path must not replay it.
		wantReducerCalls++
	}
	if interrupted.reducerCalls != wantReducerCalls {
		t.Fatalf("reducer invocations after %s/%d = %d, want %d", scenario.stage, scenario.occurrence, interrupted.reducerCalls, wantReducerCalls)
	}
}

func t032FailOnOccurrence(
	ctx context.Context,
	stage flowDurableFailpointStage,
	occurrence int,
	crashErr error,
) context.Context {
	seen := 0
	return withDurableFlowFailpoint(ctx, func(event flowDurableFailpointEvent) error {
		if event.Stage != stage {
			return nil
		}
		seen++
		if seen == occurrence {
			return crashErr
		}
		return nil
	})
}

func t032OpenPebble(t *testing.T, dir string) *PebbleFlowStore {
	t.Helper()
	store, err := OpenPebbleFlowStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func t032ClosePebble(t *testing.T, store *PebbleFlowStore) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func t032OpenEngine(
	t *testing.T,
	flow *Flow[t032State],
	store FlowStore,
) *FlowEngine[t032State] {
	t.Helper()
	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func t032StateAndHistory(t *testing.T, execution *FlowExecution[t032State]) (t032State, []FlowHistoryEntry) {
	t.Helper()
	state, err := execution.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	history, err := execution.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return state, history
}

func t032NormalizeHistory(t *testing.T, history []FlowHistoryEntry) []t032HistoryRecord {
	t.Helper()
	normalized := make([]t032HistoryRecord, 0, len(history))
	for _, entry := range history {
		record := t032HistoryRecord{
			Sequence: entry.Sequence,
			Type:     entry.Type,
			Name:     entry.Name,
		}
		switch entry.Type {
		case "EventHandled":
			event, err := decodeFlowHistoryData[t032Event](entry.Data)
			if err != nil {
				t.Fatalf("decode event at sequence %d: %v", entry.Sequence, err)
			}
			payload, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			record.Payload = string(payload)
		case flowHistoryEffectPending:
			intent, err := decodeFlowHistoryData[FlowEffectIntent](entry.Data)
			if err != nil {
				t.Fatalf("decode intent at sequence %d: %v", entry.Sequence, err)
			}
			var command t032Command
			if err := json.Unmarshal(intent.Payload, &command); err != nil {
				t.Fatalf("decode command at sequence %d: %v", entry.Sequence, err)
			}
			payload, err := json.Marshal(command)
			if err != nil {
				t.Fatal(err)
			}
			record.EffectID = intent.ID
			record.Payload = string(payload)
		case flowHistoryEffectCompleted:
			completion, err := decodeFlowHistoryData[FlowEffectCompletion](entry.Data)
			if err != nil {
				t.Fatalf("decode completion at sequence %d: %v", entry.Sequence, err)
			}
			record.EffectID = completion.ID
		default:
			payload, err := json.Marshal(entry.Data)
			if err != nil {
				t.Fatalf("encode history entry at sequence %d: %v", entry.Sequence, err)
			}
			record.Payload = string(payload)
		}
		normalized = append(normalized, record)
	}
	return normalized
}
