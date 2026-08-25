package pebble

import (
	"context"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
)

func pendingTask(id string) *runtime.ActivityTask {
	return &runtime.ActivityTask{
		ID:          id,
		ExecutionID: "exec",
		Status:      runtime.TaskPending,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

func TestTransactionListTasksUsesCanonicalSequenceOrder(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tx, err := store.BeginTransaction(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, id := range []string{"exec:10", "exec:2", "exec:1"} {
		if err := tx.EnqueueTask(context.Background(), pendingTask(id)); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := tx.ListTasks(context.Background(), "exec")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec:1", "exec:2", "exec:10"}
	if len(tasks) != len(want) {
		t.Fatalf("len=%d want=%d", len(tasks), len(want))
	}
	for i := range want {
		if tasks[i].ID != want[i] {
			t.Fatalf("tasks[%d]=%q want=%q", i, tasks[i].ID, want[i])
		}
	}
}

func TestTransactionPollDoesNotReLeaseParentSnapshot(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnqueueTask(ctx, pendingTask("exec:1")); err != nil {
		t.Fatal(err)
	}

	tx, err := store.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	first, err := tx.PollTask(ctx, "exec")
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID != "exec:1" {
		t.Fatalf("first=%+v", first)
	}
	second, err := tx.PollTask(ctx, "exec")
	if err != nil {
		t.Fatal(err)
	}
	if second != nil {
		t.Fatalf("same parent task was leased twice: %+v", second)
	}
}

func TestTransactionPollOrdersMergedParentAndStagedTasks(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.EnqueueTask(ctx, pendingTask("exec:10")); err != nil {
		t.Fatal(err)
	}

	tx, err := store.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.EnqueueTask(ctx, pendingTask("exec:2")); err != nil {
		t.Fatal(err)
	}
	leased, err := tx.PollTask(ctx, "exec")
	if err != nil {
		t.Fatal(err)
	}
	if leased == nil || leased.ID != "exec:2" {
		t.Fatalf("leased=%+v want exec:2", leased)
	}
}
