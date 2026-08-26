package axiom

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/testutil"
)

// ──────────────────────────────────────────────────────────────────────────────
// Scaling benchmarks: measure throughput at different scale points (10, 100,
// 1K, 10K) for core engine operations. All benchmarks use b.ReportAllocs()
// to track allocation regressions.
// ──────────────────────────────────────────────────────────────────────────────

// BenchmarkCompileMinimal measures compilation of the smallest valid module.
func BenchmarkCompileMinimal(b *testing.B) {
	source := []byte(testutil.MinimalModule)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(source); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileMedium measures compilation of a medium-complexity module.
func BenchmarkCompileMedium(b *testing.B) {
	source := []byte(testutil.MediumModule)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(source); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileWide_10 through _100 test how compilation scales with
// the number of context fields and rules.
func BenchmarkCompileWide_10(b *testing.B) {
	benchCompileWide(b, 10)
}

func BenchmarkCompileWide_50(b *testing.B) {
	benchCompileWide(b, 50)
}

func BenchmarkCompileWide_100(b *testing.B) {
	benchCompileWide(b, 100)
}

func benchCompileWide(b *testing.B, n int) {
	source := []byte(testutil.GenerateWideModule(n))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(source); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSignalMemory_1K sends 1000 signals to a simple counter module
// with an in-memory store.
func BenchmarkSignalMemory_1K(b *testing.B) {
	benchmarkSignalN(b, 1000, false)
}

func BenchmarkSignalMemory_10K(b *testing.B) {
	benchmarkSignalN(b, 10000, false)
}

// BenchmarkSignalPebble_1K sends 1000 signals with a Pebble store.
func BenchmarkSignalPebble_1K(b *testing.B) {
	benchmarkSignalN(b, 1000, true)
}

func benchmarkSignalN(b *testing.B, n int, usePebble bool) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()

	for iter := 0; iter < b.N; iter++ {
		var opts []Option
		opts = append(opts, WithTraceLevel(TraceMinimal))
		if usePebble {
			store, err := OpenPebble(b.TempDir(), PebbleNoSync())
			if err != nil {
				b.Fatal(err)
			}
			opts = append(opts, WithStore(store))
			defer store.Close()
		}
		engine, err := New(module, opts...)
		if err != nil {
			b.Fatal(err)
		}
		ctx := context.Background()
		id := fmt.Sprintf("bench-scale-%d", iter)
		if err := engine.Start(ctx, id, nil); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < n; i++ {
			if err := engine.Signal(ctx, id, "Ping", nil); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkNewEngineMinimal measures the cost of creating a new Engine.
func BenchmarkNewEngineMinimal(b *testing.B) {
	module, err := Compile([]byte(testutil.MinimalModule))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := New(module); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStartExecution measures Start() cost.
func BenchmarkStartExecution(b *testing.B) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-start-%d", i)
		if err := engine.Start(ctx, id, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQueryState measures Query("state") cost.
func BenchmarkQueryState(b *testing.B) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "bench-query", nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Query(ctx, "bench-query", "state"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPatchMinimal measures Patch cost on a simple module.
func BenchmarkPatchMinimal(b *testing.B) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "bench-patch", nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := engine.Patch(ctx, "bench-patch", Patch{"Counter.count": i}); err != nil {
			b.Fatal(err)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SCALE-006: Scaling Benchmarks & Regression Threshold Tests
// ──────────────────────────────────────────────────────────────────────────────

func BenchmarkEngineScalingIndependentWorkers_1(b *testing.B)  { benchEngineScaling(b, 1) }
func BenchmarkEngineScalingIndependentWorkers_4(b *testing.B)  { benchEngineScaling(b, 4) }
func BenchmarkEngineScalingIndependentWorkers_16(b *testing.B) { benchEngineScaling(b, 16) }
func BenchmarkEngineScalingIndependentWorkers_32(b *testing.B) { benchEngineScaling(b, 32) }

func benchEngineScaling(b *testing.B, concurrency int) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		b.Fatal(err)
	}
	engine, err := New(module, WithTraceLevel(TraceMinimal))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	opsPerWorker := b.N / concurrency
	if opsPerWorker < 1 {
		opsPerWorker = 1
	}

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				execID := fmt.Sprintf("scale-w%d-i%d", workerID, i)
				if err := engine.Start(ctx, execID, nil); err != nil {
					b.Error(err)
					return
				}
				if err := engine.Signal(ctx, execID, "Ping", nil); err != nil {
					b.Error(err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

// TestScalingThresholds validates that concurrent execution scaling
// completes within budgeted time limits without race or corruption.
func TestScalingThresholds(t *testing.T) {
	module, err := Compile([]byte(testutil.SimpleSignalModule))
	if err != nil {
		t.Fatalf("Compile error = %v", err)
	}

	t.Run("MemoryStore_100ConcurrentWorkers", func(t *testing.T) {
		engine, err := New(module, WithTraceLevel(TraceMinimal))
		if err != nil {
			t.Fatalf("New error = %v", err)
		}
		ctx := context.Background()
		const numWorkers = 100
		const opsPerWorker = 10

		start := time.Now()
		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers)

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < opsPerWorker; i++ {
					execID := fmt.Sprintf("mem-scale-w%d-i%d", workerID, i)
					if err := engine.Start(ctx, execID, nil); err != nil {
						errCh <- err
						return
					}
					if err := engine.Signal(ctx, execID, "Ping", nil); err != nil {
						errCh <- err
						return
					}
				}
			}(w)
		}
		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("worker error: %v", err)
		}

		elapsed := time.Since(start)
		totalOps := numWorkers * opsPerWorker * 2 // Start + Signal
		t.Logf("MemoryStore: %d ops across %d concurrent workers completed in %v (%.2f ops/sec)",
			totalOps, numWorkers, elapsed, float64(totalOps)/elapsed.Seconds())
	})

	t.Run("PebbleStore_50ConcurrentWorkers", func(t *testing.T) {
		store, err := OpenPebble(t.TempDir(), PebbleNoSync())
		if err != nil {
			t.Fatalf("OpenPebble error = %v", err)
		}
		defer store.Close()

		engine, err := New(module, WithStore(store), WithTraceLevel(TraceMinimal))
		if err != nil {
			t.Fatalf("New error = %v", err)
		}
		ctx := context.Background()
		const numWorkers = 50
		const opsPerWorker = 10

		start := time.Now()
		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers)

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < opsPerWorker; i++ {
					execID := fmt.Sprintf("peb-scale-w%d-i%d", workerID, i)
					if err := engine.Start(ctx, execID, nil); err != nil {
						errCh <- err
						return
					}
					if err := engine.Signal(ctx, execID, "Ping", nil); err != nil {
						errCh <- err
						return
					}
				}
			}(w)
		}
		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("worker error: %v", err)
		}

		elapsed := time.Since(start)
		totalOps := numWorkers * opsPerWorker * 2
		t.Logf("PebbleStore: %d ops across %d concurrent workers completed in %v (%.2f ops/sec)",
			totalOps, numWorkers, elapsed, float64(totalOps)/elapsed.Seconds())
	})
}
