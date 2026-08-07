package runtime

import (
	"context"
	"fmt"
	"time"
)

func (s *retryStore) EnqueueTask(ctx context.Context, task *ActivityTask) error {
	if task == nil {
		return s.Store.EnqueueTask(ctx, task)
	}
	mode := activityPolicy(s.module, task.ActivityName).concurrency
	switch mode {
	case "first":
		return s.enqueueFirst(ctx, task)
	case "latest":
		return s.enqueueLatest(ctx, task)
	default:
		return s.Store.EnqueueTask(ctx, task)
	}
}

func (s *retryStore) enqueueFirst(ctx context.Context, task *ActivityTask) error {
	tasks, err := s.Store.ListTasks(ctx, task.ExecutionID)
	if err != nil {
		return err
	}
	for _, existing := range tasks {
		if !sameConcurrencyLane(existing, task) || !taskIsActive(existing) {
			continue
		}
		now := time.Now().UTC()
		next := cloneRetryTask(task)
		next.Status = TaskSuperseded
		next.Error = fmt.Sprintf("concurrency:first kept active task %s", existing.ID)
		next.LockedBy = ""
		next.LockedUntil = time.Time{}
		next.NextAttemptAt = time.Time{}
		next.UpdatedAt = now
		if err := s.Store.EnqueueTask(ctx, next); err != nil {
			return err
		}
		return s.Store.AppendHistory(ctx, task.ExecutionID, "ActivitySuperseded", map[string]any{
			"activity": task.ActivityName,
			"rule":     task.RuleName,
			"task":     task.ID,
			"mode":     "first",
			"kept":     existing.ID,
			"reason":   "an earlier pending or running task already owns the activity lane",
		})
	}
	return s.Store.EnqueueTask(ctx, task)
}

func (s *retryStore) enqueueLatest(ctx context.Context, task *ActivityTask) error {
	tasks, err := s.Store.ListTasks(ctx, task.ExecutionID)
	if err != nil {
		return err
	}
	for _, existing := range tasks {
		if !sameConcurrencyLane(existing, task) || existing.Status != TaskPending {
			continue
		}
		now := time.Now().UTC()
		next := cloneRetryTask(existing)
		next.Status = TaskSuperseded
		next.Error = fmt.Sprintf("concurrency:latest replaced by task %s", task.ID)
		next.LockedBy = ""
		next.LockedUntil = time.Time{}
		next.NextAttemptAt = time.Time{}
		next.UpdatedAt = now
		if err := s.Store.UpdateTask(ctx, next); err != nil {
			return err
		}
		if err := s.Store.AppendHistory(ctx, task.ExecutionID, "ActivitySuperseded", map[string]any{
			"activity":   existing.ActivityName,
			"rule":       existing.RuleName,
			"task":       existing.ID,
			"mode":       "latest",
			"replacedBy": task.ID,
			"reason":     "a newer pending task replaced this pending task",
		}); err != nil {
			return err
		}
	}
	return s.Store.EnqueueTask(ctx, task)
}

func sameConcurrencyLane(existing, incoming *ActivityTask) bool {
	return existing != nil && incoming != nil &&
		existing.ExecutionID == incoming.ExecutionID &&
		existing.ActivityName == incoming.ActivityName &&
		existing.ID != incoming.ID
}

func taskIsActive(task *ActivityTask) bool {
	return task != nil && (task.Status == TaskPending || task.Status == TaskRunning)
}
