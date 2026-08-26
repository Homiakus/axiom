package pebble

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
	pebbledb "github.com/cockroachdb/pebble"
)

const (
	defaultLeaseTTL = 30 * time.Second
)

type Store struct {
	mu         sync.Mutex
	db         *pebbledb.DB
	leaseTTL   time.Duration
	owner      string
	sync       bool
	flushEvery time.Duration
	stopFlush  chan struct{}
	flushDone  chan struct{}
	codec      codecKind
}

type Option func(*Store)

func WithLeaseTTL(ttl time.Duration) Option {
	return func(s *Store) {
		if ttl > 0 {
			s.leaseTTL = ttl
		}
	}
}

func WithLeaseOwner(owner string) Option {
	return func(s *Store) {
		if owner != "" {
			s.owner = owner
		}
	}
}

func WithNoSync() Option {
	return func(s *Store) {
		s.sync = false
	}
}

func WithSyncEvery(interval time.Duration) Option {
	return func(s *Store) {
		if interval > 0 {
			s.sync = false
			s.flushEvery = interval
		}
	}
}
// WithJSONCodec configures the store to use JSON encoding for value records (default).
func WithJSONCodec() Option {
	return func(s *Store) {
		s.codec = codecJSON
	}
}

// WithGobCodec configures the store to use Gob encoding for value records (opt-in).
func WithGobCodec() Option {
	return func(s *Store) {
		s.codec = codecGob
	}
}

// Open opens a Pebble-backed durable store. By default, JSON encoding is used.
// It verifies the persisted schema version ("1") and codec marker in store metadata,
// failing fast if a conflict or unsupported schema is detected.
func Open(path string, opts ...Option) (*Store, error) {
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		return nil, err
	}
	store := &Store{
		db:       db,
		leaseTTL: defaultLeaseTTL,
		owner:    fmt.Sprintf("worker-%d", time.Now().UnixNano()),
		sync:     true,
		codec:    codecJSON,
	}
	for _, opt := range opts {
		opt(store)
	}
	if err := store.ensureStoreFormat(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if store.flushEvery > 0 {
		store.stopFlush = make(chan struct{})
		store.flushDone = make(chan struct{})
		go store.flushLoop()
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if s.stopFlush != nil {
		close(s.stopFlush)
		<-s.flushDone
	}
	return s.db.Close()
}

func (s *Store) flushLoop() {
	ticker := time.NewTicker(s.flushEvery)
	defer ticker.Stop()
	defer close(s.flushDone)
	for {
		select {
		case <-ticker.C:
			_ = s.db.Flush()
		case <-s.stopFlush:
			_ = s.db.Flush()
			return
		}
	}
}

func (s *Store) CreateExecution(ctx context.Context, execution *runtime.Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if exists, err := s.exists(execKey(execution.ID)); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("execution already exists: %s", execution.ID)
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeExecution(batch, execution); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) GetExecution(ctx context.Context, id string) (*runtime.Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getExecutionLocked(id)
}

func (s *Store) getExecutionLocked(id string) (*runtime.Execution, error) {
	var execution runtime.Execution
	if err := s.getJSON(execKey(id), &execution); err != nil {
		if errors.Is(err, pebbledb.ErrNotFound) {
			return nil, runtime.ErrExecutionNotFound
		}
		return nil, err
	}
	return cloneExecution(&execution), nil
}

func (s *Store) SaveExecution(ctx context.Context, execution *runtime.Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if exists, err := s.exists(execKey(execution.ID)); err != nil {
		return err
	} else if !exists {
		return runtime.ErrExecutionNotFound
	}
	next := cloneExecution(execution)
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeExecution(batch, next); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) AppendHistory(ctx context.Context, executionID string, entryType string, payload map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, err := s.nextHistorySeq(executionID)
	if err != nil {
		return err
	}
	entry := runtime.HistoryEntry{
		Seq:       seq,
		Type:      entryType,
		Payload:   cloneAnyMap(payload),
		CreatedAt: time.Now().UTC(),
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.putValue(batch, historyKey(executionID, seq), entry); err != nil {
		return err
	}
	if err := putUint(batch, historySeqKey(executionID), seq); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) ListHistory(ctx context.Context, executionID string) ([]runtime.HistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listHistoryLocked(executionID)
}

func (s *Store) listHistoryLocked(executionID string) ([]runtime.HistoryEntry, error) {
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: []byte(historyPrefix(executionID)), UpperBound: []byte(historyPrefixEnd(executionID))})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []runtime.HistoryEntry
	for iter.First(); iter.Valid(); iter.Next() {
		var entry runtime.HistoryEntry
		if err := decodeValue(s.codec, iter.Value(), &entry); err != nil {
			return nil, err
		}
		out = append(out, runtime.HistoryEntry{Seq: entry.Seq, Type: entry.Type, Payload: cloneAnyMap(entry.Payload), CreatedAt: entry.CreatedAt})
	}
	return out, iter.Error()
}

func (s *Store) EnqueueTask(ctx context.Context, task *runtime.ActivityTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneTask(task)
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeTask(batch, next); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) ListTasks(ctx context.Context, executionID string) ([]*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: []byte(taskPrefix(executionID)), UpperBound: []byte(taskPrefixEnd(executionID))})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []*runtime.ActivityTask
	for iter.First(); iter.Valid(); iter.Next() {
		var task runtime.ActivityTask
		if err := decodeValue(s.codec, iter.Value(), &task); err != nil {
			return nil, err
		}
		out = append(out, cloneTask(&task))
	}
	return out, iter.Error()
}

func (s *Store) PollTask(ctx context.Context, executionID string) (*runtime.ActivityTask, error) {
	task, err := s.LeaseTask(ctx, executionID, s.owner, s.leaseTTL)
	if errors.Is(err, errNoTask) {
		return nil, nil
	}
	return task, err
}

func (s *Store) PollTaskWithLease(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*runtime.ActivityTask, error) {
	task, err := s.LeaseTask(ctx, executionID, workerID, leaseTTL)
	if errors.Is(err, errNoTask) {
		return nil, nil
	}
	return task, err
}

func (s *Store) UpdateTask(ctx context.Context, task *runtime.ActivityTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if exists, err := s.exists(taskKey(task.ExecutionID, task.ID)); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("task not found: %s", task.ID)
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeTask(batch, cloneTask(task)); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) HeartbeatTask(ctx context.Context, taskID string, workerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.getTaskByIDLocked(taskID)
	if err != nil {
		return err
	}
	if workerID != "" && task.LockedBy != workerID {
		return fmt.Errorf("task %s locked by %s", taskID, task.LockedBy)
	}
	now := time.Now().UTC()
	if !task.LockedUntil.IsZero() {
		task.LockedUntil = now.Add(s.leaseTTL)
	}
	task.UpdatedAt = now
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeTask(batch, task); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) CompleteTask(ctx context.Context, taskID string, result map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.getTaskByIDLocked(taskID)
	if err != nil {
		return err
	}
	task.Status = runtime.TaskCompleted
	task.Result = cloneAnyMap(result)
	task.Error = ""
	task.LockedBy = ""
	task.LockedUntil = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeTask(batch, task); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) FailTask(ctx context.Context, taskID string, errorMessage string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.getTaskByIDLocked(taskID)
	if err != nil {
		return err
	}
	task.Status = runtime.TaskFailed
	task.Error = errorMessage
	task.LockedBy = ""
	task.LockedUntil = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := s.writeTask(batch, task); err != nil {
		return err
	}
	return batch.Commit(s.writeOptions())
}

func (s *Store) FindTask(ctx context.Context, executionID string, ruleName string, activityName string, idempotencyKey string) (*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findTaskLocked(executionID, ruleName, activityName, idempotencyKey)
}

func (s *Store) findTaskLocked(executionID string, ruleName string, activityName string, idempotencyKey string) (*runtime.ActivityTask, error) {
	var taskID string
	if err := s.getJSON(taskDedupKey(executionID, ruleName, activityName, idempotencyKey), &taskID); err != nil {
		if errors.Is(err, pebbledb.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var task runtime.ActivityTask
	if err := s.getJSON(taskKey(executionID, taskID), &task); err != nil {
		if errors.Is(err, pebbledb.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return cloneTask(&task), nil
}

func (s *Store) NextTaskSeq(ctx context.Context, executionID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextTaskSeqLocked(executionID)
}

func (s *Store) nextTaskSeqLocked(executionID string) (int, error) {
	seq, err := s.getUint(taskSeqKey(executionID))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return seq + 1, nil
}

var errNoTask = errors.New("no pending task")

func (s *Store) LeaseTask(ctx context.Context, executionID string, owner string, ttl time.Duration) (*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner == "" {
		owner = s.owner
	}
	if ttl <= 0 {
		ttl = s.leaseTTL
	}
	now := time.Now().UTC()
	task, err := s.nextPendingTaskLocked(executionID, now)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errNoTask
	}
	task.Status = runtime.TaskRunning
	task.Attempt++
	task.LockedBy = owner
	task.LockedUntil = now.Add(ttl)
	task.UpdatedAt = now
	batch := s.db.NewBatch()
	if err := s.writeTask(batch, task); err != nil {
		_ = batch.Close()
		return nil, err
	}
	if err := batch.Commit(s.writeOptions()); err != nil {
		_ = batch.Close()
		return nil, err
	}
	_ = batch.Close()
	return cloneTask(task), nil
}

func (s *Store) RecoverExpiredLeases(ctx context.Context, executionID string, ttl time.Duration) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ttl <= 0 {
		ttl = s.leaseTTL
	}
	now := time.Now().UTC()
	tasks, err := s.listTasksByStatusLocked(executionID, runtime.TaskRunning)
	if err != nil {
		return 0, err
	}
	batch := s.db.NewBatch()
	defer batch.Close()
	recovered := 0
	for _, task := range tasks {
		if task.Status != runtime.TaskRunning {
			continue
		}
		expired := false
		if ttl > 0 {
			expired = !task.UpdatedAt.Add(ttl).After(now)
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
		if err := s.writeTask(batch, task); err != nil {
			return 0, err
		}
		recovered++
	}
	if recovered == 0 {
		return 0, nil
	}
	return recovered, batch.Commit(s.writeOptions())
}

func (s *Store) writeOptions() *pebbledb.WriteOptions {
	if s.sync {
		return pebbledb.Sync
	}
	return pebbledb.NoSync
}

func (s *Store) listTasksLocked(executionID string) ([]*runtime.ActivityTask, error) {
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: []byte(taskPrefix(executionID)), UpperBound: []byte(taskPrefixEnd(executionID))})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []*runtime.ActivityTask
	for iter.First(); iter.Valid(); iter.Next() {
		var task runtime.ActivityTask
		if err := decodeValue(s.codec, iter.Value(), &task); err != nil {
			return nil, err
		}
		out = append(out, &task)
	}
	return out, iter.Error()
}

func (s *Store) nextPendingTaskLocked(executionID string, now time.Time) (*runtime.ActivityTask, error) {
	tasks, err := s.listTasksByStatusLocked(executionID, runtime.TaskPending)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	task := tasks[0]
	if !task.NextAttemptAt.IsZero() && task.NextAttemptAt.After(now) {
		return nil, nil
	}
	return task, nil
}

func (s *Store) listTasksByStatusLocked(executionID string, status runtime.TaskStatus) ([]*runtime.ActivityTask, error) {
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: []byte(taskStatusPrefix(executionID, status)), UpperBound: []byte(taskStatusPrefixEnd(executionID, status))})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	out := []*runtime.ActivityTask{}
	for iter.First(); iter.Valid(); iter.Next() {
		var taskID string
		if err := decodeValue(s.codec, iter.Value(), &taskID); err != nil {
			return nil, err
		}
		task, err := s.getTaskByIDLocked(taskID)
		if err != nil {
			continue
		}
		if task.ExecutionID != executionID || task.Status != status {
			continue
		}
		out = append(out, task)
	}
	return out, iter.Error()
}

func (s *Store) getTaskByIDLocked(taskID string) (*runtime.ActivityTask, error) {
	var executionID string
	if err := s.getJSON(taskIDKey(taskID), &executionID); err == nil {
		var task runtime.ActivityTask
		if err := s.getJSON(taskKey(executionID, taskID), &task); err != nil {
			return nil, err
		}
		return cloneTask(&task), nil
	} else if !errors.Is(err, pebbledb.ErrNotFound) {
		return nil, err
	}
	return nil, fmt.Errorf("task not found: %s", taskID)
}

func (s *Store) writeExecution(batch *pebbledb.Batch, execution *runtime.Execution) error {
	return s.putValue(batch, execKey(execution.ID), execution)
}

func (s *Store) writeTask(batch *pebbledb.Batch, task *runtime.ActivityTask) error {
	var old runtime.ActivityTask
	if err := s.getJSON(taskKey(task.ExecutionID, task.ID), &old); err == nil {
		if err := batch.Delete([]byte(taskStatusKey(&old)), pebbledb.NoSync); err != nil {
			return err
		}
	} else if !errors.Is(err, pebbledb.ErrNotFound) {
		return err
	}
	if err := s.putValue(batch, taskKey(task.ExecutionID, task.ID), task); err != nil {
		return err
	}
	if err := s.putValue(batch, taskStatusKey(task), task.ID); err != nil {
		return err
	}
	if err := s.putValue(batch, taskIDKey(task.ID), task.ExecutionID); err != nil {
		return err
	}
	if err := s.putValue(batch, taskDedupKey(task.ExecutionID, task.RuleName, task.ActivityName, task.IdempotencyKey), task.ID); err != nil {
		return err
	}
	seq := taskSeqFromID(task.ID)
	if seq > 0 {
		current, err := s.getUint(taskSeqKey(task.ExecutionID))
		if err != nil && !errors.Is(err, pebbledb.ErrNotFound) {
			return err
		}
		if seq > current {
			return putUint(batch, taskSeqKey(task.ExecutionID), seq)
		}
	}
	return nil
}

func (s *Store) exists(key string) (bool, error) {
	value, closer, err := s.db.Get([]byte(key))
	if err != nil {
		if errors.Is(err, pebbledb.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	_ = value
	return true, closer.Close()
}

func (s *Store) getJSON(key string, out any) error {
	value, closer, err := s.db.Get([]byte(key))
	if err != nil {
		return err
	}
	defer closer.Close()
	return decodeValue(s.codec, value, out)
}

func (s *Store) putValue(batch *pebbledb.Batch, key string, value any) error {
	data, err := encodeValue(s.codec, value)
	if err != nil {
		return err
	}
	return batch.Set([]byte(key), data, pebbledb.NoSync)
}

func (s *Store) getUint(key string) (int, error) {
	value, closer, err := s.db.Get([]byte(key))
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	n, err := strconv.Atoi(string(value))
	return n, err
}

func putUint(batch *pebbledb.Batch, key string, value int) error {
	return batch.Set([]byte(key), []byte(strconv.Itoa(value)), pebbledb.NoSync)
}

func (s *Store) nextHistorySeq(executionID string) (int, error) {
	seq, err := s.getUint(historySeqKey(executionID))
	if errors.Is(err, pebbledb.ErrNotFound) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return seq + 1, nil
}

func execKey(id string) string                   { return "exec/" + escape(id) }
func historySeqKey(executionID string) string    { return "hseq/" + escape(executionID) }
func historyPrefix(executionID string) string    { return "hist/" + escape(executionID) + "/" }
func historyPrefixEnd(executionID string) string { return prefixEnd(historyPrefix(executionID)) }
func historyKey(executionID string, seq int) string {
	return fmt.Sprintf("%s%020d", historyPrefix(executionID), seq)
}
func taskSeqKey(executionID string) string             { return "tseq/" + escape(executionID) }
func taskPrefix(executionID string) string             { return "task/" + escape(executionID) + "/" }
func taskPrefixEnd(executionID string) string          { return prefixEnd(taskPrefix(executionID)) }
func taskKey(executionID string, taskID string) string { return taskPrefix(executionID) + escape(taskID) }
func taskIDKey(taskID string) string                   { return "taskid/" + escape(taskID) }
func taskStatusPrefix(executionID string, status runtime.TaskStatus) string {
	return "tstatus/" + escape(executionID) + "/" + string(status) + "/"
}
func taskStatusPrefixEnd(executionID string, status runtime.TaskStatus) string {
	return prefixEnd(taskStatusPrefix(executionID, status))
}
func taskStatusKey(task *runtime.ActivityTask) string {
	availableAt := task.CreatedAt
	if task.Status == runtime.TaskPending && !task.NextAttemptAt.IsZero() {
		availableAt = task.NextAttemptAt
	}
	return fmt.Sprintf("%s%020d/%s", taskStatusPrefix(task.ExecutionID, task.Status), availableAt.UnixNano(), escape(task.ID))
}
func taskDedupKey(executionID string, ruleName string, activityName string, key string) string {
	return "tdedup/" + escape(executionID) + "/" + escape(ruleName) + "/" + escape(activityName) + "/" + escape(key)
}

func prefixEnd(prefix string) string {
	b := []byte(prefix)
	return string(bytes.TrimRight(append(b, 0xff), "\x00"))
}

func escape(value string) string {
	val := strings.ReplaceAll(value, "%", "%25")
	return strings.ReplaceAll(val, "/", "%2f")
}

func taskSeqFromID(taskID string) int {
	idx := strings.LastIndex(taskID, ":")
	if idx < 0 {
		return 0
	}
	seq, _ := strconv.Atoi(taskID[idx+1:])
	return seq
}

// cloneExecution delegates to runtime.CloneExecution for deep copy.
func cloneExecution(in *runtime.Execution) *runtime.Execution {
	return runtime.CloneExecution(in)
}

// cloneContext delegates to runtime.CloneContext for deep copy.
func cloneContext(in map[string]map[string]any) map[string]map[string]any {
	return runtime.CloneContext(in)
}

// cloneFacts delegates to runtime.CloneFacts for deep copy.
func cloneFacts(in map[string]runtime.FactValue) map[string]runtime.FactValue {
	return runtime.CloneFacts(in)
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
		value.Any = cloneAny(value.Any)
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
		Input:          cloneAnyMap(in.Input),
		IdempotencyKey: in.IdempotencyKey,
		Status:         in.Status,
		Attempt:        in.Attempt,
		MaxAttempts:    in.MaxAttempts,
		LockedBy:       in.LockedBy,
		LockedUntil:    in.LockedUntil,
		NextAttemptAt:  in.NextAttemptAt,
		Result:         cloneAnyMap(in.Result),
		Error:          in.Error,
		CreatedAt:      in.CreatedAt,
		UpdatedAt:      in.UpdatedAt,
	}
}

// cloneAnyMap delegates to runtime.CloneAnyMap for deep copy.
func cloneAnyMap(in map[string]any) map[string]any {
	return runtime.CloneAnyMap(in)
}

// cloneAny delegates to runtime.CloneAny for deep copy.
func cloneAny(value any) any {
	return runtime.CloneAny(value)
}
