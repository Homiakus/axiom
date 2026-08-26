package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

// StoreTester is a helper function that initializes a Store and provides a cleanup callback.
type StoreTester func(t *testing.T) (Store, func())

// RunADGOStoreConformanceSuite verifies that a store backend satisfies the complete
// behavioral contracts required by the ADGO runtime.
func RunADGOStoreConformanceSuite(t *testing.T, tester StoreTester) {
	t.Helper()

	newStore := func(t *testing.T) (Store, func()) {
		t.Helper()
		s, cleanup := tester(t)
		if s == nil {
			t.Fatal("store tester returned nil store")
		}
		return s, cleanup
	}

	t.Run("NotFound_MissingExecution", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		_, err := store.Load(ctx, "nonexistent-execution-id")
		if !errors.Is(err, ErrExecutionNotFound) {
			t.Fatalf("Load(missing) err = %v; want %v", err, ErrExecutionNotFound)
		}

		_, err = store.Commit(ctx, "nonexistent-execution-id", 1, func(e *Execution) error {
			e.Status = StatusCompleted
			return nil
		})
		if !errors.Is(err, ErrExecutionNotFound) {
			t.Fatalf("Commit(missing) err = %v; want %v", err, ErrExecutionNotFound)
		}
	})

	t.Run("Create_ConflictOnExisting", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-conflict-test", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("initial Create failed: %v", err)
		}

		// Second create with identical ID must fail with ErrExecutionExists
		err := store.Create(ctx, exec)
		if !errors.Is(err, ErrExecutionExists) {
			t.Fatalf("Create(existing) err = %v; want %v", err, ErrExecutionExists)
		}
	})

	t.Run("Create_InputIsolation", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-isolation-create", 1)
		exec.Data["k1"] = json.RawMessage(`"original-value"`)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Mutate original caller structure
		exec.Data["k1"] = json.RawMessage(`"mutated-caller-value"`)
		exec.Status = StatusFailed

		// Loaded execution must retain original stored values
		loaded, err := store.Load(ctx, "exec-isolation-create")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if string(loaded.Data["k1"]) != `"original-value"` {
			t.Fatalf("Create leaked caller mutation into store: got %s, want \"original-value\"", string(loaded.Data["k1"]))
		}
		if loaded.Status != StatusRunning {
			t.Fatalf("Create leaked caller status mutation into store: got %s, want %s", loaded.Status, StatusRunning)
		}
	})

	t.Run("Load_ResultIsolation", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-isolation-load", 1)
		exec.Data["k1"] = json.RawMessage(`"original-value"`)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		first, err := store.Load(ctx, "exec-isolation-load")
		if err != nil {
			t.Fatalf("first Load failed: %v", err)
		}
		first.Data["k1"] = json.RawMessage(`"mutated-first-read"`)
		first.Status = StatusCompleted

		second, err := store.Load(ctx, "exec-isolation-load")
		if err != nil {
			t.Fatalf("second Load failed: %v", err)
		}
		if string(second.Data["k1"]) != `"original-value"` {
			t.Fatalf("Load result aliased stored state: got %s, want \"original-value\"", string(second.Data["k1"]))
		}
		if second.Status != StatusRunning {
			t.Fatalf("Load result aliased stored status: got %s, want %s", second.Status, StatusRunning)
		}
	})

	t.Run("Commit_SuccessAndVersionIncrement", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-commit-success", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		updated, err := store.Commit(ctx, "exec-commit-success", 1, func(e *Execution) error {
			e.Status = StatusWaiting
			e.Data["updated"] = json.RawMessage(`true`)
			return nil
		})
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
		if updated.Version != 2 {
			t.Fatalf("Commit returned Version %d; want 2", updated.Version)
		}
		if updated.Status != StatusWaiting {
			t.Fatalf("Commit returned Status %s; want %s", updated.Status, StatusWaiting)
		}

		// Verify stored state matches
		stored, err := store.Load(ctx, "exec-commit-success")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if stored.Version != 2 || stored.Status != StatusWaiting {
			t.Fatalf("stored state version=%d, status=%s; want 2, %s", stored.Version, stored.Status, StatusWaiting)
		}
	})

	t.Run("Commit_VersionConflict", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-commit-conflict", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		// Advance to version 2
		if _, err := store.Commit(ctx, "exec-commit-conflict", 1, func(e *Execution) error {
			e.Status = StatusWaiting
			return nil
		}); err != nil {
			t.Fatalf("first Commit failed: %v", err)
		}

		// Attempt commit with stale expected version 1
		_, err := store.Commit(ctx, "exec-commit-conflict", 1, func(e *Execution) error {
			e.Status = StatusCompleted
			return nil
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("Commit(stale version) err = %v; want %v", err, ErrConflict)
		}

		// Verify state was not modified
		stored, err := store.Load(ctx, "exec-commit-conflict")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if stored.Version != 2 || stored.Status != StatusWaiting {
			t.Fatalf("state mutated after conflict: version=%d, status=%s", stored.Version, stored.Status)
		}
	})

	t.Run("Commit_CallbackErrorRollsBack", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-commit-rollback", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		customErr := errors.New("custom business logic validation error")
		_, err := store.Commit(ctx, "exec-commit-rollback", 1, func(e *Execution) error {
			e.Status = StatusCompleted
			e.Data["bad"] = json.RawMessage(`true`)
			return customErr
		})
		if !errors.Is(err, customErr) {
			t.Fatalf("Commit err = %v; want %v", err, customErr)
		}

		// Verify version is still 1 and status is unchanged
		stored, err := store.Load(ctx, "exec-commit-rollback")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if stored.Version != 1 || stored.Status != StatusRunning {
			t.Fatalf("Commit with error modified state: version=%d, status=%s", stored.Version, stored.Status)
		}
	})

	t.Run("Commit_UnserializableRollsBack", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-commit-unserializable", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		_, err := store.Commit(ctx, "exec-commit-unserializable", 1, func(e *Execution) error {
			e.Quality = map[string]float64{"score": math.NaN()}
			return nil
		})

		// If commit failed due to serialization error, version must remain 1
		stored, loadErr := store.Load(ctx, "exec-commit-unserializable")
		if loadErr != nil && err == nil {
			t.Fatalf("commit succeeded but load failed: %v", loadErr)
		}
		if stored != nil && stored.Version > 1 && err != nil {
			t.Fatalf("commit returned error %v but stored version advanced to %d", err, stored.Version)
		}
	})

	t.Run("Inbox_MissingExecution", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		err := store.PutInbox(ctx, "missing-exec", Event{ID: "ev-1", Type: "Signal"})
		if !errors.Is(err, ErrExecutionNotFound) {
			t.Fatalf("PutInbox(missing) err = %v; want %v", err, ErrExecutionNotFound)
		}
	})

	t.Run("Inbox_EmptyEventID", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-inbox-empty-id", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		err := store.PutInbox(ctx, "exec-inbox-empty-id", Event{ID: "", Type: "Signal"})
		if err == nil {
			t.Fatal("expected error putting event with empty ID")
		}
	})

	t.Run("Inbox_DeduplicationAndOrder", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-inbox-dedup", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		t1 := t0.Add(time.Second)
		t2 := t0.Add(2 * time.Second)

		// Put events in out-of-order sequence
		if err := store.PutInbox(ctx, "exec-inbox-dedup", Event{ID: "ev-2", Type: "Signal", At: t1}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutInbox(ctx, "exec-inbox-dedup", Event{ID: "ev-1", Type: "Signal", At: t0}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutInbox(ctx, "exec-inbox-dedup", Event{ID: "ev-3", Type: "Signal", At: t2}); err != nil {
			t.Fatal(err)
		}

		// Duplicate put of ev-2 should be a no-op / idempotent
		if err := store.PutInbox(ctx, "exec-inbox-dedup", Event{ID: "ev-2", Type: "Signal", At: t1}); err != nil {
			t.Fatal(err)
		}

		inbox, err := store.ListInbox(ctx, "exec-inbox-dedup")
		if err != nil {
			t.Fatalf("ListInbox failed: %v", err)
		}
		if len(inbox) != 3 {
			t.Fatalf("ListInbox count = %d; want 3", len(inbox))
		}
		if inbox[0].ID != "ev-1" || inbox[1].ID != "ev-2" || inbox[2].ID != "ev-3" {
			t.Fatalf("inbox not in deterministic sorted order: %+v", inbox)
		}
	})

	t.Run("Inbox_AckIdempotency", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		ctx := context.Background()

		exec := newTestExecution("exec-inbox-ack", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		if err := store.PutInbox(ctx, "exec-inbox-ack", Event{ID: "ev-1", Type: "Signal", At: t0}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutInbox(ctx, "exec-inbox-ack", Event{ID: "ev-2", Type: "Signal", At: t0.Add(time.Second)}); err != nil {
			t.Fatal(err)
		}

		// Ack ev-1
		if err := store.AckInbox(ctx, "exec-inbox-ack", []string{"ev-1"}); err != nil {
			t.Fatalf("AckInbox failed: %v", err)
		}

		inbox, err := store.ListInbox(ctx, "exec-inbox-ack")
		if err != nil {
			t.Fatal(err)
		}
		if len(inbox) != 1 || inbox[0].ID != "ev-2" {
			t.Fatalf("after first ack, inbox = %+v; want [ev-2]", inbox)
		}

		// Duplicate ack of already-acked ev-1 should succeed idempotently
		if err := store.AckInbox(ctx, "exec-inbox-ack", []string{"ev-1"}); err != nil {
			t.Fatalf("idempotent AckInbox failed: %v", err)
		}

		// Ack remaining ev-2
		if err := store.AckInbox(ctx, "exec-inbox-ack", []string{"ev-2"}); err != nil {
			t.Fatal(err)
		}

		inbox, err = store.ListInbox(ctx, "exec-inbox-ack")
		if err != nil {
			t.Fatal(err)
		}
		if len(inbox) != 0 {
			t.Fatalf("after second ack, inbox not empty: %+v", inbox)
		}
	})

	t.Run("ContextCancellation_OperationsHonorPreCancelledContext", func(t *testing.T) {
		store, cleanup := newStore(t)
		defer cleanup()
		bgCtx := context.Background()

		exec := newTestExecution("exec-ctx-cancel", 1)
		if err := store.Create(bgCtx, exec); err != nil {
			t.Fatalf("setup Create failed: %v", err)
		}

		canceledCtx, cancel := context.WithCancel(bgCtx)
		cancel()

		// 1. Create with canceled context
		newExec := newTestExecution("exec-ctx-cancel-2", 1)
		if err := store.Create(canceledCtx, newExec); !errors.Is(err, context.Canceled) {
			t.Fatalf("Create(canceled) err = %v; want %v", err, context.Canceled)
		}
		if _, err := store.Load(bgCtx, "exec-ctx-cancel-2"); !errors.Is(err, ErrExecutionNotFound) {
			t.Fatalf("Load after canceled Create: err = %v; want %v", err, ErrExecutionNotFound)
		}

		// 2. Load with canceled context
		if _, err := store.Load(canceledCtx, "exec-ctx-cancel"); !errors.Is(err, context.Canceled) {
			t.Fatalf("Load(canceled) err = %v; want %v", err, context.Canceled)
		}

		// 3. Commit with canceled context
		mutateCalled := false
		if _, err := store.Commit(canceledCtx, "exec-ctx-cancel", 1, func(e *Execution) error {
			mutateCalled = true
			return nil
		}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Commit(canceled) err = %v; want %v", err, context.Canceled)
		}
		if mutateCalled {
			t.Fatal("Commit mutate callback was called despite canceled context")
		}
		loaded, err := store.Load(bgCtx, "exec-ctx-cancel")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if loaded.Version != 1 {
			t.Fatalf("version advanced after canceled Commit: %d", loaded.Version)
		}

		// 4. PutInbox with canceled context
		if err := store.PutInbox(canceledCtx, "exec-ctx-cancel", Event{ID: "ev-cancel", Type: "Signal"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("PutInbox(canceled) err = %v; want %v", err, context.Canceled)
		}
		inbox, err := store.ListInbox(bgCtx, "exec-ctx-cancel")
		if err != nil {
			t.Fatalf("ListInbox failed: %v", err)
		}
		if len(inbox) != 0 {
			t.Fatalf("PutInbox persisted event under canceled context: %+v", inbox)
		}

		// 5. ListInbox with canceled context
		if _, err := store.ListInbox(canceledCtx, "exec-ctx-cancel"); !errors.Is(err, context.Canceled) {
			t.Fatalf("ListInbox(canceled) err = %v; want %v", err, context.Canceled)
		}

		// 6. AckInbox with canceled context
		if err := store.PutInbox(bgCtx, "exec-ctx-cancel", Event{ID: "ev-1", Type: "Signal"}); err != nil {
			t.Fatal(err)
		}
		if err := store.AckInbox(canceledCtx, "exec-ctx-cancel", []string{"ev-1"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("AckInbox(canceled) err = %v; want %v", err, context.Canceled)
		}
		inbox, err = store.ListInbox(bgCtx, "exec-ctx-cancel")
		if err != nil {
			t.Fatal(err)
		}
		if len(inbox) != 1 {
			t.Fatalf("AckInbox under canceled context mutated inbox: %+v", inbox)
		}
	})
}

func newTestExecution(id string, version uint64) *Execution {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	exec := &Execution{
		ID:          id,
		PlanID:      "test-plan",
		PlanVersion: "1.0",
		PlanDigest:  "sha256:testdigest",
		Version:     version,
		Status:      StatusRunning,
		CreatedAt:   t0,
		UpdatedAt:   t0,
		Data:        map[string]json.RawMessage{},
	}
	ensureExecution(exec)
	return exec
}

// TestADGOStoreConformanceSuite executes the shared conformance suite against
// all standard ADGO store backends: MemoryStore, FileStore, and PebbleStore.
func TestADGOStoreConformanceSuite(t *testing.T) {
	t.Run("MemoryStore", func(t *testing.T) {
		RunADGOStoreConformanceSuite(t, func(t *testing.T) (Store, func()) {
			return NewMemoryStore(), func() {}
		})
	})

	t.Run("FileStore", func(t *testing.T) {
		RunADGOStoreConformanceSuite(t, func(t *testing.T) (Store, func()) {
			dir := t.TempDir()
			store, err := NewFileStore(dir)
			if err != nil {
				t.Fatalf("NewFileStore failed: %v", err)
			}
			return store, func() {}
		})
	})

	t.Run("PebbleStore", func(t *testing.T) {
		RunADGOStoreConformanceSuite(t, func(t *testing.T) (Store, func()) {
			dir := t.TempDir()
			store, err := OpenPebbleStore(dir)
			if err != nil {
				t.Fatalf("OpenPebbleStore failed: %v", err)
			}
			return store, func() {
				_ = store.Close()
			}
		})
	})
}

// TestADGODurableStoreReopenEquivalence verifies reopen and restart equivalence
// for persistent storage backends (FileStore and PebbleStore).
func TestADGODurableStoreReopenEquivalence(t *testing.T) {
	ctx := context.Background()

	t.Run("FileStore_Reopen", func(t *testing.T) {
		dir := t.TempDir()
		store1, err := NewFileStore(dir)
		if err != nil {
			t.Fatal(err)
		}

		exec := newTestExecution("reopen-file-exec", 1)
		exec.Data["key"] = json.RawMessage(`"val"`)
		if err := store1.Create(ctx, exec); err != nil {
			t.Fatal(err)
		}
		if _, err := store1.Commit(ctx, "reopen-file-exec", 1, func(e *Execution) error {
			e.Status = StatusWaiting
			e.Data["updated"] = json.RawMessage(`42`)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := store1.PutInbox(ctx, "reopen-file-exec", Event{ID: "ev-1", Type: "Wake"}); err != nil {
			t.Fatal(err)
		}

		// Reopen store from disk
		store2, err := NewFileStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := store2.Load(ctx, "reopen-file-exec")
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Version != 2 || loaded.Status != StatusWaiting {
			t.Fatalf("reopened state version=%d, status=%s; want 2, %s", loaded.Version, loaded.Status, StatusWaiting)
		}
		inbox, err := store2.ListInbox(ctx, "reopen-file-exec")
		if err != nil {
			t.Fatal(err)
		}
		if len(inbox) != 1 || inbox[0].ID != "ev-1" {
			t.Fatalf("reopened inbox = %+v; want [ev-1]", inbox)
		}
	})

	t.Run("PebbleStore_Reopen", func(t *testing.T) {
		dir := t.TempDir()
		store1, err := OpenPebbleStore(dir)
		if err != nil {
			t.Fatal(err)
		}

		exec := newTestExecution("reopen-pebble-exec", 1)
		exec.Data["key"] = json.RawMessage(`"val"`)
		if err := store1.Create(ctx, exec); err != nil {
			t.Fatal(err)
		}
		if _, err := store1.Commit(ctx, "reopen-pebble-exec", 1, func(e *Execution) error {
			e.Status = StatusWaiting
			e.Data["updated"] = json.RawMessage(`42`)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := store1.PutInbox(ctx, "reopen-pebble-exec", Event{ID: "ev-1", Type: "Wake"}); err != nil {
			t.Fatal(err)
		}
		if err := store1.Close(); err != nil {
			t.Fatal(err)
		}

		// Reopen store from disk
		store2, err := OpenPebbleStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer store2.Close()

		loaded, err := store2.Load(ctx, "reopen-pebble-exec")
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Version != 2 || loaded.Status != StatusWaiting {
			t.Fatalf("reopened state version=%d, status=%s; want 2, %s", loaded.Version, loaded.Status, StatusWaiting)
		}
		inbox, err := store2.ListInbox(ctx, "reopen-pebble-exec")
		if err != nil {
			t.Fatal(err)
		}
		if len(inbox) != 1 || inbox[0].ID != "ev-1" {
			t.Fatalf("reopened inbox = %+v; want [ev-1]", inbox)
		}
	})
}
