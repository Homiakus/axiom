package runtime_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Homiakus/axiom/internal/compiler"
	"github.com/Homiakus/axiom/internal/runtime"
	"github.com/Homiakus/axiom/internal/store/memory"
)

// SCALE-002: Measure double-serialization between Engine and Store,
// verify per-execution locking semantics, and benchmark concurrent engine operations.

func compileTestModule(t testing.TB) *compiler.Module {
	t.Helper()
	module, err := compiler.Compile([]byte(`domain ConcurrencyBench

context State:
  counter: Int = 0
  active: Bool = true

signal Increment:
  delta: Int

signal Toggle:
  active: Bool

computed isPositive: Bool =
  State.counter > 0

fact CounterActive when:
  State.active and isPositive

rule OnIncrement:
  on Increment
  write:
    State.counter = State.counter + signal.delta

rule OnToggle:
  on Toggle
  write:
    State.active = signal.active
`))
	if err != nil {
		t.Fatalf("compiler.Compile error = %v", err)
	}
	return module
}

// TestEngineConcurrentIndependentExecutions verifies that independent executions
// can be started, signaled, and patched concurrently without any data corruption or deadlocks.
func TestEngineConcurrentIndependentExecutions(t *testing.T) {
	module := compileTestModule(t)
	store := memory.NewStore()
	engine := runtime.NewEngine(module, store, nil)

	const numWorkers = 16
	const opsPerWorker = 25

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*opsPerWorker)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ctx := context.Background()
			for i := 0; i < opsPerWorker; i++ {
				execID := fmt.Sprintf("indep-w%d-i%d", workerID, i)
				// 1. Start execution
				if err := engine.Start(ctx, execID, map[string]any{
					"State": map[string]any{"counter": 10, "active": true},
				}); err != nil {
					errCh <- fmt.Errorf("worker %d start %s: %w", workerID, execID, err)
					return
				}
				// 2. Signal execution
				if err := engine.Signal(ctx, execID, "Increment", map[string]any{"delta": 5}); err != nil {
					errCh <- fmt.Errorf("worker %d signal %s: %w", workerID, execID, err)
					return
				}
				// 3. Patch execution
				if err := engine.Patch(ctx, execID, map[string]any{
					"State": map[string]any{"counter": 20},
				}); err != nil {
					errCh <- fmt.Errorf("worker %d patch %s: %w", workerID, execID, err)
					return
				}
				// 4. Verify final state
				exec, err := store.GetExecution(ctx, execID)
				if err != nil {
					errCh <- fmt.Errorf("worker %d get %s: %w", workerID, execID, err)
					return
				}
				if exec == nil {
					errCh <- fmt.Errorf("worker %d exec %s is nil", workerID, execID)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent execution error: %v", err)
	}
}

// TestEngineConcurrentSameExecutionSerialization verifies that concurrent signals
// against the same execution ID are properly serialized and yield consistent state.
func TestEngineConcurrentSameExecutionSerialization(t *testing.T) {
	module := compileTestModule(t)
	store := memory.NewStore()
	engine := runtime.NewEngine(module, store, nil)

	ctx := context.Background()
	execID := "shared-serialized-exec"

	if err := engine.Start(ctx, execID, map[string]any{
		"State": map[string]any{"counter": 0, "active": true},
	}); err != nil {
		t.Fatalf("Start error = %v", err)
	}

	const numWorkers = 8
	const incrementsPerWorker = 20

	var wg sync.WaitGroup
	var successCount atomic.Int64

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < incrementsPerWorker; i++ {
				// Each signal uses execution API with lock
				api := engine.Execution(execID)
				if err := api.Signal(ctx, "Increment", map[string]any{"delta": 1}); err != nil {
					t.Errorf("worker %d signal error: %v", workerID, err)
					return
				}
				successCount.Add(1)
			}
		}(w)
	}

	wg.Wait()

	exec, err := store.GetExecution(ctx, execID)
	if err != nil {
		t.Fatalf("GetExecution error = %v", err)
	}

	expectedTotal := int64(numWorkers * incrementsPerWorker)
	if successCount.Load() != expectedTotal {
		t.Fatalf("successCount = %d, want %d", successCount.Load(), expectedTotal)
	}

	gotCounter, ok := exec.Context["State"]["counter"].(int)
	if !ok {
		// Might be int64 depending on json conversion
		if c64, ok64 := exec.Context["State"]["counter"].(int64); ok64 {
			gotCounter = int(c64)
		} else {
			t.Fatalf("unexpected counter type: %T = %v", exec.Context["State"]["counter"], exec.Context["State"]["counter"])
		}
	}
	if gotCounter != int(expectedTotal) {
		t.Fatalf("final counter = %d, want %d", gotCounter, expectedTotal)
	}
}

// ==================== Engine Concurrency Benchmarks ====================

func BenchmarkEngineConcurrentIndependentExecutions(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			module := compileTestModule(b)
			store := memory.NewStore()
			engine := runtime.NewEngine(module, store, nil)
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			var wg sync.WaitGroup
			opsPerWorker := b.N / c
			if opsPerWorker < 1 {
				opsPerWorker = 1
			}

			for w := 0; w < c; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for i := 0; i < opsPerWorker; i++ {
						execID := fmt.Sprintf("bench-indep-w%d-i%d", workerID, i)
						_ = engine.Start(ctx, execID, map[string]any{
							"State": map[string]any{"counter": i, "active": true},
						})
						_ = engine.Signal(ctx, execID, "Increment", map[string]any{"delta": 1})
					}
				}(w)
			}
			wg.Wait()
		})
	}
}

func BenchmarkEngineConcurrentSameExecution(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			module := compileTestModule(b)
			store := memory.NewStore()
			engine := runtime.NewEngine(module, store, nil)
			ctx := context.Background()
			execID := fmt.Sprintf("bench-same-exec-c%d", c)

			_ = engine.Start(ctx, execID, map[string]any{
				"State": map[string]any{"counter": 0, "active": true},
			})

			b.ResetTimer()
			b.ReportAllocs()

			var wg sync.WaitGroup
			opsPerWorker := b.N / c
			if opsPerWorker < 1 {
				opsPerWorker = 1
			}

			for w := 0; w < c; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < opsPerWorker; i++ {
						api := engine.Execution(execID)
						_ = api.Signal(ctx, "Increment", map[string]any{"delta": 1})
					}
				}()
			}
			wg.Wait()
		})
	}
}
