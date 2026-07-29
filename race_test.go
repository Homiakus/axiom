package axiom

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Homiakus/axiom/internal/testutil"
)

// ──────────────────────────────────────────────────────────────────────────────
// Race tests: designed to trigger race conditions when run with -race.
// These tests combine concurrent access patterns that stress the engine's
// synchronisation primitives (KeyedLocker, storeMu, store transactions).
// ──────────────────────────────────────────────────────────────────────────────

// TestRaceConcurrentSignalsSameExecution sends signals from multiple goroutines
// to the same execution, verifying no data race and final state correctness.
func TestRaceConcurrentSignalsSameExecution(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "race-same", nil); err != nil {
		t.Fatal(err)
	}

	const workers = 8
	const perWorker = 100
	start := make(chan struct{})
	var wg sync.WaitGroup

	run := engine.Execution("race-same")
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perWorker; j++ {
				if err := run.Signal(ctx, "Ping", nil); err != nil {
					t.Errorf("Signal error: %v", err)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	// Verify final counter value.
	state, err := engine.Query(ctx, "race-same", "state")
	if err != nil {
		t.Fatal(err)
	}
	counter := state["context"].(map[string]map[string]any)["Counter"]
	if counter["count"] != workers*perWorker {
		t.Fatalf("Counter.count = %v, want %d", counter["count"], workers*perWorker)
	}
}

// TestRaceConcurrentPatchAndSignal mixes Patch and Signal calls from
// different goroutines on the same execution.
func TestRaceConcurrentPatchAndSignal(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "race-mixed", nil); err != nil {
		t.Fatal(err)
	}

	const workers = 4
	const ops = 50
	start := make(chan struct{})
	var wg sync.WaitGroup

	// Half do signals, half do patches.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		if i%2 == 0 {
			go func() {
				defer wg.Done()
				<-start
				for j := 0; j < ops; j++ {
					_ = engine.Signal(ctx, "race-mixed", "Ping", nil)
				}
			}()
		} else {
			go func() {
				defer wg.Done()
				<-start
				for j := 0; j < ops; j++ {
					_ = engine.Patch(ctx, "race-mixed", Patch{"Counter.count": j})
				}
			}()
		}
	}
	close(start)
	wg.Wait()

	// Just verify we can query state without crash.
	_, err = engine.Query(ctx, "race-mixed", "state")
	if err != nil {
		t.Fatal(err)
	}
}

// TestRaceConcurrentDifferentExecutions verifies that independent executions
// don't share mutable state.
func TestRaceConcurrentDifferentExecutions(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const executions = 10
	const signals = 50
	start := make(chan struct{})
	var wg sync.WaitGroup

	for e := 0; e < executions; e++ {
		id := fmt.Sprintf("race-exec-%d", e)
		if err := engine.Start(ctx, id, nil); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			for i := 0; i < signals; i++ {
				if err := engine.Signal(ctx, id, "Ping", nil); err != nil {
					t.Errorf("Signal(%s) error: %v", id, err)
					return
				}
			}
		}(id)
	}
	close(start)
	wg.Wait()

	// Verify each execution got exactly 'signals' increments.
	for e := 0; e < executions; e++ {
		id := fmt.Sprintf("race-exec-%d", e)
		state, err := engine.Query(ctx, id, "state")
		if err != nil {
			t.Fatal(err)
		}
		counter := state["context"].(map[string]map[string]any)["Counter"]
		if counter["count"] != signals {
			t.Fatalf("Execution %s: Counter.count = %v, want %d", id, counter["count"], signals)
		}
	}
}

// TestRaceSignalAndQuery mixes Signal and Query from different goroutines.
func TestRaceSignalAndQuery(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "race-query", nil); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup

	// Signaler.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			_ = engine.Signal(ctx, "race-query", "Ping", nil)
		}
	}()

	// Querier.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			_, _ = engine.Query(ctx, "race-query", "state")
		}
	}()

	close(start)
	wg.Wait()
}

// TestRacePebbleConcurrentSignals tests concurrent signals on a Pebble store.
func TestRacePebbleConcurrentSignals(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
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
	if err := engine.Start(ctx, "race-pebble", nil); err != nil {
		t.Fatal(err)
	}

	const workers = 4
	const perWorker = 30
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perWorker; j++ {
				_ = engine.Signal(ctx, "race-pebble", "Ping", nil)
			}
		}()
	}
	close(start)
	wg.Wait()

	state, err := engine.Query(ctx, "race-pebble", "state")
	if err != nil {
		t.Fatal(err)
	}
	counter := state["context"].(map[string]map[string]any)["Counter"]
	if counter["count"] != workers*perWorker {
		t.Fatalf("Counter.count = %v, want %d", counter["count"], workers*perWorker)
	}
}

// TestRaceFlowConcurrentDispatch tests concurrent Dispatch on the Flow
// (Go-first) frontend.
func TestRaceFlowConcurrentDispatch(t *testing.T) {
	type state struct{ Count int }
	type evt struct{ By int }

	flow := NewFlow("race-flow", state{})
	Handle(flow, func(_ context.Context, s state, e evt) (FlowResult[state], error) {
		s.Count += e.By
		return Next(s), nil
	})
	engine, err := OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	const perWorker = 100
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perWorker; j++ {
				if err := engine.Execution("shared").Dispatch(context.Background(), evt{By: 1}); err != nil {
					t.Errorf("Dispatch error: %v", err)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	s, err := engine.Execution("shared").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != workers*perWorker {
		t.Fatalf("Count = %d, want %d", s.Count, workers*perWorker)
	}
}
