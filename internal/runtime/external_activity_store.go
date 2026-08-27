package runtime

import (
	"context"
	"errors"
	"time"
)

// ErrExternalActivityWorkerRequired means an execution has an active task whose
// activity is explicitly owned by a fenced external worker. The inline runner
// must not lease or mutate that task.
var ErrExternalActivityWorkerRequired = errors.New("axiom: external activity requires external worker")

type externalOwnershipStore struct {
	Store
	external map[string]struct{}
}

func wrapExternalOwnershipStore(store Store, external map[string]struct{}) Store {
	if store == nil || len(external) == 0 {
		return store
	}
	copied := make(map[string]struct{}, len(external))
	for name := range external {
		copied[name] = struct{}{}
	}
	base := &externalOwnershipStore{Store: store, external: copied}
	if transactional, ok := store.(TransactionalStore); ok {
		return &externalOwnershipTransactionalStore{externalOwnershipStore: base, transactional: transactional}
	}
	return base
}

func (s *externalOwnershipStore) PollTask(ctx context.Context, executionID string) (*ActivityTask, error) {
	if err := s.rejectInlineExternalWork(ctx, executionID); err != nil {
		return nil, err
	}
	return s.Store.PollTask(ctx, executionID)
}

func (s *externalOwnershipStore) PollTaskWithLease(ctx context.Context, executionID, workerID string, leaseTTL time.Duration) (*ActivityTask, error) {
	if workerID == "inline-worker" {
		if err := s.rejectInlineExternalWork(ctx, executionID); err != nil {
			return nil, err
		}
	}
	return s.Store.PollTaskWithLease(ctx, executionID, workerID, leaseTTL)
}

// Preserve the optional TaskDedupStore capability across the ownership wrapper.
// Stores without a specialized index retain the same list-based fallback used
// by Engine.scheduleActivity before wrapping.
func (s *externalOwnershipStore) FindTask(ctx context.Context, executionID, ruleName, activityName, idempotencyKey string) (*ActivityTask, error) {
	if indexed, ok := s.Store.(TaskDedupStore); ok {
		return indexed.FindTask(ctx, executionID, ruleName, activityName, idempotencyKey)
	}
	tasks, err := s.Store.ListTasks(ctx, executionID)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if task != nil && task.RuleName == ruleName && task.ActivityName == activityName && task.IdempotencyKey == idempotencyKey {
			copy := *task
			copy.Input = cloneAnyMap(task.Input)
			copy.Result = cloneAnyMap(task.Result)
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *externalOwnershipStore) NextTaskSeq(ctx context.Context, executionID string) (int, error) {
	if indexed, ok := s.Store.(TaskDedupStore); ok {
		return indexed.NextTaskSeq(ctx, executionID)
	}
	tasks, err := s.Store.ListTasks(ctx, executionID)
	if err != nil {
		return 0, err
	}
	return len(tasks) + 1, nil
}

func (s *externalOwnershipStore) rejectInlineExternalWork(ctx context.Context, executionID string) error {
	tasks, err := s.Store.ListTasks(ctx, executionID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if _, external := s.external[task.ActivityName]; !external {
			continue
		}
		switch task.Status {
		case TaskPending, TaskRunning:
			return ErrExternalActivityWorkerRequired
		}
	}
	return nil
}

type externalOwnershipTransactionalStore struct {
	*externalOwnershipStore
	transactional TransactionalStore
}

func (s *externalOwnershipTransactionalStore) BeginTransaction(ctx context.Context) (StoreTransaction, error) {
	tx, err := s.transactional.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := &externalOwnershipStore{Store: tx, external: s.external}
	return &externalOwnershipStoreTransaction{externalOwnershipStore: wrapped, tx: tx}, nil
}

type externalOwnershipStoreTransaction struct {
	*externalOwnershipStore
	tx StoreTransaction
}

func (s *externalOwnershipStoreTransaction) Commit() error   { return s.tx.Commit() }
func (s *externalOwnershipStoreTransaction) Rollback() error { return s.tx.Rollback() }
