package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
)

type Store struct {
	mu         sync.Mutex
	executions map[string]*runtime.Execution
	history    map[string][]runtime.HistoryEntry
	tasks      map[string][]*runtime.ActivityTask
	taskByID   map[string]*runtime.ActivityTask
	pending    map[string][]string
	taskIndex  map[string]map[string]*runtime.ActivityTask
}

func NewStore() *Store {
	return &Store{
		executions: map[string]*runtime.Execution{},
		history:    map[string][]runtime.HistoryEntry{},
		tasks:      map[string][]*runtime.ActivityTask{},
		taskByID:   map[string]*runtime.ActivityTask{},
		pending:    map[string][]string{},
		taskIndex:  map[string]map[string]*runtime.ActivityTask{},
	}
}

// CreateExecution stores a new execution in memory.
// The in-memory store does not use context for cancellation; operations are
// instantaneous under a mutex.
func (s *Store) CreateExecution(ctx context.Context, execution *runtime.Execution) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.executions[execution.ID]; ok {
		return fmt.Errorf("execution already exists: %s", execution.ID)
	}
	s.executions[execution.ID] = cloneExecution(execution)
	return nil
}

func (s *Store) GetExecution(ctx context.Context, id string) (*runtime.Execution, error) {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	execution, ok := s.executions[id]
	if !ok {
		return nil, runtime.ErrExecutionNotFound
	}
	return cloneExecution(execution), nil
}

func (s *Store) SaveExecution(ctx context.Context, execution *runtime.Execution) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.executions[execution.ID]; !ok {
		return runtime.ErrExecutionNotFound
	}
	next := cloneExecution(execution)
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	s.executions[execution.ID] = next
	return nil
}

func (s *Store) AppendHistory(ctx context.Context, executionID string, entryType string, payload map[string]any) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := len(s.history[executionID]) + 1
	s.history[executionID] = append(s.history[executionID], runtime.HistoryEntry{
		Seq:       seq,
		Type:      entryType,
		Payload:   runtime.CloneAnyMap(payload),
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (s *Store) ListHistory(ctx context.Context, executionID string) ([]runtime.HistoryEntry, error) {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.history[executionID]
	out := make([]runtime.HistoryEntry, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Payload = runtime.CloneAnyMap(value.Payload)
	}
	return out, nil
}

func (s *Store) EnqueueTask(ctx context.Context, task *runtime.ActivityTask) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneTask(task)
	s.tasks[task.ExecutionID] = append(s.tasks[task.ExecutionID], next)
	s.indexTask(next)
	return nil
}

func (s *Store) ListTasks(ctx context.Context, executionID string) ([]*runtime.ActivityTask, error) {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.tasks[executionID]
	out := make([]*runtime.ActivityTask, 0, len(values))
	for _, value := range values {
		out = append(out, cloneTask(value))
	}
	return out, nil
}

func (s *Store) PollTask(ctx context.Context, executionID string) (*runtime.ActivityTask, error) {
	return s.PollTaskWithLease(ctx, executionID, "memory-worker", time.Minute)
}

func (s *Store) PollTaskWithLease(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*runtime.ActivityTask, error) {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	if workerID == "" {
		workerID = "memory-worker"
	}
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	now := time.Now().UTC()
	// Scan only the queue snapshot that existed at method entry. A future retry
	// is rotated to the tail, but must not be visited again in the same poll;
	// otherwise a queue containing only future retries spins forever.
	queueLen := len(s.pending[executionID])
	for scanned := 0; scanned < queueLen; scanned++ {
		taskID := s.pending[executionID][0]
		s.pending[executionID] = s.pending[executionID][1:]
		task := s.taskByID[taskID]
		if task == nil || task.ExecutionID != executionID || task.Status != runtime.TaskPending {
			continue
		}
		if !task.NextAttemptAt.IsZero() && task.NextAttemptAt.After(now) {
			s.pending[executionID] = append(s.pending[executionID], taskID)
			continue
		}
		task.Status = runtime.TaskRunning
		task.Attempt++
		task.LockedBy = workerID
		task.LockedUntil = now.Add(leaseTTL)
		task.UpdatedAt = now
		return cloneTask(task), nil
	}
	return nil, nil
}

func (s *Store) UpdateTask(ctx context.Context, task *runtime.ActivityTask) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.tasks[task.ExecutionID]
	for i, existing := range values {
		if existing.ID == task.ID {
			next := cloneTask(task)
			values[i] = next
			s.indexTask(next)
			return nil
		}
	}
	return fmt.Errorf("task not found: %s", task.ID)
}

func (s *Store) HeartbeatTask(ctx context.Context, taskID string, workerID string) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.taskByID[taskID]
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if workerID != "" && task.LockedBy != workerID {
		return fmt.Errorf("task %s locked by %s", taskID, task.LockedBy)
	}
	if !task.LockedUntil.IsZero() {
		task.LockedUntil = time.Now().UTC().Add(task.LockedUntil.Sub(task.UpdatedAt))
	}
	task.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) RecoverExpiredLeases(ctx context.Context, executionID string, leaseTTL time.Duration) (int, error) {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	recovered := 0
	for _, task := range s.tasks[executionID] {
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
		s.pending[executionID] = append(s.pending[executionID], task.ID)
		recovered++
	}
	return recovered, nil
}

func (s *Store) CompleteTask(ctx context.Context, taskID string, result map[string]any) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.taskByID[taskID]
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	task.Status = runtime.TaskCompleted
	task.Result = runtime.CloneAnyMap(result)
	task.Error = ""
	task.LockedBy = ""
	task.LockedUntil = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) FailTask(ctx context.Context, taskID string, errorMessage string) error {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.taskByID[taskID]
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	task.Status = runtime.TaskFailed
	task.Error = errorMessage
	task.LockedBy = ""
	task.LockedUntil = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) FindTask(ctx context.Context, executionID string, ruleName string, activityName string, idempotencyKey string) (*runtime.ActivityTask, error) {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.taskIndex[executionID][taskIndexKey(ruleName, activityName, idempotencyKey)]
	return cloneTask(task), nil
}

func (s *Store) NextTaskSeq(ctx context.Context, executionID string) (int, error) {
	_ = ctx // in-memory store does not need context
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks[executionID]) + 1, nil
}

func (s *Store) indexTask(task *runtime.ActivityTask) {
	if task == nil {
		return
	}
	s.taskByID[task.ID] = task
	if task.Status == runtime.TaskPending {
		s.pending[task.ExecutionID] = append(s.pending[task.ExecutionID], task.ID)
	}
	if s.taskIndex[task.ExecutionID] == nil {
		s.taskIndex[task.ExecutionID] = map[string]*runtime.ActivityTask{}
	}
	s.taskIndex[task.ExecutionID][taskIndexKey(task.RuleName, task.ActivityName, task.IdempotencyKey)] = task
}

func taskIndexKey(ruleName string, activityName string, idempotencyKey string) string {
	return ruleName + "\x00" + activityName + "\x00" + idempotencyKey
}

// cloneExecution delegates to runtime.CloneExecution for deep copy.
func cloneExecution(in *runtime.Execution) *runtime.Execution {
	return runtime.CloneExecution(in)
}

func cloneExecutionState(in runtime.ExecutionState) runtime.ExecutionState {
	out := runtime.ExecutionState{
		ActiveAtoms: append([]uint64{}, in.ActiveAtoms...),
		Present:     append([]uint64{}, in.Present...),
		BoolValues:  append([]uint64{}, in.BoolValues...),
		DirtyFields: append([]uint64{}, in.DirtyFields...),
	}
	if in.Values != nil {
		out.Values = make(map[uint32]runtime.Value, len(in.Values))
		for key, value := range in.Values {
			out.Values[key] = cloneValue(value)
		}
	}
	if in.AtomValues != nil {
		out.AtomValues = make(map[uint32]runtime.Value, len(in.AtomValues))
		for key, value := range in.AtomValues {
			out.AtomValues[key] = cloneValue(value)
		}
	}
	if in.FactValues != nil {
		out.FactValues = make(map[uint32]map[string]runtime.Value, len(in.FactValues))
		for atomID, values := range in.FactValues {
			next := make(map[string]runtime.Value, len(values))
			for name, value := range values {
				next[name] = cloneValue(value)
			}
			out.FactValues[atomID] = next
		}
	}
	return out
}

func cloneValue(value runtime.Value) runtime.Value {
	if value.Kind == runtime.ValueAny {
		value.Any = runtime.CloneAny(value.Any)
	}
	return value
}

func cloneTask(in *runtime.ActivityTask) *runtime.ActivityTask {
	if in == nil {
		return nil
	}
	return &runtime.ActivityTask{
		ID:             in.ID,
		ExecutionID:    in.ExecutionID,
		RuleName:       in.RuleName,
		ActivityName:   in.ActivityName,
		Input:          runtime.CloneAnyMap(in.Input),
		IdempotencyKey: in.IdempotencyKey,
		Status:         in.Status,
		Attempt:        in.Attempt,
		MaxAttempts:    in.MaxAttempts,
		LockedBy:       in.LockedBy,
		LockedUntil:    in.LockedUntil,
		NextAttemptAt:  in.NextAttemptAt,
		Result:         runtime.CloneAnyMap(in.Result),
		Error:          in.Error,
		CreatedAt:      in.CreatedAt,
		UpdatedAt:      in.UpdatedAt,
	}
}
