package pebble

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	runtimepkg "github.com/Homiakus/axiom/internal/runtime"
)

// SCALE-001: Benchmark Pebble transaction contention under varying concurrency.
//
// The Store.BeginTransaction method holds s.mu for the entire transaction
// lifetime (until Commit/Rollback). These benchmarks quantify the throughput
// and latency impact of that global serialization at different concurrency
// levels.

// ---------- helpers ----------

func openBenchStore(b *testing.B) *Store {
	b.Helper()
	store, err := Open(b.TempDir(), WithNoSync())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	return store
}

func seedExecution(b *testing.B, store *Store, id string) {
	b.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	exec := &runtimepkg.Execution{
		ID:        id,
		Domain:    "bench",
		Status:    runtimepkg.StatusStarted,
		Context:   map[string]map[string]any{"State": {"counter": 0}},
		Computed:  map[string]any{},
		Facts:     map[string]runtimepkg.FactValue{},
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateExecution(ctx, exec); err != nil {
		b.Fatal(err)
	}
}

func seedTask(b *testing.B, store *Store, execID, taskID string) {
	b.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	task := &runtimepkg.ActivityTask{
		ID:             taskID,
		ExecutionID:    execID,
		RuleName:       "rule-1",
		ActivityName:   "act-1",
		IdempotencyKey: taskID,
		Status:         runtimepkg.TaskPending,
		Input:          map[string]any{"key": "value"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.EnqueueTask(ctx, task); err != nil {
		b.Fatal(err)
	}
}

// ---------- Write-heavy transaction: create execution + enqueue task + history + commit ----------

func benchWriteHeavyTransaction(b *testing.B, store *Store, concurrency int) {
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

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
				execID := fmt.Sprintf("exec-w%d-i%d", workerID, i)
				now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
				tx, err := store.BeginTransaction(ctx)
				if err != nil {
					b.Error(err)
					return
				}
				exec := &runtimepkg.Execution{
					ID:        execID,
					Domain:    "bench",
					Status:    runtimepkg.StatusStarted,
					Context:   map[string]map[string]any{"State": {"counter": i}},
					Computed:  map[string]any{},
					Facts:     map[string]runtimepkg.FactValue{},
					Version:   1,
					CreatedAt: now,
					UpdatedAt: now,
				}
				if err := tx.CreateExecution(ctx, exec); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				task := &runtimepkg.ActivityTask{
					ID:             fmt.Sprintf("task-w%d-i%d", workerID, i),
					ExecutionID:    execID,
					RuleName:       "rule-1",
					ActivityName:   "act-1",
					IdempotencyKey: fmt.Sprintf("key-w%d-i%d", workerID, i),
					Status:         runtimepkg.TaskPending,
					Input:          map[string]any{"step": i},
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				if err := tx.EnqueueTask(ctx, task); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				if err := tx.AppendHistory(ctx, execID, "started", map[string]any{"step": i}); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				if err := tx.Commit(); err != nil {
					b.Error(err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

// ---------- Read-heavy transaction: get execution + list tasks + list history ----------

func benchReadHeavyTransaction(b *testing.B, store *Store, execID string, concurrency int) {
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	opsPerWorker := b.N / concurrency
	if opsPerWorker < 1 {
		opsPerWorker = 1
	}

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				tx, err := store.BeginTransaction(ctx)
				if err != nil {
					b.Error(err)
					return
				}
				if _, err := tx.GetExecution(ctx, execID); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				if _, err := tx.ListTasks(ctx, execID); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				if _, err := tx.ListHistory(ctx, execID); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				if err := tx.Rollback(); err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// ---------- Mixed: some workers write, some read ----------

func benchMixedTransaction(b *testing.B, store *Store, readExecID string, concurrency int) {
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	opsPerWorker := b.N / concurrency
	if opsPerWorker < 1 {
		opsPerWorker = 1
	}

	readers := concurrency / 2
	if readers < 1 {
		readers = 1
	}
	writers := concurrency - readers

	// Writers: create independent executions
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				execID := fmt.Sprintf("mixed-w%d-i%d", workerID, i)
				now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
				tx, err := store.BeginTransaction(ctx)
				if err != nil {
					b.Error(err)
					return
				}
				exec := &runtimepkg.Execution{
					ID:        execID,
					Domain:    "bench",
					Status:    runtimepkg.StatusStarted,
					Context:   map[string]map[string]any{},
					Computed:  map[string]any{},
					Facts:     map[string]runtimepkg.FactValue{},
					Version:   1,
					CreatedAt: now,
					UpdatedAt: now,
				}
				if err := tx.CreateExecution(ctx, exec); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				if err := tx.Commit(); err != nil {
					b.Error(err)
					return
				}
			}
		}(w)
	}

	// Readers: repeatedly read the pre-seeded execution
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				tx, err := store.BeginTransaction(ctx)
				if err != nil {
					b.Error(err)
					return
				}
				_, _ = tx.GetExecution(ctx, readExecID)
				_, _ = tx.ListTasks(ctx, readExecID)
				_ = tx.Rollback()
			}
		}()
	}
	wg.Wait()
}

// ---------- Same-execution concurrent save (contention worst case) ----------

func benchSameExecutionSave(b *testing.B, store *Store, execID string, concurrency int) {
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

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
				tx, err := store.BeginTransaction(ctx)
				if err != nil {
					b.Error(err)
					return
				}
				exec, err := tx.GetExecution(ctx, execID)
				if err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				exec.Context = map[string]map[string]any{
					"State": {"counter": workerID*1000 + i},
				}
				if err := tx.SaveExecution(ctx, exec); err != nil {
					_ = tx.Rollback()
					b.Error(err)
					return
				}
				if err := tx.Commit(); err != nil {
					b.Error(err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

// ---------- Direct (non-transactional) baseline ----------

func benchDirectWriteBaseline(b *testing.B, store *Store, concurrency int) {
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

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
				execID := fmt.Sprintf("direct-w%d-i%d", workerID, i)
				now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
				exec := &runtimepkg.Execution{
					ID:        execID,
					Domain:    "bench",
					Status:    runtimepkg.StatusStarted,
					Context:   map[string]map[string]any{"State": {"counter": i}},
					Computed:  map[string]any{},
					Facts:     map[string]runtimepkg.FactValue{},
					Version:   1,
					CreatedAt: now,
					UpdatedAt: now,
				}
				if err := store.CreateExecution(ctx, exec); err != nil {
					b.Error(err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

// ==================== Benchmark entry points ====================

// Write-heavy: 1, 2, 4, 8, 16, 32 goroutines
func BenchmarkTransactionWriteHeavy(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16, 32} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchStore(b)
			benchWriteHeavyTransaction(b, store, c)
		})
	}
}

// Read-heavy: 1, 2, 4, 8, 16, 32 goroutines
func BenchmarkTransactionReadHeavy(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16, 32} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchStore(b)
			execID := "read-target"
			seedExecution(b, store, execID)
			for i := 0; i < 5; i++ {
				seedTask(b, store, execID, fmt.Sprintf("task-%d", i))
			}
			ctx := context.Background()
			for i := 0; i < 3; i++ {
				_ = store.AppendHistory(ctx, execID, "event", map[string]any{"i": i})
			}
			benchReadHeavyTransaction(b, store, execID, c)
		})
	}
}

// Mixed read/write: 2, 4, 8, 16, 32 goroutines
func BenchmarkTransactionMixed(b *testing.B) {
	for _, c := range []int{2, 4, 8, 16, 32} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchStore(b)
			execID := "mixed-read-target"
			seedExecution(b, store, execID)
			benchMixedTransaction(b, store, execID, c)
		})
	}
}

// Same-execution save contention: 1, 2, 4, 8, 16 goroutines
func BenchmarkTransactionSameExecution(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchStore(b)
			execID := "shared-exec"
			seedExecution(b, store, execID)
			benchSameExecutionSave(b, store, execID, c)
		})
	}
}

// Direct (non-transactional) write baseline for comparison
func BenchmarkDirectWriteBaseline(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16, 32} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchStore(b)
			benchDirectWriteBaseline(b, store, c)
		})
	}
}

// Commit latency distribution: measure per-transaction latency at different concurrency levels
func BenchmarkTransactionCommitLatency(b *testing.B) {
	for _, c := range []int{1, 4, 16} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchStore(b)
			ctx := context.Background()

			var mu sync.Mutex
			latencies := make([]time.Duration, 0, b.N)

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
					localLatencies := make([]time.Duration, 0, opsPerWorker)
					for i := 0; i < opsPerWorker; i++ {
						execID := fmt.Sprintf("lat-w%d-i%d", workerID, i)
						now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)

						start := time.Now()
						tx, err := store.BeginTransaction(ctx)
						if err != nil {
							b.Error(err)
							return
						}
						exec := &runtimepkg.Execution{
							ID:        execID,
							Domain:    "bench",
							Status:    runtimepkg.StatusStarted,
							Context:   map[string]map[string]any{},
							Computed:  map[string]any{},
							Facts:     map[string]runtimepkg.FactValue{},
							Version:   1,
							CreatedAt: now,
							UpdatedAt: now,
						}
						if err := tx.CreateExecution(ctx, exec); err != nil {
							_ = tx.Rollback()
							b.Error(err)
							return
						}
						if err := tx.Commit(); err != nil {
							b.Error(err)
							return
						}
						localLatencies = append(localLatencies, time.Since(start))
					}
					mu.Lock()
					latencies = append(latencies, localLatencies...)
					mu.Unlock()
				}(w)
			}
			wg.Wait()
			b.StopTimer()

			if len(latencies) > 0 {
				// Sort and report p50/p95/p99
				sortDurations(latencies)
				b.ReportMetric(float64(percentile(latencies, 0.50).Microseconds()), "p50-µs")
				b.ReportMetric(float64(percentile(latencies, 0.95).Microseconds()), "p95-µs")
				b.ReportMetric(float64(percentile(latencies, 0.99).Microseconds()), "p99-µs")
			}
		})
	}
}

// ---------- latency helpers ----------

func sortDurations(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
