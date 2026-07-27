package axiom

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

const resilienceCounterSource = `domain ResilienceCounter

signal Increment:
  by: Int

context Counter:
  value: Int = 0

rule increment:
  on Increment
  write:
    Counter.value = Counter.value + signal.by
`

type resilienceState struct {
	Value int `json:"value"`
}

type resilienceIncrement struct {
	By int `json:"by"`
}

func (resilienceIncrement) AxiomEventName() string { return "Increment" }

func TestFlowConcurrentSameExecutionPreservesEveryUpdate(t *testing.T) {
	flow := NewFlow("flow-concurrent-shared", resilienceState{})
	Handle(flow, func(_ context.Context, state resilienceState, event resilienceIncrement) (FlowResult[resilienceState], error) {
		state.Value += event.By
		return Next(state), nil
	})
	engine, err := OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	const perWorker = 200
	runConcurrent(t, workers, perWorker, func(_ int) error {
		return engine.Execution("shared").Dispatch(context.Background(), resilienceIncrement{By: 1})
	})
	state, err := engine.Execution("shared").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := workers * perWorker; state.Value != want {
		t.Fatalf("state.Value = %d, want %d", state.Value, want)
	}
	history, err := engine.Execution("shared").History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := workers * perWorker; len(history) != want {
		t.Fatalf("history entries = %d, want %d", len(history), want)
	}
}

func TestRuntimeConcurrentSameExecutionPreservesEveryUpdate(t *testing.T) {
	module, err := Compile([]byte(resilienceCounterSource))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "shared", nil); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	const perWorker = 100
	run := engine.Execution("shared")
	runConcurrent(t, workers, perWorker, func(_ int) error {
		return run.Dispatch(ctx, resilienceIncrement{By: 1})
	})
	var state resilienceState
	if err := run.State(ctx, &state); err != nil {
		t.Fatal(err)
	}
	if want := workers * perWorker; state.Value != want {
		t.Fatalf("state.Value = %d, want %d", state.Value, want)
	}
}

func TestRuntimePebbleParallelExecutionsDoNotShareTransactionState(t *testing.T) {
	module, err := Compile([]byte(resilienceCounterSource))
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenPebble(t.TempDir(), PebbleNoSync())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine, err := New(module, WithStore(store), WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const workers = 8
	const perWorker = 40
	runConcurrent(t, workers, perWorker, func(worker int) error {
		return engine.Execution(fmt.Sprintf("worker-%d", worker)).Dispatch(ctx, resilienceIncrement{By: 1})
	})
	for worker := 0; worker < workers; worker++ {
		var state resilienceState
		if err := engine.Execution(fmt.Sprintf("worker-%d", worker)).State(ctx, &state); err != nil {
			t.Fatal(err)
		}
		if state.Value != perWorker {
			t.Fatalf("worker %d state.Value = %d, want %d", worker, state.Value, perWorker)
		}
	}
}

func TestFlowFailedEffectDoesNotCommitStateOrHistory(t *testing.T) {
	testErr := errors.New("effect failed")
	type command struct{}
	flow := NewFlow("failed-effect", resilienceState{})
	Handle(flow, func(_ context.Context, state resilienceState, event resilienceIncrement) (FlowResult[resilienceState], error) {
		state.Value += event.By
		return Next(state, Call(command{})), nil
	})
	EffectHandler(flow, func(context.Context, command) error { return testErr })
	engine, err := OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}
	run := engine.Execution("one")
	if err := run.Dispatch(context.Background(), resilienceIncrement{By: 1}); !errors.Is(err, testErr) {
		t.Fatalf("Dispatch() error = %v, want %v", err, testErr)
	}
	state, err := run.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Value != 0 {
		t.Fatalf("state committed after failed effect: %d", state.Value)
	}
	history, err := run.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("history committed after failed effect: %#v", history)
	}
}

func TestFlowParallelSoak(t *testing.T) {
	if testing.Short() {
		t.Skip("soak test disabled in short mode")
	}
	flow := NewFlow("flow-soak", resilienceState{})
	Handle(flow, func(_ context.Context, state resilienceState, event resilienceIncrement) (FlowResult[resilienceState], error) {
		state.Value += event.By
		return Next(state), nil
	})
	engine, err := OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	const perWorker = 500
	runConcurrent(t, workers, perWorker, func(worker int) error {
		return engine.Execution(fmt.Sprintf("soak-%d", worker)).Dispatch(context.Background(), resilienceIncrement{By: 1})
	})
	for worker := 0; worker < workers; worker++ {
		state, err := engine.Execution(fmt.Sprintf("soak-%d", worker)).State(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if state.Value != perWorker {
			t.Fatalf("worker %d state.Value = %d, want %d", worker, state.Value, perWorker)
		}
	}
}

func runConcurrent(t *testing.T, workers, perWorker int, operation func(worker int) error) {
	t.Helper()
	start := make(chan struct{})
	errorsChannel := make(chan error, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			<-start
			for index := 0; index < perWorker; index++ {
				if err := operation(worker); err != nil {
					errorsChannel <- fmt.Errorf("worker %d operation %d: %w", worker, index, err)
					return
				}
			}
		}(worker)
	}
	close(start)
	group.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
}
