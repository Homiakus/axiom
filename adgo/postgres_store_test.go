package adgo

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corepg "github.com/Homiakus/axiom/internal/store/postgres"
)

func TestPostgresStore_ADGOConformanceSuite(t *testing.T) {
	RunADGOStoreConformanceSuite(t, func(t *testing.T) (Store, func()) {
		dbName := fmt.Sprintf("adgo_pg_conf_%d", time.Now().UnixNano())
		corepg.ResetSimDB(dbName)
		store, err := OpenPostgresStore("pgsim", dbName)
		if err != nil {
			t.Fatalf("OpenPostgresStore failed: %v", err)
		}
		return store, func() {
			_ = store.Close()
		}
	})
}

func TestPostgresStore_CatalogAndPruning(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("adgo_pg_cat_%d", time.Now().UnixNano())
	corepg.ResetSimDB(dbName)
	store, err := OpenPostgresStore("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create three executions
	for _, id := range []string{"exec-c", "exec-a", "exec-b"} {
		if err := store.Create(ctx, newTestExecution(id, 1)); err != nil {
			t.Fatalf("Create %s failed: %v", id, err)
		}
	}

	// List catalog -> must be sorted
	ids, err := store.ListExecutionIDs(ctx)
	if err != nil {
		t.Fatalf("ListExecutionIDs failed: %v", err)
	}
	if len(ids) != 3 || ids[0] != "exec-a" || ids[1] != "exec-b" || ids[2] != "exec-c" {
		t.Fatalf("unexpected catalog IDs: %v", ids)
	}

	// Delete execution "exec-b"
	if err := store.DeleteExecution(ctx, "exec-b"); err != nil {
		t.Fatalf("DeleteExecution failed: %v", err)
	}

	// Verify catalog reflects deletion
	ids2, err := store.ListExecutionIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids2) != 2 || ids2[0] != "exec-a" || ids2[1] != "exec-c" {
		t.Fatalf("catalog after deletion mismatch: %v", ids2)
	}

	// Deleted execution cannot be loaded
	if _, err := store.Load(ctx, "exec-b"); err != ErrExecutionNotFound {
		t.Fatalf("expected ErrExecutionNotFound for pruned execution, got %v", err)
	}
}

func TestPostgresStore_Versions(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("adgo_pg_ver_%d", time.Now().UnixNano())
	corepg.ResetSimDB(dbName)
	store, err := OpenPostgresStore("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	exec := newTestExecution("exec-version-test", 1)
	if err := store.Create(ctx, exec); err != nil {
		t.Fatal(err)
	}

	// Commit 3 updates
	for i := 1; i <= 3; i++ {
		_, err := store.Commit(ctx, "exec-version-test", uint64(i), func(e *Execution) error {
			e.Status = StatusRunning
			return nil
		})
		if err != nil {
			t.Fatalf("Commit %d failed: %v", i, err)
		}
	}

	// ListVersions -> must return version 1, 2, 3, 4 in ascending order
	versions, err := store.ListVersions(ctx, "exec-version-test")
	if err != nil {
		t.Fatalf("ListVersions failed: %v", err)
	}
	if len(versions) != 4 {
		t.Fatalf("expected 4 version snapshots, got %d", len(versions))
	}
	for i, v := range versions {
		if v.Version != uint64(i+1) {
			t.Errorf("version[%d] = %d; want %d", i, v.Version, i+1)
		}
	}
}

func TestPostgresProviderHealthStore(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("adgo_pg_health_%d", time.Now().UnixNano())
	corepg.ResetSimDB(dbName)
	store, err := OpenPostgresStore("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	hStore := NewPostgresProviderHealthStore(store.DB())

	// Initially missing
	if _, err := hStore.LoadProviderHealth(ctx, "llm", "openai"); err != ErrProviderHealthNotFound {
		t.Fatalf("expected ErrProviderHealthNotFound, got %v", err)
	}

	// Update creates record
	upd, err := hStore.UpdateProviderHealth(ctx, "llm", "openai", func(h *ProviderHealth) {
		h.EWMAQuality = 0.99
		h.ConsecutiveFailures = 0
	})
	if err != nil {
		t.Fatalf("UpdateProviderHealth failed: %v", err)
	}
	if upd.EWMAQuality != 0.99 {
		t.Fatalf("unexpected EWMAQuality: %v", upd.EWMAQuality)
	}

	// Load updated
	loaded, err := hStore.LoadProviderHealth(ctx, "llm", "openai")
	if err != nil {
		t.Fatalf("LoadProviderHealth failed: %v", err)
	}
	if loaded.EWMAQuality != 0.99 {
		t.Fatalf("loaded EWMAQuality mismatch: %v", loaded.EWMAQuality)
	}

	// Delete
	_ = hStore.DeleteProviderHealth(ctx, "llm", "openai")
	if _, err := hStore.LoadProviderHealth(ctx, "llm", "openai"); err != ErrProviderHealthNotFound {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestPostgresScheduleStore(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("adgo_pg_sched_%d", time.Now().UnixNano())
	corepg.ResetSimDB(dbName)
	store, err := OpenPostgresStore("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sStore := NewPostgresScheduleStore(store.DB())

	sched := &Schedule{
		ID:         "sched-1",
		Version:    1,
		PlanDigest: "backup-digest-123",
		Every:      time.Hour,
		Enabled:    true,
	}
	if err := sStore.Create(ctx, sched); err != nil {
		t.Fatalf("Schedule Create failed: %v", err)
	}

	loaded, err := sStore.Load(ctx, "sched-1")
	if err != nil {
		t.Fatalf("Schedule Load failed: %v", err)
	}
	if loaded.PlanDigest != "backup-digest-123" {
		t.Fatalf("unexpected PlanDigest: %s", loaded.PlanDigest)
	}

	// Commit CAS
	committed, err := sStore.Commit(ctx, "sched-1", 1, func(s *Schedule) error {
		s.Enabled = false
		return nil
	})
	if err != nil {
		t.Fatalf("Schedule Commit failed: %v", err)
	}
	if committed.Version != 2 || committed.Enabled {
		t.Fatalf("unexpected committed schedule: %+v", committed)
	}

	// Stale commit fails with ErrConflict
	if _, err := sStore.Commit(ctx, "sched-1", 1, func(s *Schedule) error {
		return nil
	}); err != ErrConflict {
		t.Fatalf("expected ErrConflict on stale schedule commit, got %v", err)
	}
}

func TestPostgresStore_ConcurrentCommits(t *testing.T) {
	ctx := context.Background()
	dbName := fmt.Sprintf("adgo_pg_contention_%d", time.Now().UnixNano())
	corepg.ResetSimDB(dbName)
	store, err := OpenPostgresStore("pgsim", dbName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	exec := newTestExecution("contention-exec-1", 1)
	if err := store.Create(ctx, exec); err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var wg sync.WaitGroup
	var successCount atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for attempt := 0; attempt < 10; attempt++ {
				cur, err := store.Load(ctx, "contention-exec-1")
				if err != nil {
					continue
				}
				_, err = store.Commit(ctx, "contention-exec-1", cur.Version, func(e *Execution) error {
					e.Status = StatusRunning
					return nil
				})
				if err == nil {
					successCount.Add(1)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if successCount.Load() == 0 {
		t.Fatalf("expected at least 1 successful commit under contention")
	}

	final, err := store.Load(ctx, "contention-exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if final.Version <= 1 {
		t.Fatalf("expected version > 1 after concurrent commits, got %d", final.Version)
	}
}
