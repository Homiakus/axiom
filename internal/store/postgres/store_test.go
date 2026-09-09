package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
	"github.com/Homiakus/axiom/internal/testutil"
)

func TestPostgresStoreContract(t *testing.T) {
	testutil.RunStoreContract(t, func(t *testing.T) runtime.Store {
		dbName := fmt.Sprintf("test_db_%d", time.Now().UnixNano())
		ResetSimDB(dbName)
		store, err := Open("pgsim", dbName)
		if err != nil {
			t.Fatalf("open pgsim store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	})
}

func TestPostgresStore_ConcurrentCAS(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("test_cas_%d", time.Now().UnixNano())
	ResetSimDB(dbName)
	store, err := Open("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	exec := &runtime.Execution{
		ID:        "exec-cas-1",
		Status:    runtime.StatusRunning,
		Context:   map[string]map[string]any{"Data": {"count": 0}},
		Computed:  map[string]any{},
		Facts:     map[string]runtime.FactValue{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}

	const workers = 10
	var wg sync.WaitGroup
	var successfulUpdates atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for attempt := 0; attempt < 5; attempt++ {
				cur, err := store.GetExecution(ctx, "exec-cas-1")
				if err != nil {
					continue
				}
				cur.Context["Data"]["count"] = workerID*100 + attempt
				if err := store.SaveExecution(ctx, cur); err == nil {
					successfulUpdates.Add(1)
					break
				}
			}
		}(i)
	}
	wg.Wait()

	if successfulUpdates.Load() == 0 {
		t.Fatalf("expected at least 1 successful update under contention")
	}

	final, err := store.GetExecution(ctx, "exec-cas-1")
	if err != nil {
		t.Fatal(err)
	}
	if final.Version <= 1 {
		t.Fatalf("expected version > 1 after concurrent updates, got %d", final.Version)
	}
}

func TestPostgresStore_WorkerFencing_SkipLocked(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("test_fence_%d", time.Now().UnixNano())
	ResetSimDB(dbName)
	store, err := Open("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	execID := "exec-fence"
	exec := &runtime.Execution{
		ID:        execID,
		Status:    runtime.StatusRunning,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}

	task := &runtime.ActivityTask{
		ID:           "task-1",
		ExecutionID:  execID,
		RuleName:     "Deploy",
		ActivityName: "Deploy",
		Attempt:      1,
		Status:       runtime.TaskPending,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := store.EnqueueTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	// Worker A claims task with short lease
	claimedA, err := store.PollTaskWithLease(ctx, execID, "worker-A", 50*time.Millisecond)
	if err != nil || claimedA == nil {
		t.Fatalf("worker A claim failed: %v", err)
	}

	// Worker B attempts to claim immediately -> should get nil (still locked / running)
	claimedB, err := store.PollTaskWithLease(ctx, execID, "worker-B", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimedB != nil {
		t.Fatalf("worker B acquired running task prematurely: %+v", claimedB)
	}

	// Wait for lease to expire
	time.Sleep(60 * time.Millisecond)

	// Worker B claims expired task
	claimedB2, err := store.PollTaskWithLease(ctx, execID, "worker-B", time.Minute)
	if err != nil || claimedB2 == nil {
		t.Fatalf("worker B failed to claim expired task: %v", err)
	}
	if claimedB2.LockedBy != "worker-B" {
		t.Fatalf("expected worker B, got %s", claimedB2.LockedBy)
	}

	// Stale worker A tries to heartbeat -> must be rejected
	if err := store.HeartbeatTask(ctx, "task-1", "worker-A"); err == nil {
		t.Fatalf("expected heartbeat error for stale worker A, got nil")
	}

	// Active worker B heartbeats -> succeeds
	if err := store.HeartbeatTask(ctx, "task-1", "worker-B"); err != nil {
		t.Fatalf("worker B heartbeat failed: %v", err)
	}
}

func TestPostgresStore_LivePostgresIfAvailable(t *testing.T) {
	pgURL := os.Getenv("POSTGRES_TEST_URL")
	if pgURL == "" {
		t.Skip("skipping live postgres test (POSTGRES_TEST_URL not set)")
	}
	db, err := sql.Open("pgx", pgURL)
	if err != nil {
		t.Skipf("cannot open POSTGRES_TEST_URL: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("cannot ping POSTGRES_TEST_URL: %v", err)
	}
	defer db.Close()

	store, err := OpenFromDB(db)
	if err != nil {
		t.Fatalf("OpenFromDB failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	execID := fmt.Sprintf("live-pg-%d", time.Now().UnixNano())
	exec := &runtime.Execution{
		ID:        execID,
		Status:    runtime.StatusRunning,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("live PG CreateExecution failed: %v", err)
	}
	loaded, err := store.GetExecution(ctx, execID)
	if err != nil {
		t.Fatalf("live PG GetExecution failed: %v", err)
	}
	if loaded.ID != execID {
		t.Fatalf("loaded ID mismatch: got %s want %s", loaded.ID, execID)
	}
}

func TestPostgresStore_TransactionRollback(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("test_rollback_%d", time.Now().UnixNano())
	ResetSimDB(dbName)
	store, err := Open("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	exec := &runtime.Execution{
		ID:        "exec-rollback",
		Version:   1,
		Status:    runtime.StatusRunning,
		Context:   map[string]map[string]any{"Data": {"key": "original"}},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}

	// Begin manual transaction that modifies then rolls back
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, "UPDATE axiom_executions SET status = $1 WHERE id = $2", "failed", "exec-rollback")
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	// Explicit rollback
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// State must be unmutated
	cur, err := store.GetExecution(ctx, "exec-rollback")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Status != runtime.StatusRunning {
		t.Fatalf("expected status running after rollback, got %s", cur.Status)
	}
	if cur.Context["Data"]["key"] != "original" {
		t.Fatalf("expected original value, got %v", cur.Context["Data"]["key"])
	}
}

func TestPostgresStore_MigrationIdempotency(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("test_mig_%d", time.Now().UnixNano())
	ResetSimDB(dbName)

	db, err := sql.Open("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// First migration run
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	// Second migration run (idempotency check)
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second migration run must be idempotent, got: %v", err)
	}

	// Verify migrations recorded in axiom_schema_migrations
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM axiom_schema_migrations").Scan(&count); err != nil {
		t.Fatalf("failed to query migrations count: %v", err)
	}
	if count != len(Migrations) {
		t.Fatalf("expected %d migrations recorded, got %d", len(Migrations), count)
	}
}

func TestPostgresStore_FaultInjectionAroundCommit(t *testing.T) {
	dbName := fmt.Sprintf("test_fault_%d", time.Now().UnixNano())
	ResetSimDB(dbName)
	store, err := Open("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	exec := &runtime.Execution{
		ID:        "exec-fault-1",
		Version:   1,
		Status:    runtime.StatusRunning,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(context.Background(), exec); err != nil {
		t.Fatal(err)
	}

	// Cancel context before save
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	exec.Status = runtime.StatusCompleted
	if err := store.SaveExecution(canceledCtx, exec); err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}

	// Verify state wasn't mutated
	cur, err := store.GetExecution(context.Background(), "exec-fault-1")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Status != runtime.StatusRunning {
		t.Fatalf("expected status running, got %s", cur.Status)
	}
}

