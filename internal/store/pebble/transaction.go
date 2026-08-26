package pebble

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
	pebbledb "github.com/cockroachdb/pebble"
)

type txStore struct {
	mu          sync.Mutex
	parent      *Store
	batch       *pebbledb.Batch
	closed      bool
	executions  map[string]*runtime.Execution
	tasks       map[string]*runtime.ActivityTask
	history     map[string][]runtime.HistoryEntry
	historySeq  map[string]int
	unlockFuncs []func()
	lockedExecs map[string]struct{}
}

func (s *Store) BeginTransaction(ctx context.Context) (runtime.StoreTransaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &txStore{
		parent:      s,
		batch:       s.db.NewBatch(),
		executions:  map[string]*runtime.Execution{},
		tasks:       map[string]*runtime.ActivityTask{},
		history:     map[string][]runtime.HistoryEntry{},
		historySeq:  map[string]int{},
		lockedExecs: map[string]struct{}{},
	}, nil
}

func (tx *txStore) lockExecution(executionID string) {
	if executionID == "" || tx.parent.execLocks == nil {
		return
	}
	if tx.lockedExecs == nil {
		tx.lockedExecs = make(map[string]struct{})
	}
	if _, ok := tx.lockedExecs[executionID]; ok {
		return
	}
	unlock := tx.parent.execLocks.Lock(executionID)
	tx.unlockFuncs = append(tx.unlockFuncs, unlock)
	tx.lockedExecs[executionID] = struct{}{}
}

func (tx *txStore) cleanupLocks() {
	for i := len(tx.unlockFuncs) - 1; i >= 0; i-- {
		tx.unlockFuncs[i]()
	}
	tx.unlockFuncs = nil
	tx.lockedExecs = nil
}

func (tx *txStore) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.closed {
		return nil
	}
	tx.closed = true
	defer tx.cleanupLocks()
	defer tx.batch.Close()
	return tx.batch.Commit(tx.parent.writeOptions())
}

func (tx *txStore) Rollback() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.closed {
		return nil
	}
	tx.closed = true
	defer tx.cleanupLocks()
	return tx.batch.Close()
}

func (tx *txStore) CreateExecution(ctx context.Context, execution *runtime.Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(execution.ID)
	if _, ok := tx.executions[execution.ID]; ok {
		return fmt.Errorf("execution already exists: %s", execution.ID)
	}
	if exists, err := tx.parent.exists(execKey(execution.ID)); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("execution already exists: %s", execution.ID)
	}
	next := cloneExecution(execution)
	tx.executions[next.ID] = next
	return tx.parent.writeExecution(tx.batch, next)
}

func (tx *txStore) GetExecution(ctx context.Context, id string) (*runtime.Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(id)
	if execution, ok := tx.executions[id]; ok {
		return cloneExecution(execution), nil
	}
	execution, err := tx.parent.getExecutionLocked(id)
	if err != nil {
		return nil, err
	}
	return execution, nil
}

func (tx *txStore) SaveExecution(ctx context.Context, execution *runtime.Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(execution.ID)
	if _, ok := tx.executions[execution.ID]; !ok {
		if exists, err := tx.parent.exists(execKey(execution.ID)); err != nil {
			return err
		} else if !exists {
			return runtime.ErrExecutionNotFound
		}
	}
	next := cloneExecution(execution)
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	tx.executions[next.ID] = next
	return tx.parent.writeExecution(tx.batch, next)
}

func (tx *txStore) AppendHistory(ctx context.Context, executionID string, entryType string, payload map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(executionID)
	seq, err := tx.nextHistorySeqLocked(executionID)
	if err != nil {
		return err
	}
	entry := runtime.HistoryEntry{
		Seq:       seq,
		Type:      entryType,
		Payload:   cloneAnyMap(payload),
		CreatedAt: time.Now().UTC(),
	}
	if err := tx.parent.putValue(tx.batch, historyKey(executionID, seq), entry); err != nil {
		return err
	}
	if err := putUint(tx.batch, historySeqKey(executionID), seq); err != nil {
		return err
	}
	tx.historySeq[executionID] = seq
	tx.history[executionID] = append(tx.history[executionID], runtime.HistoryEntry{
		Seq:       entry.Seq,
		Type:      entry.Type,
		Payload:   cloneAnyMap(entry.Payload),
		CreatedAt: entry.CreatedAt,
	})
	return nil
}

func (tx *txStore) ListHistory(ctx context.Context, executionID string) ([]runtime.HistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(executionID)
	history, err := tx.parent.listHistoryLocked(executionID)
	if err != nil {
		return nil, err
	}
	for _, entry := range tx.history[executionID] {
		history = append(history, runtime.HistoryEntry{Seq: entry.Seq, Type: entry.Type, Payload: cloneAnyMap(entry.Payload), CreatedAt: entry.CreatedAt})
	}
	return history, nil
}

func (tx *txStore) EnqueueTask(ctx context.Context, task *runtime.ActivityTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(task.ExecutionID)
	next := cloneTask(task)
	tx.tasks[next.ID] = next
	return tx.parent.writeTask(tx.batch, next)
}

func (tx *txStore) ListTasks(ctx context.Context, executionID string) ([]*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(executionID)
	return tx.listTasksMerged(executionID)
}

func (tx *txStore) listTasksMerged(executionID string) ([]*runtime.ActivityTask, error) {
	tasks, err := tx.parent.listTasksLocked(executionID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*runtime.ActivityTask, len(tasks)+len(tx.tasks))
	for _, task := range tasks {
		byID[task.ID] = cloneTask(task)
	}
	for _, task := range tx.tasks {
		if task.ExecutionID == executionID {
			byID[task.ID] = cloneTask(task)
		}
	}
	out := make([]*runtime.ActivityTask, 0, len(byID))
	for _, task := range byID {
		out = append(out, cloneTask(task))
	}
	sortTasksCanonical(out)
	return out, nil
}

func (tx *txStore) PollTask(ctx context.Context, executionID string) (*runtime.ActivityTask, error) {
	task, err := tx.PollTaskWithLease(ctx, executionID, tx.parent.owner, tx.parent.leaseTTL)
	if errors.Is(err, errNoTask) {
		return nil, nil
	}
	return task, err
}

func (tx *txStore) PollTaskWithLease(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(executionID)
	return tx.pollTaskWithLeaseLocked(ctx, executionID, workerID, leaseTTL)
}

func (tx *txStore) pollTaskWithLeaseLocked(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*runtime.ActivityTask, error) {
	if workerID == "" {
		workerID = tx.parent.owner
	}
	if leaseTTL <= 0 {
		leaseTTL = tx.parent.leaseTTL
	}
	now := time.Now().UTC()
	task, err := tx.nextPendingTask(executionID, now)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}
	task.Status = runtime.TaskRunning
	task.Attempt++
	task.LockedBy = workerID
	task.LockedUntil = now.Add(leaseTTL)
	task.UpdatedAt = now
	tx.tasks[task.ID] = cloneTask(task)
	if err := tx.parent.writeTask(tx.batch, task); err != nil {
		return nil, err
	}
	return cloneTask(task), nil
}

func (tx *txStore) HeartbeatTask(ctx context.Context, taskID string, workerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	task, err := tx.getTaskLocked(taskID)
	if err != nil {
		return err
	}
	tx.lockExecution(task.ExecutionID)
	if workerID != "" && task.LockedBy != workerID {
		return fmt.Errorf("task %s locked by %s", taskID, task.LockedBy)
	}
	now := time.Now().UTC()
	if !task.LockedUntil.IsZero() {
		task.LockedUntil = now.Add(tx.parent.leaseTTL)
	}
	task.UpdatedAt = now
	tx.tasks[task.ID] = cloneTask(task)
	return tx.parent.writeTask(tx.batch, task)
}

func (tx *txStore) RecoverExpiredLeases(ctx context.Context, executionID string, leaseTTL time.Duration) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(executionID)
	if leaseTTL <= 0 {
		leaseTTL = tx.parent.leaseTTL
	}
	now := time.Now().UTC()
	tasks, err := tx.listTasksMerged(executionID)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, task := range tasks {
		if task.Status != runtime.TaskRunning {
			continue
		}
		expired := false
		if leaseTTL > 0 {
			expired = !task.UpdatedAt.Add(leaseTTL).After(now)
		}
		if !expired && !task.LockedUntil.IsZero() {
			expired = !task.LockedUntil.After(now)
		}
		if !expired {
			continue
		}
		task.Status = runtime.TaskPending
		task.LockedBy = ""
		task.LockedUntil = time.Time{}
		task.UpdatedAt = now
		tx.tasks[task.ID] = cloneTask(task)
		if err := tx.parent.writeTask(tx.batch, task); err != nil {
			return 0, err
		}
		recovered++
	}
	return recovered, nil
}

func (tx *txStore) CompleteTask(ctx context.Context, taskID string, result map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	task, err := tx.getTaskLocked(taskID)
	if err != nil {
		return err
	}
	tx.lockExecution(task.ExecutionID)
	task.Status = runtime.TaskCompleted
	task.Result = cloneAnyMap(result)
	task.Error = ""
	task.LockedBy = ""
	task.LockedUntil = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	tx.tasks[task.ID] = cloneTask(task)
	return tx.parent.writeTask(tx.batch, task)
}

func (tx *txStore) FailTask(ctx context.Context, taskID string, errorMessage string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	task, err := tx.getTaskLocked(taskID)
	if err != nil {
		return err
	}
	tx.lockExecution(task.ExecutionID)
	task.Status = runtime.TaskFailed
	task.Error = errorMessage
	task.LockedBy = ""
	task.LockedUntil = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	tx.tasks[task.ID] = cloneTask(task)
	return tx.parent.writeTask(tx.batch, task)
}

func (tx *txStore) UpdateTask(ctx context.Context, task *runtime.ActivityTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(task.ExecutionID)
	if _, ok := tx.tasks[task.ID]; !ok {
		if exists, err := tx.parent.exists(taskKey(task.ExecutionID, task.ID)); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("task not found: %s", task.ID)
		}
	}
	next := cloneTask(task)
	tx.tasks[next.ID] = next
	return tx.parent.writeTask(tx.batch, next)
}

func (tx *txStore) FindTask(ctx context.Context, executionID string, ruleName string, activityName string, idempotencyKey string) (*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(executionID)
	for _, task := range tx.tasks {
		if task.ExecutionID == executionID && task.RuleName == ruleName && task.ActivityName == activityName && task.IdempotencyKey == idempotencyKey {
			return cloneTask(task), nil
		}
	}
	return tx.parent.findTaskLocked(executionID, ruleName, activityName, idempotencyKey)
}

func (tx *txStore) NextTaskSeq(ctx context.Context, executionID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.lockExecution(executionID)
	next, err := tx.parent.nextTaskSeqLocked(executionID)
	if err != nil {
		return 0, err
	}
	for _, task := range tx.tasks {
		if task.ExecutionID != executionID {
			continue
		}
		if seq := taskSeqFromID(task.ID); seq >= next {
			next = seq + 1
		}
	}
	return next, nil
}

func (tx *txStore) getTaskLocked(taskID string) (*runtime.ActivityTask, error) {
	if task, ok := tx.tasks[taskID]; ok {
		return cloneTask(task), nil
	}
	return tx.parent.getTaskByIDLocked(taskID)
}

func (tx *txStore) nextPendingTask(executionID string, now time.Time) (*runtime.ActivityTask, error) {
	tasks, err := tx.listTasksMerged(executionID)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if task.Status != runtime.TaskPending {
			continue
		}
		if !task.NextAttemptAt.IsZero() && task.NextAttemptAt.After(now) {
			continue
		}
		return cloneTask(task), nil
	}
	return nil, nil
}

func (tx *txStore) nextHistorySeqLocked(executionID string) (int, error) {
	if seq, ok := tx.historySeq[executionID]; ok {
		return seq + 1, nil
	}
	seq, err := tx.parent.getUint(historySeqKey(executionID))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return seq + 1, nil
}
