package axiom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type durableRecoverySink struct {
	mu                 sync.Mutex
	attempts           []string
	applications       map[string]int
	failAfterApplyOnce bool
}

func newDurableRecoverySink() *durableRecoverySink {
	return &durableRecoverySink{applications: make(map[string]int)}
}

func (s *durableRecoverySink) handle(ctx context.Context, command durableFlowCommand) error {
	id, ok := FlowEffectIDFromContext(ctx)
	if !ok {
		return errors.New("missing durable effect id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, id)
	if _, applied := s.applications[id]; !applied {
		s.applications[id] = command.Value
	}
	if s.failAfterApplyOnce {
		s.failAfterApplyOnce = false
		return errors.New("synthetic ambiguous effect failure after external apply")
	}
	return nil
}

func (s *durableRecoverySink) snapshot() ([]string, map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempts := append([]string(nil), s.attempts...)
	applications := make(map[string]int, len(s.applications))
	for id, value := range s.applications {
		applications[id] = value
	}
	return attempts, applications
}

type failAcknowledgeOncePebbleStore struct {
	*PebbleFlowStore
	mu       sync.Mutex
	failOnce bool
}

func (s *failAcknowledgeOncePebbleStore) SaveStateAndAppend(
	ctx context.Context,
	flow string,
	id string,
	state []byte,
	entries []FlowHistoryEntry,
) error {
	s.mu.Lock()
	fail := false
	if s.failOnce {
		for _, entry := range entries {
			if entry.Type == flowHistoryEffectCompleted {
				s.failOnce = false
				fail = true
				break
			}
		}
	}
	s.mu.Unlock()
	if fail {
		return errors.New("synthetic acknowledge commit failure")
	}
	return s.PebbleFlowStore.SaveStateAndAppend(ctx, flow, id, state, entries)
}

func TestDurableFlowCrashRecoveryMatrix(t *testing.T) {
	t.Run("before state and intent commit re-dispatches from clean durable state", func(t *testing.T) {
		dir := t.TempDir()
		sink := newDurableRecoverySink()
		flow := newDurableTestFlow(sink.handle)
		store := openDurableRecoveryPebble(t, dir)
		engine := openDurableRecoveryEngine(t, flow, store)

		crashErr := errors.New("crash before state and intent commit")
		ctx := failDurableRecoveryAt(context.Background(), flowFailpointBeforeStateIntentCommit, crashErr)
		err := engine.Execution("before-commit").Dispatch(ctx, durableFlowEvent{By: 3})
		if !errors.Is(err, crashErr) {
			t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
		}
		_, historyLength, found, err := store.LoadState(context.Background(), "durable-test", "before-commit")
		if err != nil {
			t.Fatal(err)
		}
		if found || historyLength != 0 {
			t.Fatalf("state before commit: found=%v historyLength=%d, want false/0", found, historyLength)
		}
		assertDurableRecoverySink(t, sink, "", 0, 0, 0)
		closeDurableRecoveryPebble(t, store)

		store = openDurableRecoveryPebble(t, dir)
		defer closeDurableRecoveryPebble(t, store)
		engine = openDurableRecoveryEngine(t, flow, store)
		if err := engine.Execution("before-commit").Dispatch(context.Background(), durableFlowEvent{By: 3}); err != nil {
			t.Fatal(err)
		}
		assertDurableRecoveryCompleted(t, engine, sink, "before-commit", 3, 1, 1)
	})

	t.Run("after state and intent commit drains pending without reducer replay", func(t *testing.T) {
		dir := t.TempDir()
		sink := newDurableRecoverySink()
		flow := newDurableTestFlow(sink.handle)
		store := openDurableRecoveryPebble(t, dir)
		engine := openDurableRecoveryEngine(t, flow, store)

		crashErr := errors.New("crash after state and intent commit")
		ctx := failDurableRecoveryAt(context.Background(), flowFailpointAfterStateIntentCommit, crashErr)
		err := engine.Execution("after-commit").Dispatch(ctx, durableFlowEvent{By: 4})
		if !errors.Is(err, crashErr) {
			t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
		}
		pendingID := assertDurableRecoveryPending(t, store, "after-commit", 4)
		assertDurableRecoverySink(t, sink, pendingID, 0, 0, 4)
		closeDurableRecoveryPebble(t, store)

		store = openDurableRecoveryPebble(t, dir)
		defer closeDurableRecoveryPebble(t, store)
		engine = openDurableRecoveryEngine(t, flow, store)
		if err := engine.Execution("after-commit").DrainEffects(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertDurableRecoveryCompleted(t, engine, sink, "after-commit", 4, 1, 1)
	})

	t.Run("ambiguous effect failure retries same id and business apply stays idempotent", func(t *testing.T) {
		dir := t.TempDir()
		sink := newDurableRecoverySink()
		sink.failAfterApplyOnce = true
		flow := newDurableTestFlow(sink.handle)
		store := openDurableRecoveryPebble(t, dir)
		engine := openDurableRecoveryEngine(t, flow, store)

		err := engine.Execution("ambiguous-effect").Dispatch(context.Background(), durableFlowEvent{By: 5})
		var deliveryErr *FlowEffectDeliveryError
		if !errors.As(err, &deliveryErr) || !deliveryErr.StateCommitted() {
			t.Fatalf("Dispatch() error = %v, want committed FlowEffectDeliveryError", err)
		}
		pendingID := assertDurableRecoveryPending(t, store, "ambiguous-effect", 5)
		if deliveryErr.EffectID != pendingID {
			t.Fatalf("delivery error effect id = %q, want %q", deliveryErr.EffectID, pendingID)
		}
		assertDurableRecoverySink(t, sink, pendingID, 1, 1, 5)
		closeDurableRecoveryPebble(t, store)

		store = openDurableRecoveryPebble(t, dir)
		defer closeDurableRecoveryPebble(t, store)
		engine = openDurableRecoveryEngine(t, flow, store)
		if err := engine.Execution("ambiguous-effect").DrainEffects(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertDurableRecoveryCompleted(t, engine, sink, "ambiguous-effect", 5, 2, 1)
	})

	t.Run("after effect success before acknowledgement redelivers same id", func(t *testing.T) {
		dir := t.TempDir()
		sink := newDurableRecoverySink()
		flow := newDurableTestFlow(sink.handle)
		store := openDurableRecoveryPebble(t, dir)
		engine := openDurableRecoveryEngine(t, flow, store)

		crashErr := errors.New("crash after effect success before acknowledge")
		ctx := failDurableRecoveryAt(context.Background(), flowFailpointAfterEffectDelivery, crashErr)
		err := engine.Execution("after-effect").Dispatch(ctx, durableFlowEvent{By: 6})
		if !errors.Is(err, crashErr) {
			t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
		}
		pendingID := assertDurableRecoveryPending(t, store, "after-effect", 6)
		assertDurableRecoverySink(t, sink, pendingID, 1, 1, 6)
		closeDurableRecoveryPebble(t, store)

		store = openDurableRecoveryPebble(t, dir)
		defer closeDurableRecoveryPebble(t, store)
		engine = openDurableRecoveryEngine(t, flow, store)
		if err := engine.Execution("after-effect").DrainEffects(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertDurableRecoveryCompleted(t, engine, sink, "after-effect", 6, 2, 1)
	})

	t.Run("acknowledge commit failure over real pebble redelivers same id", func(t *testing.T) {
		dir := t.TempDir()
		sink := newDurableRecoverySink()
		flow := newDurableTestFlow(sink.handle)
		baseStore := openDurableRecoveryPebble(t, dir)
		store := &failAcknowledgeOncePebbleStore{PebbleFlowStore: baseStore, failOnce: true}
		engine := openDurableRecoveryEngine(t, flow, store)

		err := engine.Execution("ack-failure").Dispatch(context.Background(), durableFlowEvent{By: 7})
		var acknowledgeErr *FlowEffectAcknowledgeError
		if !errors.As(err, &acknowledgeErr) || !acknowledgeErr.StateCommitted() {
			t.Fatalf("Dispatch() error = %v, want committed FlowEffectAcknowledgeError", err)
		}
		pendingID := assertDurableRecoveryPending(t, baseStore, "ack-failure", 7)
		if acknowledgeErr.EffectID != pendingID {
			t.Fatalf("acknowledge error effect id = %q, want %q", acknowledgeErr.EffectID, pendingID)
		}
		assertDurableRecoverySink(t, sink, pendingID, 1, 1, 7)
		closeDurableRecoveryPebble(t, baseStore)

		baseStore = openDurableRecoveryPebble(t, dir)
		defer closeDurableRecoveryPebble(t, baseStore)
		engine = openDurableRecoveryEngine(t, flow, baseStore)
		if err := engine.Execution("ack-failure").DrainEffects(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertDurableRecoveryCompleted(t, engine, sink, "ack-failure", 7, 2, 1)
	})

	t.Run("after acknowledgement commit recovery does not redeliver", func(t *testing.T) {
		dir := t.TempDir()
		sink := newDurableRecoverySink()
		flow := newDurableTestFlow(sink.handle)
		store := openDurableRecoveryPebble(t, dir)
		engine := openDurableRecoveryEngine(t, flow, store)

		crashErr := errors.New("crash after acknowledge commit")
		ctx := failDurableRecoveryAt(context.Background(), flowFailpointAfterAcknowledgeCommit, crashErr)
		err := engine.Execution("after-ack").Dispatch(ctx, durableFlowEvent{By: 8})
		if !errors.Is(err, crashErr) {
			t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
		}
		pendingID := assertDurableRecoveryCompletedInStore(t, store, "after-ack", 8)
		assertDurableRecoverySink(t, sink, pendingID, 1, 1, 8)
		closeDurableRecoveryPebble(t, store)

		store = openDurableRecoveryPebble(t, dir)
		defer closeDurableRecoveryPebble(t, store)
		engine = openDurableRecoveryEngine(t, flow, store)
		if err := engine.Execution("after-ack").DrainEffects(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertDurableRecoveryCompleted(t, engine, sink, "after-ack", 8, 1, 1)
	})

	t.Run("recovery can crash before delivery and resume after another reopen", func(t *testing.T) {
		dir := t.TempDir()
		sink := newDurableRecoverySink()
		flow := newDurableTestFlow(sink.handle)
		store := openDurableRecoveryPebble(t, dir)
		engine := openDurableRecoveryEngine(t, flow, store)

		initialCrash := errors.New("initial crash after pending intent commit")
		ctx := failDurableRecoveryAt(context.Background(), flowFailpointAfterStateIntentCommit, initialCrash)
		err := engine.Execution("recovery-crash").Dispatch(ctx, durableFlowEvent{By: 9})
		if !errors.Is(err, initialCrash) {
			t.Fatalf("Dispatch() error = %v, want %v", err, initialCrash)
		}
		pendingID := assertDurableRecoveryPending(t, store, "recovery-crash", 9)
		closeDurableRecoveryPebble(t, store)

		store = openDurableRecoveryPebble(t, dir)
		engine = openDurableRecoveryEngine(t, flow, store)
		recoveryCrash := errors.New("crash during recovery before effect delivery")
		recoveryCtx := failDurableRecoveryAt(context.Background(), flowFailpointBeforeEffectDelivery, recoveryCrash)
		err = engine.Execution("recovery-crash").DrainEffects(recoveryCtx)
		if !errors.Is(err, recoveryCrash) {
			t.Fatalf("first DrainEffects() error = %v, want %v", err, recoveryCrash)
		}
		assertDurableRecoveryPending(t, store, "recovery-crash", 9)
		assertDurableRecoverySink(t, sink, pendingID, 0, 0, 9)
		closeDurableRecoveryPebble(t, store)

		store = openDurableRecoveryPebble(t, dir)
		defer closeDurableRecoveryPebble(t, store)
		engine = openDurableRecoveryEngine(t, flow, store)
		if err := engine.Execution("recovery-crash").DrainEffects(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertDurableRecoveryCompleted(t, engine, sink, "recovery-crash", 9, 1, 1)
	})
}

func failDurableRecoveryAt(ctx context.Context, stage flowDurableFailpointStage, crashErr error) context.Context {
	return withDurableFlowFailpoint(ctx, func(event flowDurableFailpointEvent) error {
		if event.Stage == stage {
			return crashErr
		}
		return nil
	})
}

func openDurableRecoveryPebble(t *testing.T, dir string) *PebbleFlowStore {
	t.Helper()
	store, err := OpenPebbleFlowStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func closeDurableRecoveryPebble(t *testing.T, store *PebbleFlowStore) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func openDurableRecoveryEngine(
	t *testing.T,
	flow *Flow[durableFlowState],
	store FlowStore,
) *FlowEngine[durableFlowState] {
	t.Helper()
	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func assertDurableRecoveryPending(t *testing.T, store *PebbleFlowStore, executionID string, wantCount int) string {
	t.Helper()
	stateBytes, historyLength, found, err := store.LoadState(context.Background(), "durable-test", executionID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || historyLength != 2 {
		t.Fatalf("pending state: found=%v historyLength=%d, want true/2", found, historyLength)
	}
	var state durableFlowState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if state.Count != wantCount {
		t.Fatalf("state.Count = %d, want %d", state.Count, wantCount)
	}
	history, err := store.LoadHistory(context.Background(), "durable-test", executionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Type != "EventHandled" || history[1].Type != flowHistoryEffectPending {
		t.Fatalf("pending history = %#v", history)
	}
	intent, err := decodeFlowHistoryData[FlowEffectIntent](history[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	if intent.ID == "" {
		t.Fatal("pending effect id is empty")
	}
	return intent.ID
}

func assertDurableRecoveryCompletedInStore(t *testing.T, store *PebbleFlowStore, executionID string, wantCount int) string {
	t.Helper()
	stateBytes, historyLength, found, err := store.LoadState(context.Background(), "durable-test", executionID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || historyLength != 3 {
		t.Fatalf("completed state: found=%v historyLength=%d, want true/3", found, historyLength)
	}
	var state durableFlowState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if state.Count != wantCount {
		t.Fatalf("state.Count = %d, want %d", state.Count, wantCount)
	}
	history, err := store.LoadHistory(context.Background(), "durable-test", executionID)
	if err != nil {
		t.Fatal(err)
	}
	return assertDurableRecoveryHistoryCompleted(t, history)
}

func assertDurableRecoveryCompleted(
	t *testing.T,
	engine *FlowEngine[durableFlowState],
	sink *durableRecoverySink,
	executionID string,
	wantCount int,
	wantAttempts int,
	wantApplications int,
) {
	t.Helper()
	execution := engine.Execution(executionID)
	state, err := execution.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Count != wantCount {
		t.Fatalf("state.Count = %d, want %d", state.Count, wantCount)
	}
	history, err := execution.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pendingID := assertDurableRecoveryHistoryCompleted(t, history)
	assertDurableRecoverySink(t, sink, pendingID, wantAttempts, wantApplications, wantCount)
}

func assertDurableRecoveryHistoryCompleted(t *testing.T, history []FlowHistoryEntry) string {
	t.Helper()
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3: %#v", len(history), history)
	}
	wantTypes := []string{"EventHandled", flowHistoryEffectPending, flowHistoryEffectCompleted}
	for i, want := range wantTypes {
		if history[i].Sequence != i+1 || history[i].Type != want {
			t.Fatalf("history[%d] = sequence %d type %q, want sequence %d type %q", i, history[i].Sequence, history[i].Type, i+1, want)
		}
	}
	intent, err := decodeFlowHistoryData[FlowEffectIntent](history[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := decodeFlowHistoryData[FlowEffectCompletion](history[2].Data)
	if err != nil {
		t.Fatal(err)
	}
	if intent.ID == "" || completion.ID != intent.ID {
		t.Fatalf("intent/completion ids = %q/%q", intent.ID, completion.ID)
	}
	return intent.ID
}

func assertDurableRecoverySink(
	t *testing.T,
	sink *durableRecoverySink,
	wantID string,
	wantAttempts int,
	wantApplications int,
	wantValue int,
) {
	t.Helper()
	attempts, applications := sink.snapshot()
	if len(attempts) != wantAttempts {
		t.Fatalf("delivery attempts = %#v, want %d", attempts, wantAttempts)
	}
	for i, id := range attempts {
		if wantID != "" && id != wantID {
			t.Fatalf("attempt[%d] effect id = %q, want %q", i, id, wantID)
		}
	}
	if len(applications) != wantApplications {
		t.Fatalf("unique business applications = %#v, want %d", applications, wantApplications)
	}
	if wantApplications == 0 {
		return
	}
	if value, ok := applications[wantID]; !ok || value != wantValue {
		t.Fatalf("application[%q] = %d/%v, want %d/true", wantID, value, ok, wantValue)
	}
	for id := range applications {
		if id != wantID {
			t.Fatalf("unexpected applied effect id %q, want only %q", id, wantID)
		}
	}
}

func (s *durableRecoverySink) String() string {
	attempts, applications := s.snapshot()
	return fmt.Sprintf("attempts=%v applications=%v", attempts, applications)
}
