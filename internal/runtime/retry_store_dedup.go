package runtime

import "context"

func (s *retryStore) FindTask(ctx context.Context, executionID string, ruleName string, activityName string, idempotencyKey string) (*ActivityTask, error) {
	if indexed, ok := s.Store.(TaskDedupStore); ok {
		return indexed.FindTask(ctx, executionID, ruleName, activityName, idempotencyKey)
	}
	tasks, err := s.Store.ListTasks(ctx, executionID)
	if err != nil {
		return nil, err
	}
	for index := len(tasks) - 1; index >= 0; index-- {
		task := tasks[index]
		if task == nil {
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
