package axiom

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestDurableFlowOutboxStatusAndBoundedDrain(t *testing.T) {
	store := newDurableFlowTestStore()
	sink := newT032Sink()
	reducers := &t032ReducerCounter{}
	flow := newT032Flow(sink, reducers)
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("bounded-drain")

	t033LeavePending(t, execution, 5)
	if reducers.value() != 1 {
		t.Fatalf("reducer calls = %d, want 1", reducers.value())
	}
	before, err := execution.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if attempts, _ := sink.snapshot(); len(attempts) != 0 {
		t.Fatalf("effect attempts before bounded drain = %d, want 0", len(attempts))
	}

	status, err := execution.OutboxStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t033AssertStatus(t, status, 6, 5, 0, 2)
	if status.OldestPendingAt.IsZero() {
		t.Fatal("OldestPendingAt is zero with pending work")
	}
	afterStatus, err := execution.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(afterStatus) != len(before) {
		t.Fatalf("OutboxStatus changed history length: before=%d after=%d", len(before), len(afterStatus))
	}
	if attempts, _ := sink.snapshot(); len(attempts) != 0 {
		t.Fatalf("OutboxStatus invoked effect handlers: attempts=%d", len(attempts))
	}

	result, err := execution.DrainEffectsLimit(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 2, 2, 3)
	status, err = execution.OutboxStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t033AssertStatus(t, status, 8, 3, 2, 4)

	result, err = execution.DrainEffectsLimit(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 2, 2, 1)

	result, err = execution.DrainEffectsLimit(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 1, 1, 0)
	status, err = execution.OutboxStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t033AssertStatus(t, status, 11, 0, 5, 0)
	if !status.OldestPendingAt.IsZero() {
		t.Fatalf("OldestPendingAt = %v with empty outbox", status.OldestPendingAt)
	}

	attemptsBefore, applicationsBefore := sink.snapshot()
	result, err = execution.DrainEffectsLimit(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 0, 0, 0)
	attemptsAfter, applicationsAfter := sink.snapshot()
	if len(attemptsAfter) != len(attemptsBefore) || len(applicationsAfter) != len(applicationsBefore) {
		t.Fatalf("completed outbox redelivered: attempts %d->%d applications %d->%d", len(attemptsBefore), len(attemptsAfter), len(applicationsBefore), len(applicationsAfter))
	}
	if reducers.value() != 1 {
		t.Fatalf("reducer was replayed during drains: calls=%d want 1", reducers.value())
	}
}

func TestDurableFlowBoundedDrainPersistsAcrossPebbleReopen(t *testing.T) {
	dir := t.TempDir()
	sink := newT032Sink()
	reducers := &t032ReducerCounter{}
	flow := newT032Flow(sink, reducers)

	store := t032OpenPebble(t, dir)
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("bounded-reopen")
	t033LeavePending(t, execution, 4)

	result, err := execution.DrainEffectsLimit(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 2, 2, 2)
	t032ClosePebble(t, store)

	store = t032OpenPebble(t, dir)
	engine = t032OpenEngine(t, flow, store)
	execution = engine.Execution("bounded-reopen")
	status, err := execution.OutboxStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t033AssertStatus(t, status, 7, 2, 2, 4)

	result, err = execution.DrainEffectsLimit(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 1, 1, 1)
	t032ClosePebble(t, store)

	store = t032OpenPebble(t, dir)
	defer t032ClosePebble(t, store)
	engine = t032OpenEngine(t, flow, store)
	execution = engine.Execution("bounded-reopen")
	if err := execution.DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err = execution.OutboxStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t033AssertStatus(t, status, 9, 0, 4, 0)
	attempts, applications := sink.snapshot()
	if len(attempts) != 4 || len(applications) != 4 {
		t.Fatalf("after reopen drains attempts=%d applications=%d, want 4/4", len(attempts), len(applications))
	}
	if reducers.value() != 1 {
		t.Fatalf("reducer calls across reopen recovery = %d, want 1", reducers.value())
	}
}

func TestDurableFlowBoundedDrainReportsAcknowledgementFailure(t *testing.T) {
	store := newDurableFlowTestStore()
	sink := newT032Sink()
	flow := newT032Flow(sink, &t032ReducerCounter{})
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("ack-failure")
	t033LeavePending(t, execution, 1)

	store.mu.Lock()
	store.failCompletionOnce = true
	store.mu.Unlock()
	result, err := execution.DrainEffectsLimit(context.Background(), 1)
	var acknowledgeErr *FlowEffectAcknowledgeError
	if !errors.As(err, &acknowledgeErr) {
		t.Fatalf("DrainEffectsLimit() error = %v, want FlowEffectAcknowledgeError", err)
	}
	t033AssertDrainResult(t, result, 1, 0, 1)
	status, statusErr := execution.OutboxStatus(context.Background())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	t033AssertStatus(t, status, 2, 1, 0, 2)

	firstAttempts, _ := sink.snapshot()
	if len(firstAttempts) != 1 {
		t.Fatalf("attempts after acknowledgement failure = %d, want 1", len(firstAttempts))
	}
	result, err = execution.DrainEffectsLimit(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 1, 1, 0)
	attempts, applications := sink.snapshot()
	if len(attempts) != 2 || attempts[0] != attempts[1] || len(applications) != 1 {
		t.Fatalf("ack retry attempts=%v applications=%v, want stable ID and one idempotent application", attempts, applications)
	}
}

func TestDurableFlowBoundedDrainStopsOnDeliveryFailure(t *testing.T) {
	store := newDurableFlowTestStore()
	fail := true
	calls := 0
	var ids []string
	flow := NewFlow("t033-delivery-failure", t032State{})
	Handle(flow, func(_ context.Context, state t032State, event t032Event) (FlowResult[t032State], error) {
		state.Total += event.By
		effects := make([]Effect, 0, event.Effects)
		for slot := 0; slot < event.Effects; slot++ {
			effects = append(effects, Call(t032Command{Event: 1, Slot: slot, Value: state.Total}))
		}
		return FlowResult[t032State]{State: state, Effects: effects}, nil
	})
	EffectHandler(flow, func(ctx context.Context, _ t032Command) error {
		calls++
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
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("delivery-failure")
	t033LeavePending(t, execution, 3)

	result, err := execution.DrainEffectsLimit(context.Background(), 2)
	var deliveryErr *FlowEffectDeliveryError
	if !errors.As(err, &deliveryErr) {
		t.Fatalf("DrainEffectsLimit() error = %v, want FlowEffectDeliveryError", err)
	}
	t033AssertDrainResult(t, result, 1, 0, 3)
	if calls != 1 {
		t.Fatalf("handler calls after failure = %d, want 1", calls)
	}

	fail = false
	result, err = execution.DrainEffectsLimit(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	t033AssertDrainResult(t, result, 2, 2, 1)
	if calls != 3 {
		t.Fatalf("handler calls after recovery batch = %d, want 3", calls)
	}
	if len(ids) < 2 || ids[0] != ids[1] {
		t.Fatalf("failed effect did not retry with stable ID: ids=%v", ids)
	}
}

func TestDurableFlowDrainLimitValidationAndLegacyDrainAll(t *testing.T) {
	store := newDurableFlowTestStore()
	sink := newT032Sink()
	flow := newT032Flow(sink, &t032ReducerCounter{})
	engine := t032OpenEngine(t, flow, store)
	execution := engine.Execution("legacy-drain-all")
	t033LeavePending(t, execution, 3)

	for _, limit := range []int{0, -1, -10} {
		if _, err := execution.DrainEffectsLimit(context.Background(), limit); err == nil {
			t.Fatalf("DrainEffectsLimit(%d) expected validation error", limit)
		}
	}
	if attempts, _ := sink.snapshot(); len(attempts) != 0 {
		t.Fatalf("invalid limits invoked handlers: attempts=%d", len(attempts))
	}
	status, err := execution.OutboxStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t033AssertStatus(t, status, 4, 3, 0, 2)

	if err := execution.DrainEffects(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err = execution.OutboxStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t033AssertStatus(t, status, 7, 0, 3, 0)
	attempts, applications := sink.snapshot()
	if len(attempts) != 3 || len(applications) != 3 {
		t.Fatalf("legacy DrainEffects attempts=%d applications=%d, want 3/3", len(attempts), len(applications))
	}
}

func BenchmarkDurableFlowOutboxStatusLargeHistory(b *testing.B) {
	store := newDurableFlowTestStore()
	flow := newDurableTestFlow(func(context.Context, durableFlowCommand) error { return nil })
	engine, err := OpenFlow(flow, WithFlowStore(store), WithDurableFlowEffects())
	if err != nil {
		b.Fatal(err)
	}
	execution := engine.Execution("large-history")

	const effects = 5000
	const completedEffects = 4500
	entries := make([]FlowHistoryEntry, 0, effects+completedEffects)
	sequence := 1
	for i := 0; i < effects; i++ {
		id := fmt.Sprintf("bench-effect-%05d", i)
		entries = append(entries, FlowHistoryEntry{
			Sequence: sequence,
			Type:     flowHistoryEffectPending,
			Name:     "bench",
			Data:     FlowEffectIntent{ID: id, Name: "bench"},
		})
		sequence++
		if i < completedEffects {
			entries = append(entries, FlowHistoryEntry{
				Sequence: sequence,
				Type:     flowHistoryEffectCompleted,
				Name:     "bench",
				Data:     FlowEffectCompletion{ID: id},
			})
			sequence++
		}
	}
	if err := store.SaveStateAndAppend(context.Background(), "durable-test", "large-history", []byte(`{"count":0}`), entries); err != nil {
		b.Fatal(err)
	}
	status, err := execution.OutboxStatus(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	if status.HistoryLength != len(entries) || status.Pending != effects-completedEffects || status.Completed != completedEffects {
		b.Fatalf("unexpected benchmark status: %#v entries=%d", status, len(entries))
	}
	b.ReportMetric(float64(len(entries)), "history_entries")
	b.ResetTimer()
	for b.Loop() {
		if _, err := execution.OutboxStatus(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func t033LeavePending(t *testing.T, execution *FlowExecution[t032State], effects int) {
	t.Helper()
	crashErr := errors.New("t033 stop after state and intents commit")
	ctx := t032FailOnOccurrence(context.Background(), flowFailpointAfterStateIntentCommit, 1, crashErr)
	err := execution.Dispatch(ctx, t032Event{By: 1, Effects: effects})
	if !errors.Is(err, crashErr) {
		t.Fatalf("Dispatch() error = %v, want %v", err, crashErr)
	}
}

func t033AssertStatus(t *testing.T, got FlowOutboxStatus, historyLength, pending, completed, oldestSequence int) {
	t.Helper()
	if got.HistoryLength != historyLength || got.Pending != pending || got.Completed != completed || got.OldestPendingSequence != oldestSequence {
		t.Fatalf("outbox status = %#v, want history=%d pending=%d completed=%d oldest=%d", got, historyLength, pending, completed, oldestSequence)
	}
	if got.HasPending() != (pending > 0) {
		t.Fatalf("HasPending() = %v, want %v", got.HasPending(), pending > 0)
	}
}

func t033AssertDrainResult(t *testing.T, got FlowOutboxDrainResult, attempted, acknowledged, remaining int) {
	t.Helper()
	want := FlowOutboxDrainResult{Attempted: attempted, Acknowledged: acknowledged, Remaining: remaining}
	if got != want {
		t.Fatalf("drain result = %#v, want %#v", got, want)
	}
}
