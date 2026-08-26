package adgo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// SCALE-005: Benchmark ADGO PebbleStore concurrent operations and high-contention scenarios.

func openBenchPebbleStore(b *testing.B) *PebbleStore {
	b.Helper()
	store, err := OpenPebbleStore(b.TempDir(), WithPebbleNoSync())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	return store
}

func BenchmarkADGOPebbleStoreCreateConcurrent(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16, 32} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchPebbleStore(b)
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
						execID := fmt.Sprintf("adgo-create-w%d-i%d", workerID, i)
						exec := &Execution{
							ID:        execID,
							PlanID:    "bench-plan",
							Status:    StatusRunning,
							Data:      map[string]json.RawMessage{"counter": json.RawMessage(`0`)},
							CreatedAt: time.Now().UTC(),
							UpdatedAt: time.Now().UTC(),
						}
						if err := store.Create(ctx, exec); err != nil {
							b.Error(err)
							return
						}
					}
				}(w)
			}
			wg.Wait()
		})
	}
}

func BenchmarkADGOPebbleStoreCommitConcurrent(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16, 32} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchPebbleStore(b)
			ctx := context.Background()

			opsPerWorker := b.N / c
			if opsPerWorker < 1 {
				opsPerWorker = 1
			}

			for w := 0; w < c; w++ {
				execID := fmt.Sprintf("adgo-commit-target-w%d", w)
				exec := &Execution{
					ID:        execID,
					PlanID:    "bench-plan",
					Status:    StatusRunning,
					Data:      map[string]json.RawMessage{"counter": json.RawMessage(`0`)},
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				}
				if err := store.Create(ctx, exec); err != nil {
					b.Fatal(err)
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			var wg sync.WaitGroup
			for w := 0; w < c; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					execID := fmt.Sprintf("adgo-commit-target-w%d", workerID)
					var version uint64 = 0
					for i := 0; i < opsPerWorker; i++ {
						res, err := store.Commit(ctx, execID, version, func(e *Execution) error {
							e.Data["counter"] = json.RawMessage(fmt.Sprintf("%d", i+1))
							return nil
						})
						if err != nil {
							b.Error(err)
							return
						}
						version = res.Version
					}
				}(w)
			}
			wg.Wait()
		})
	}
}

func BenchmarkADGOPebbleStoreInboxConcurrent(b *testing.B) {
	for _, c := range []int{1, 2, 4, 8, 16, 32} {
		c := c
		b.Run(fmt.Sprintf("concurrency=%d", c), func(b *testing.B) {
			store := openBenchPebbleStore(b)
			ctx := context.Background()

			opsPerWorker := b.N / c
			if opsPerWorker < 1 {
				opsPerWorker = 1
			}

			for w := 0; w < c; w++ {
				execID := fmt.Sprintf("adgo-inbox-target-w%d", w)
				exec := &Execution{
					ID:        execID,
					PlanID:    "bench-plan",
					Status:    StatusRunning,
					Data:      map[string]json.RawMessage{"counter": json.RawMessage(`0`)},
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				}
				if err := store.Create(ctx, exec); err != nil {
					b.Fatal(err)
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			var wg sync.WaitGroup
			for w := 0; w < c; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					execID := fmt.Sprintf("adgo-inbox-target-w%d", workerID)
					for i := 0; i < opsPerWorker; i++ {
						event := Event{
							ID:      fmt.Sprintf("evt-w%d-i%d", workerID, i),
							Type:    "Step",
							Payload: json.RawMessage(`{"i": 1}`),
							At:      time.Now().UTC(),
						}
						if err := store.PutInbox(ctx, execID, event); err != nil {
							b.Error(err)
							return
						}
					}
				}(w)
			}
			wg.Wait()
		})
	}
}
