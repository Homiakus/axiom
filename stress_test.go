package axiom

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/Homiakus/axiom/internal/testutil"
)

// ──────────────────────────────────────────────────────────────────────────────
// Stress tests: long-running, high-volume tests designed to surface
// resource leaks, goroutine leaks, memory growth, and rare race conditions.
// Skipped in short mode. Run with:
//
//	go test -run TestStress -v -count=1 -timeout=5m
// ──────────────────────────────────────────────────────────────────────────────

func TestStressHighVolumeSignals(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in short mode")
	}

	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "stress-signals", nil); err != nil {
		t.Fatal(err)
	}

	const totalSignals = 50_000
	for i := 0; i < totalSignals; i++ {
		if err := engine.Signal(ctx, "stress-signals", "Ping", nil); err != nil {
			t.Fatalf("Signal() error at %d: %v", i, err)
		}
	}

	state, err := engine.Query(ctx, "stress-signals", "state")
	if err != nil {
		t.Fatal(err)
	}
	counter := state["context"].(map[string]map[string]any)["Counter"]
	if counter["count"] != totalSignals {
		t.Fatalf("Counter.count = %v, want %d", counter["count"], totalSignals)
	}
	t.Logf("Successfully processed %d signals", totalSignals)
}

func TestStressManyExecutions(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in short mode")
	}

	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const executions = 5_000
	const signalsPerExec = 10

	for e := 0; e < executions; e++ {
		id := fmt.Sprintf("stress-exec-%d", e)
		if err := engine.Start(ctx, id, nil); err != nil {
			t.Fatalf("Start(%s) error: %v", id, err)
		}
		for s := 0; s < signalsPerExec; s++ {
			if err := engine.Signal(ctx, id, "Ping", nil); err != nil {
				t.Fatalf("Signal(%s) error: %v", id, err)
			}
		}
	}

	// Spot check a few executions.
	for _, e := range []int{0, executions / 2, executions - 1} {
		id := fmt.Sprintf("stress-exec-%d", e)
		state, err := engine.Query(ctx, id, "state")
		if err != nil {
			t.Fatal(err)
		}
		counter := state["context"].(map[string]map[string]any)["Counter"]
		if counter["count"] != signalsPerExec {
			t.Fatalf("Execution %s: count = %v, want %d", id, counter["count"], signalsPerExec)
		}
	}
	t.Logf("Successfully created %d executions × %d signals", executions, signalsPerExec)
}

func TestStressConcurrentMixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in short mode")
	}

	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	const workers = 16
	const opsPerWorker = 500

	// Pre-create executions.
	for w := 0; w < workers; w++ {
		id := fmt.Sprintf("stress-mixed-%d", w)
		if err := engine.Start(ctx, id, nil); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("stress-mixed-%d", w)
			for i := 0; i < opsPerWorker; i++ {
				switch i % 3 {
				case 0:
					if err := engine.Signal(ctx, id, "Ping", nil); err != nil {
						errs <- fmt.Errorf("worker %d Signal: %w", w, err)
						return
					}
				case 1:
					if err := engine.Patch(ctx, id, Patch{"Counter.count": i}); err != nil {
						errs <- fmt.Errorf("worker %d Patch: %w", w, err)
						return
					}
				case 2:
					if _, err := engine.Query(ctx, id, "state"); err != nil {
						errs <- fmt.Errorf("worker %d Query: %w", w, err)
						return
					}
				}
			}
		}(w)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	t.Logf("Completed %d workers × %d ops", workers, opsPerWorker)
}

// TestStressGoroutineLeakAfterSignals verifies that the engine does not
// leak goroutines after processing many signals.
func TestStressGoroutineLeakAfterSignals(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in short mode")
	}

	testutil.GoroutineLeakCheck(t, 5, func() {
		module, err := Compile([]byte(testutil.SimpleSignalModule))
		if err != nil {
			t.Fatal(err)
		}
		engine, err := New(module, WithTraceLevel(TraceMinimal))
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if err := engine.Start(ctx, "leak-check", nil); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 1000; i++ {
			_ = engine.Signal(ctx, "leak-check", "Ping", nil)
		}
	})
}

// TestStressGoroutineLeakAfterRunUntilIdle verifies RunUntilIdle doesn't
// leak goroutines.
func TestStressGoroutineLeakAfterRunUntilIdle(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in short mode")
	}

	testutil.GoroutineLeakCheck(t, 5, func() {
		module, err := Compile([]byte(welcomeRuntimeSource))
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 50; i++ {
			engine, err := New(module,
				WithTraceLevel(TraceMinimal),
				WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
					return Output{"sent": true}, nil
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			id := fmt.Sprintf("leak-idle-%d", i)
			_ = engine.Start(ctx, id, nil)
			_ = engine.Signal(ctx, id, "UserRegistered", Input{"userId": "u1", "email": "a@b.com"})
			_ = engine.RunUntilIdle(ctx, id)
		}
	})
}

// TestStressMemoryStability runs many operations and checks that memory
// usage doesn't grow unboundedly (via runtime.ReadMemStats).
func TestStressMemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped in short mode")
	}

	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "memory", nil); err != nil {
		t.Fatal(err)
	}

	// Warm up and measure baseline.
	for i := 0; i < 1000; i++ {
		_ = engine.Signal(ctx, "memory", "Ping", nil)
	}
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	// Run more signals.
	for i := 0; i < 10_000; i++ {
		_ = engine.Signal(ctx, "memory", "Ping", nil)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	var heapGrowthMB float64
	if after.HeapAlloc >= baseline.HeapAlloc {
		heapGrowthMB = float64(after.HeapAlloc-baseline.HeapAlloc) / (1024 * 1024)
	} else {
		heapGrowthMB = -float64(baseline.HeapAlloc-after.HeapAlloc) / (1024 * 1024)
	}
	t.Logf("Heap growth after 10K signals: %.2f MB (baseline: %.2f MB, after: %.2f MB)",
		heapGrowthMB,
		float64(baseline.HeapAlloc)/(1024*1024),
		float64(after.HeapAlloc)/(1024*1024),
	)

	// If heap grows by more than 50 MB for 10K simple signals, something is leaking.
	if heapGrowthMB > 50 {
		t.Errorf("excessive heap growth: %.2f MB", heapGrowthMB)
	}
}
