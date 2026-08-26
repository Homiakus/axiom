package pebble_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
	"github.com/Homiakus/axiom/internal/store/pebble"
	pebbledb "github.com/cockroachdb/pebble"
)

// 1. Crash & Recovery Chaos: uncommitted transactions must leave zero trace on reopen.
func TestChaos_CrashUncommittedTransactionRecovery(t *testing.T) {
	dir := t.TempDir()

	store, err := pebble.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	ctx := context.Background()

	// Initial clean execution
	initialExec := &runtime.Execution{
		ID:        "crash-exec-1",
		Domain:    "ChaosDomain",
		Status:    runtime.StatusRunning,
		Version:   1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(ctx, initialExec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}

	// Begin transaction and stage multiple mutations, then "crash" (close store without commit or rollback)
	tx, err := store.BeginTransaction(ctx)
	if err != nil {
		t.Fatalf("BeginTransaction failed: %v", err)
	}

	mutatedExec := &runtime.Execution{
		ID:        "crash-exec-1",
		Domain:    "ChaosDomain",
		Status:    runtime.StatusCompleted,
		Version:   2,
		CreatedAt: initialExec.CreatedAt,
		UpdatedAt: time.Now().UTC(),
	}
	if err := tx.SaveExecution(ctx, mutatedExec); err != nil {
		t.Fatalf("tx.SaveExecution failed: %v", err)
	}

	if err := tx.AppendHistory(ctx, "crash-exec-1", "StepCompleted", map[string]any{"step": 1}); err != nil {
		t.Fatalf("tx.AppendHistory failed: %v", err)
	}

	if err := tx.EnqueueTask(ctx, &runtime.ActivityTask{
		ID:           "task-uncommitted",
		ExecutionID:  "crash-exec-1",
		ActivityName: "ChaosAct",
		Status:       runtime.TaskPending,
	}); err != nil {
		t.Fatalf("tx.EnqueueTask failed: %v", err)
	}

	// Simulate sudden process termination / crash by closing the store handle without committing tx
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen store from disk and verify strict crash invariance
	reopened, err := pebble.Open(dir)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer reopened.Close()

	recoveredExec, err := reopened.GetExecution(ctx, "crash-exec-1")
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}

	if recoveredExec.Version != 1 {
		t.Errorf("expected Version 1, got %d (uncommitted transaction leaked)", recoveredExec.Version)
	}
	if recoveredExec.Status != runtime.StatusRunning {
		t.Errorf("expected Status Running, got %s", recoveredExec.Status)
	}

	history, err := reopened.ListHistory(ctx, "crash-exec-1")
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected 0 history entries, got %d", len(history))
	}

	tasks, err := reopened.ListTasks(ctx, "crash-exec-1")
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

// 2. High Concurrency & Contention Stress: multiple workers on shared and independent executions.
func TestChaos_HighConcurrencyContentionPebbleStore(t *testing.T) {
	dir := t.TempDir()

	store, err := pebble.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	const (
		numWorkers    = 50
		opsPerWorker  = 30
		sharedExecKey = "shared-contention-exec"
	)

	ctx := context.Background()

	// Seed shared execution
	if err := store.CreateExecution(ctx, &runtime.Execution{
		ID:        sharedExecKey,
		Domain:    "StressDomain",
		Status:    runtime.StatusRunning,
		Version:   1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateExecution shared failed: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*opsPerWorker)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			for op := 0; op < opsPerWorker; op++ {
				// 50% independent execution, 50% shared execution
				var execID string
				if op%2 == 0 {
					execID = fmt.Sprintf("indep-worker-%d-op-%d", workerID, op)
				} else {
					execID = sharedExecKey
				}

				tx, err := store.BeginTransaction(ctx)
				if err != nil {
					errCh <- fmt.Errorf("worker %d BeginTransaction: %w", workerID, err)
					return
				}

				exec, err := tx.GetExecution(ctx, execID)
				if err != nil {
					if errors.Is(err, runtime.ErrExecutionNotFound) {
						exec = &runtime.Execution{
							ID:        execID,
							Domain:    "StressDomain",
							Status:    runtime.StatusRunning,
							Version:   1,
							CreatedAt: time.Now().UTC(),
							UpdatedAt: time.Now().UTC(),
						}
						if err := tx.CreateExecution(ctx, exec); err != nil {
							_ = tx.Rollback()
							errCh <- fmt.Errorf("worker %d CreateExecution: %w", workerID, err)
							return
						}
					} else {
						_ = tx.Rollback()
						errCh <- fmt.Errorf("worker %d GetExecution: %w", workerID, err)
						return
					}
				} else {
					exec.Version++
					exec.UpdatedAt = time.Now().UTC()
					if err := tx.SaveExecution(ctx, exec); err != nil {
						_ = tx.Rollback()
						errCh <- fmt.Errorf("worker %d SaveExecution: %w", workerID, err)
						return
					}
				}

				if err := tx.AppendHistory(ctx, execID, "WorkerTick", map[string]any{
					"worker": workerID,
					"op":     op,
				}); err != nil {
					_ = tx.Rollback()
					errCh <- fmt.Errorf("worker %d AppendHistory: %w", workerID, err)
					return
				}

				if err := tx.Commit(); err != nil {
					errCh <- fmt.Errorf("worker %d Commit: %w", workerID, err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrency error: %v", err)
		}
	}

	// Verify shared execution history sequence monotonicity
	history, err := store.ListHistory(ctx, sharedExecKey)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}

	for i := 0; i < len(history); i++ {
		expectedSeq := i + 1
		if history[i].Seq != expectedSeq {
			t.Fatalf("history sequence gap/duplicate: index %d has Seq %d, want %d", i, history[i].Seq, expectedSeq)
		}
	}
}

// 3. Poison Pills & Malformed Data Handling: corrupt records fail closed without silent wipe.
func TestChaos_PoisonPillPebbleStoreFailClosed(t *testing.T) {
	dir := t.TempDir()

	store, err := pebble.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	ctx := context.Background()
	exec := &runtime.Execution{
		ID:        "poison-target",
		Domain:    "ValidDomain",
		Status:    runtime.StatusRunning,
		Version:   1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 1. Inject malformed non-JSON data into an execution record key
	rawDB, err := pebbledb.Open(dir, &pebbledb.Options{})
	if err != nil {
		t.Fatalf("raw pebbledb.Open failed: %v", err)
	}
	// Overwrite the execution key with non-JSON corrupted bytes
	if err := rawDB.Set([]byte("exec/poison-target"), []byte("POISON_PILL_NOT_JSON_BYTE_SEQUENCE{[[}"), pebbledb.Sync); err != nil {
		t.Fatalf("rawDB.Set failed: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("rawDB.Close failed: %v", err)
	}

	// Reopen store through standard Axiom Pebble driver
	reopenedStore, err := pebble.Open(dir)
	if err != nil {
		t.Fatalf("Pebble Open failed: %v", err)
	}

	// Reading corrupted execution must fail closed with decode error, and not return empty struct
	_, getErr := reopenedStore.GetExecution(ctx, "poison-target")
	if getErr == nil {
		_ = reopenedStore.Close()
		t.Fatal("expected decode error for malformed execution data")
	}
	_ = reopenedStore.Close()

	// 2. Inject unsupported future schema version marker
	rawDB2, err := pebbledb.Open(dir, &pebbledb.Options{})
	if err != nil {
		t.Fatalf("rawDB2.Open failed: %v", err)
	}
	if err := rawDB2.Set([]byte("meta/axiom-store-schema"), []byte("9999"), pebbledb.Sync); err != nil {
		t.Fatalf("rawDB2.Set failed: %v", err)
	}
	if err := rawDB2.Close(); err != nil {
		t.Fatalf("rawDB2.Close failed: %v", err)
	}

	// Reopening store with future schema version must fail closed at Open time
	futureStore, futureSchemaErr := pebble.Open(dir)
	if futureStore != nil {
		_ = futureStore.Close()
	}
	if futureSchemaErr == nil || !strings.Contains(futureSchemaErr.Error(), "unsupported store schema") {
		t.Fatalf("expected unsupported schema error, got %v", futureSchemaErr)
	}
}

// 4. Context Cancellation Boundary: pre-canceled context returns ctx.Err() fast.
func TestChaos_ContextCancellationPebbleStore(t *testing.T) {
	dir := t.TempDir()
	store, err := pebble.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	exec := &runtime.Execution{
		ID:      "canceled-exec",
		Domain:  "CancelDomain",
		Version: 1,
	}

	if err := store.SaveExecution(canceledCtx, exec); !errors.Is(err, context.Canceled) {
		t.Errorf("SaveExecution(canceled) err = %v; want %v", err, context.Canceled)
	}

	if _, err := store.GetExecution(canceledCtx, "canceled-exec"); !errors.Is(err, context.Canceled) {
		t.Errorf("GetExecution(canceled) err = %v; want %v", err, context.Canceled)
	}

	if _, err := store.BeginTransaction(canceledCtx); !errors.Is(err, context.Canceled) {
		t.Errorf("BeginTransaction(canceled) err = %v; want %v", err, context.Canceled)
	}
}
