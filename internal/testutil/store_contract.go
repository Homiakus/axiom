package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
)

type StoreFactory func(t *testing.T) runtime.Store

// RunStoreContract applies backend-independent runtime.Store invariants. Every
// subtest receives a fresh store so failures are local and backend state cannot
// leak between contract cases.
func RunStoreContract(t *testing.T, factory StoreFactory) {
	t.Helper()

	fresh := func(t *testing.T) runtime.Store {
		t.Helper()
		store := factory(t)
		if store == nil {
			t.Fatal("store factory returned nil")
		}
		return store
	}

	t.Run("missing execution uses canonical error", func(t *testing.T) {
		store := fresh(t)
		_, err := store.GetExecution(context.Background(), "missing")
		if !errors.Is(err, runtime.ErrExecutionNotFound) {
			t.Fatalf("err=%v want %v", err, runtime.ErrExecutionNotFound)
		}
	})

	t.Run("execution create input is isolated", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		execution := contractExecution("exec")
		if err := store.CreateExecution(ctx, execution); err != nil {
			t.Fatal(err)
		}
		execution.Context["Order"]["status"] = "caller-mutated"
		got, err := store.GetExecution(ctx, execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Context["Order"]["status"] != "created" {
			t.Fatalf("stored context retained caller pointer: %+v", got.Context)
		}
	})

	t.Run("execution read result is isolated", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		execution := contractExecution("exec")
		if err := store.CreateExecution(ctx, execution); err != nil {
			t.Fatal(err)
		}
		first, err := store.GetExecution(ctx, execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		first.Context["Order"]["status"] = "read-mutated"
		second, err := store.GetExecution(ctx, execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		if second.Context["Order"]["status"] != "created" {
			t.Fatalf("read result aliases stored state: %+v", second.Context)
		}
	})

	t.Run("save increments stored version without mutating caller", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		if err := store.CreateExecution(ctx, contractExecution("exec")); err != nil {
			t.Fatal(err)
		}
		candidate, err := store.GetExecution(ctx, "exec")
		if err != nil {
			t.Fatal(err)
		}
		candidate.Context["Order"]["status"] = "saved"
		callerVersion := candidate.Version
		callerUpdatedAt := candidate.UpdatedAt
		if err := store.SaveExecution(ctx, candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.Version != callerVersion {
			t.Fatalf("SaveExecution mutated caller version: got=%d want=%d", candidate.Version, callerVersion)
		}
		if !candidate.UpdatedAt.Equal(callerUpdatedAt) {
			t.Fatalf("SaveExecution mutated caller UpdatedAt: got=%v want=%v", candidate.UpdatedAt, callerUpdatedAt)
		}
		candidate.Context["Order"]["status"] = "after-save-caller-mutation"
		stored, err := store.GetExecution(ctx, "exec")
		if err != nil {
			t.Fatal(err)
		}
		if stored.Version != callerVersion+1 {
			t.Fatalf("stored version=%d want=%d", stored.Version, callerVersion+1)
		}
		if stored.Context["Order"]["status"] != "saved" {
			t.Fatalf("saved execution aliases caller: %+v", stored.Context)
		}
	})

	t.Run("history payload is isolated on write and read", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		payload := map[string]any{"nested": map[string]any{"value": "original"}}
		if err := store.AppendHistory(ctx, "exec", "event", payload); err != nil {
			t.Fatal(err)
		}
		payload["nested"].(map[string]any)["value"] = "caller-mutated"
		first, err := store.ListHistory(ctx, "exec")
		if err != nil {
			t.Fatal(err)
		}
		if got := first[0].Payload["nested"].(map[string]any)["value"]; got != "original" {
			t.Fatalf("history write alias=%v", got)
		}
		first[0].Payload["nested"].(map[string]any)["value"] = "read-mutated"
		second, err := store.ListHistory(ctx, "exec")
		if err != nil {
			t.Fatal(err)
		}
		if got := second[0].Payload["nested"].(map[string]any)["value"]; got != "original" {
			t.Fatalf("history read alias=%v", got)
		}
	})

	t.Run("task input and list results are isolated", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		now := time.Now().UTC()
		task := &runtime.ActivityTask{
			ID:          "exec:1",
			ExecutionID: "exec",
			Status:      runtime.TaskPending,
			Input:       map[string]any{"nested": map[string]any{"value": "original"}},
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := store.EnqueueTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		task.Input["nested"].(map[string]any)["value"] = "caller-mutated"
		first, err := store.ListTasks(ctx, "exec")
		if err != nil {
			t.Fatal(err)
		}
		if got := first[0].Input["nested"].(map[string]any)["value"]; got != "original" {
			t.Fatalf("task write alias=%v", got)
		}
		first[0].Input["nested"].(map[string]any)["value"] = "read-mutated"
		second, err := store.ListTasks(ctx, "exec")
		if err != nil {
			t.Fatal(err)
		}
		if got := second[0].Input["nested"].(map[string]any)["value"]; got != "original" {
			t.Fatalf("task read alias=%v", got)
		}
	})

	t.Run("future retry is not leased early", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		now := time.Now().UTC()
		if err := store.EnqueueTask(ctx, &runtime.ActivityTask{
			ID:            "exec:1",
			ExecutionID:   "exec",
			Status:        runtime.TaskPending,
			NextAttemptAt: now.Add(time.Hour),
			CreatedAt:     now,
			UpdatedAt:     now,
		}); err != nil {
			t.Fatal(err)
		}
		got, err := store.PollTaskWithLease(ctx, "exec", "worker", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("future task leased early: %+v", got)
		}
	})

	t.Run("completed task is not leased again", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		now := time.Now().UTC()
		if err := store.EnqueueTask(ctx, &runtime.ActivityTask{ID: "exec:1", ExecutionID: "exec", Status: runtime.TaskPending, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		leased, err := store.PollTaskWithLease(ctx, "exec", "worker", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if leased == nil {
			t.Fatal("due task was not leased")
		}
		if err := store.CompleteTask(ctx, leased.ID, map[string]any{"ok": true}); err != nil {
			t.Fatal(err)
		}
		next, err := store.PollTaskWithLease(ctx, "exec", "worker", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if next != nil {
			t.Fatalf("completed task was leased again: %+v", next)
		}
	})

	t.Run("heartbeat rejects different worker", func(t *testing.T) {
		store := fresh(t)
		ctx := context.Background()
		now := time.Now().UTC()
		if err := store.EnqueueTask(ctx, &runtime.ActivityTask{ID: "exec:1", ExecutionID: "exec", Status: runtime.TaskPending, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		leased, err := store.PollTaskWithLease(ctx, "exec", "worker-a", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if leased == nil {
			t.Fatal("due task was not leased")
		}
		if err := store.HeartbeatTask(ctx, leased.ID, "worker-b"); err == nil {
			t.Fatal("heartbeat from different worker succeeded")
		}
		if err := store.HeartbeatTask(ctx, leased.ID, "worker-a"); err != nil {
			t.Fatalf("heartbeat from owner failed: %v", err)
		}
	})
}

func contractExecution(id string) *runtime.Execution {
	now := time.Now().UTC()
	return &runtime.Execution{
		ID:        id,
		Status:    runtime.StatusRunning,
		Context:   map[string]map[string]any{"Order": {"status": "created"}},
		Computed:  map[string]any{},
		Facts:     map[string]runtime.FactValue{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}
