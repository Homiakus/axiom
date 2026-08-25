package memory

import (
	"context"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
)

func TestPollTaskWithLeaseReturnsWhenOnlyFutureTaskIsPending(t *testing.T) {
	store := NewStore()
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

	task, err := store.PollTaskWithLease(ctx, "exec", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if task != nil {
		t.Fatalf("future task leased early: %+v", task)
	}
}

func TestPollTaskWithLeaseSkipsFutureTaskAndLeasesDueTask(t *testing.T) {
	store := NewStore()
	ctx := context.Background()
	now := time.Now().UTC()
	future := &runtime.ActivityTask{
		ID:            "exec:1",
		ExecutionID:   "exec",
		Status:        runtime.TaskPending,
		NextAttemptAt: now.Add(time.Hour),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	due := &runtime.ActivityTask{
		ID:          "exec:2",
		ExecutionID: "exec",
		Status:      runtime.TaskPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.EnqueueTask(ctx, future); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueTask(ctx, due); err != nil {
		t.Fatal(err)
	}

	task, err := store.PollTaskWithLease(ctx, "exec", "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.ID != due.ID {
		t.Fatalf("leased=%+v want %s", task, due.ID)
	}

	remaining, err := store.ListTasks(ctx, "exec")
	if err != nil {
		t.Fatal(err)
	}
	if remaining[0].Status != runtime.TaskPending {
		t.Fatalf("future status=%s want pending", remaining[0].Status)
	}
}
