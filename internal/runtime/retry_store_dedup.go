package runtime

import "context"

func (s *retryStore) FindTask(ctx context.Context, executionID string, ruleName string, activityName string, idempotencyKey string) (*ActivityTask, error) {
	if indexed, ok := s.Store.(TaskDedupStore); ok {
		task, err := indexed.FindTask(ctx, executionID, ruleName, activityName, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if task == nil || task.Status != TaskSuperseded {
			return task, nil
		}
		// A superseded task is terminal for scheduling purposes and must not
		// shadow an older completed/active task with the same idempotency key.
		// Fall back to the full task list only for this uncommon case.
	}
	tasks, err := s.Store.ListTasks(ctx, executionID)
	if err != nil {
		return nil, err
	}
	for index := len(tasks) - 1; index >= 0; index-- {
		task := tasks[index]
		if task == nil || task.Status == TaskSuperseded {
			continue
		}
		if task.RuleName == ruleName && task.ActivityName == activityName && task.IdempotencyKey == idempotencyKey {
			return cloneRetryTask(task), nil
		}
	}
	return nil, nil
}

func (s *retryStore) NextTaskSeq(ctx context.Context, executionID string) (int, error) {
	if indexed, ok := s.Store.(TaskDedupStore); ok {
		return indexed.NextTaskSeq(ctx, executionID)
	}
	tasks, err := s.Store.ListTasks(ctx, executionID)
	if err != nil {
		return 0, err
	}
	return len(tasks) + 1, nil
}
