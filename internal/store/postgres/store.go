package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
)

const (
	defaultLeaseTTL = 30 * time.Second
)

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

func WithAutoMigrate(auto bool) Option {
	return func(s *Store) {
		s.autoMigrate = auto
	}
}

type Store struct {
	db          *sql.DB
	leaseTTL    time.Duration
	owner       string
	autoMigrate bool
}

// Open opens a PostgreSQL durable store from a driver name and data source name.
func Open(driverName, dataSourceName string, opts ...Option) (*Store, error) {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("axiom/postgres: open database: %w", err)
	}
	return OpenFromDB(db, opts...)
}

// OpenFromDB wraps an existing *sql.DB connection pool as an Axiom PostgreSQL store.
func OpenFromDB(db *sql.DB, opts ...Option) (*Store, error) {
	if db == nil {
		return nil, errors.New("axiom/postgres: db cannot be nil")
	}
	s := &Store{
		db:          db,
		leaseTTL:    defaultLeaseTTL,
		owner:       fmt.Sprintf("worker-%d", time.Now().UnixNano()),
		autoMigrate: true,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.autoMigrate {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := Migrate(ctx, s.db); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) CreateExecution(ctx context.Context, execution *runtime.Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if execution == nil || execution.ID == "" {
		return errors.New("axiom/postgres: execution is required")
	}
	clone := cloneRuntimeExecution(execution)
	state, err := json.Marshal(clone)
	if err != nil {
		return fmt.Errorf("axiom/postgres: marshal execution state: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	execQuery := `
INSERT INTO axiom_executions (id, version, status, plan_id, plan_digest, state, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err = tx.ExecContext(ctx, execQuery, clone.ID, clone.Version, string(clone.Status), "", "", state, now, now)
	if err != nil {
		return fmt.Errorf("execution already exists: %s: %w", clone.ID, err)
	}

	vQuery := `
INSERT INTO axiom_execution_versions (id, version, state, committed_at)
VALUES ($1, $2, $3, $4)`
	if _, err := tx.ExecContext(ctx, vQuery, clone.ID, clone.Version, state, now); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetExecution(ctx context.Context, id string) (*runtime.Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := "SELECT state FROM axiom_executions WHERE id = $1"
	var raw []byte
	err := s.db.QueryRowContext(ctx, query, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, runtime.ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}

	var execution runtime.Execution
	if err := json.Unmarshal(raw, &execution); err != nil {
		return nil, fmt.Errorf("axiom/postgres: decode execution: %w", err)
	}
	ensureRuntimeExecution(&execution)
	return &execution, nil
}

func (s *Store) SaveExecution(ctx context.Context, execution *runtime.Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if execution == nil || execution.ID == "" {
		return errors.New("axiom/postgres: execution is required")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var curVersion int64
	var rawState []byte
	lockQuery := "SELECT version, state FROM axiom_executions WHERE id = $1 FOR UPDATE"
	err = tx.QueryRowContext(ctx, lockQuery, execution.ID).Scan(&curVersion, &rawState)
	if errors.Is(err, sql.ErrNoRows) {
		return runtime.ErrExecutionNotFound
	}
	if err != nil {
		return err
	}

	next := cloneRuntimeExecution(execution)
	next.Version = int(curVersion) + 1
	next.UpdatedAt = time.Now().UTC()

	nextState, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("axiom/postgres: marshal execution: %w", err)
	}

	updateQuery := `
UPDATE axiom_executions
SET version = $1, status = $2, state = $3, updated_at = $4
WHERE id = $5 AND version = $6`
	res, err := tx.ExecContext(ctx, updateQuery, next.Version, string(next.Status), nextState, next.UpdatedAt, next.ID, curVersion)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("axiom/postgres: conflict saving execution")
	}

	vQuery := `
INSERT INTO axiom_execution_versions (id, version, state, committed_at)
VALUES ($1, $2, $3, $4)`
	if _, err := tx.ExecContext(ctx, vQuery, next.ID, next.Version, nextState, next.UpdatedAt); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) AppendHistory(ctx context.Context, executionID string, entryType string, payload map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var maxSeq int64
	seqQuery := "SELECT COALESCE(MAX(seq), 0) FROM axiom_history WHERE execution_id = $1"
	if err := tx.QueryRowContext(ctx, seqQuery, executionID).Scan(&maxSeq); err != nil {
		return err
	}
	nextSeq := maxSeq + 1

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	insertQuery := `
INSERT INTO axiom_history (execution_id, seq, entry_type, payload, created_at)
VALUES ($1, $2, $3, $4, $5)`
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, insertQuery, executionID, nextSeq, entryType, payloadBytes, now); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) ListHistory(ctx context.Context, executionID string) ([]runtime.HistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := `
SELECT entry_type, payload, created_at
FROM axiom_history
WHERE execution_id = $1
ORDER BY seq ASC`
	rows, err := s.db.QueryContext(ctx, query, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []runtime.HistoryEntry
	seq := 1
	for rows.Next() {
		var entryType string
		var rawPayload []byte
		var createdAt time.Time
		if err := rows.Scan(&entryType, &rawPayload, &createdAt); err != nil {
			return nil, err
		}
		var payload map[string]any
		if len(rawPayload) > 0 {
			_ = json.Unmarshal(rawPayload, &payload)
		}
		out = append(out, runtime.HistoryEntry{
			Seq:       seq,
			Type:      entryType,
			Payload:   payload,
			CreatedAt: createdAt.UTC(),
		})
		seq++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) EnqueueTask(ctx context.Context, task *runtime.ActivityTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task == nil || task.ID == "" {
		return errors.New("axiom/postgres: task is required")
	}
	inputBytes, _ := json.Marshal(task.Input)
	resultBytes, _ := json.Marshal(task.Result)
	now := time.Now().UTC()
	var notBefore *time.Time
	if !task.NextAttemptAt.IsZero() {
		nb := task.NextAttemptAt.UTC()
		notBefore = &nb
	}
	var leaseUntil *time.Time
	if !task.LockedUntil.IsZero() {
		lu := task.LockedUntil.UTC()
		leaseUntil = &lu
	}

	query := `
INSERT INTO axiom_tasks (
    task_id, execution_id, node_id, activity, attempt, status,
    lease_owner, lease_until, not_before, idempotency_key, input, result, error, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`
	_, err := s.db.ExecContext(ctx, query,
		task.ID, task.ExecutionID, task.RuleName, task.ActivityName, task.Attempt, string(task.Status),
		task.LockedBy, leaseUntil, notBefore, task.IdempotencyKey, inputBytes, resultBytes, task.Error, now, now,
	)
	return err
}

func (s *Store) ListTasks(ctx context.Context, executionID string) ([]*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := `
SELECT task_id, execution_id, node_id, activity, attempt, status,
       lease_owner, lease_until, not_before, idempotency_key, input, result, error, created_at, updated_at
FROM axiom_tasks
WHERE execution_id = $1
ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, query, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*runtime.ActivityTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) PollTask(ctx context.Context, executionID string) (*runtime.ActivityTask, error) {
	return s.PollTaskWithLease(ctx, executionID, s.owner, s.leaseTTL)
}

func (s *Store) PollTaskWithLease(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*runtime.ActivityTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseTTL)

	// SKIP LOCKED prevents concurrent workers from serializing on locked queue items
	query := `
SELECT task_id, execution_id, node_id, activity, attempt, status,
       lease_owner, lease_until, not_before, idempotency_key, input, result, error, created_at, updated_at
FROM axiom_tasks
WHERE execution_id = $1 AND (status = 'pending' OR (status = 'running' AND lease_until <= $2))
  AND (not_before IS NULL OR not_before <= $2)
ORDER BY attempt ASC, created_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED`

	row := s.db.QueryRowContext(ctx, query, executionID, now, workerID, leaseUntil)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	task.Status = runtime.TaskRunning
	task.LockedBy = workerID
	task.LockedUntil = leaseUntil
	task.UpdatedAt = now
	return task, nil
}

func (s *Store) HeartbeatTask(ctx context.Context, taskID string, workerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(s.leaseTTL)
	query := `
UPDATE axiom_tasks
SET lease_until = $1, updated_at = $2
WHERE task_id = $3 AND lease_owner = $4 AND status = 'running'`
	res, err := s.db.ExecContext(ctx, query, leaseUntil, now, taskID, workerID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("axiom/postgres: heartbeat rejected: worker is not lease owner or lease expired")
	}
	return nil
}

func (s *Store) RecoverExpiredLeases(ctx context.Context, executionID string, leaseTTL time.Duration) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	cutoff := now.Add(-leaseTTL)
	query := `
UPDATE axiom_tasks
SET status = 'pending', lease_owner = '', lease_until = NULL, updated_at = $1
WHERE execution_id = $2 AND status = 'running' AND lease_until <= $3`
	res, err := s.db.ExecContext(ctx, query, now, executionID, cutoff)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	return int(affected), err
}

func (s *Store) CompleteTask(ctx context.Context, taskID string, result map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resBytes, _ := json.Marshal(result)
	now := time.Now().UTC()
	query := `
UPDATE axiom_tasks
SET status = 'completed', result = $1, error = '', updated_at = $2
WHERE task_id = $3`
	_, err := s.db.ExecContext(ctx, query, resBytes, now, taskID)
	return err
}

func (s *Store) FailTask(ctx context.Context, taskID string, errorMessage string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	query := `
UPDATE axiom_tasks
SET status = 'failed', error = $1, updated_at = $2
WHERE task_id = $3`
	_, err := s.db.ExecContext(ctx, query, errorMessage, now, taskID)
	return err
}

func (s *Store) UpdateTask(ctx context.Context, task *runtime.ActivityTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task == nil || task.ID == "" {
		return errors.New("axiom/postgres: task is required")
	}
	now := time.Now().UTC()
	inputBytes, _ := json.Marshal(task.Input)
	resultBytes, _ := json.Marshal(task.Result)
	var notBefore *time.Time
	if !task.NextAttemptAt.IsZero() {
		nb := task.NextAttemptAt.UTC()
		notBefore = &nb
	}
	var leaseUntil *time.Time
	if !task.LockedUntil.IsZero() {
		lu := task.LockedUntil.UTC()
		leaseUntil = &lu
	}

	query := `
UPDATE axiom_tasks
SET attempt = $1, status = $2, lease_owner = $3, lease_until = $4,
    not_before = $5, input = $6, result = $7, error = $8, updated_at = $9
WHERE task_id = $10`
	_, err := s.db.ExecContext(ctx, query,
		task.Attempt, string(task.Status), task.LockedBy, leaseUntil,
		notBefore, inputBytes, resultBytes, task.Error, now, task.ID,
	)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(s rowScanner) (*runtime.ActivityTask, error) {
	var taskID, execID, nodeID, activity, status, leaseOwner, idempotencyKey, errStr string
	var attempt int
	var leaseUntil, notBefore *time.Time
	var inputBytes, resultBytes []byte
	var createdAt, updatedAt time.Time

	err := s.Scan(
		&taskID, &execID, &nodeID, &activity, &attempt, &status,
		&leaseOwner, &leaseUntil, &notBefore, &idempotencyKey, &inputBytes, &resultBytes, &errStr,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	var input map[string]any
	if len(inputBytes) > 0 {
		_ = json.Unmarshal(inputBytes, &input)
	}
	var result map[string]any
	if len(resultBytes) > 0 {
		_ = json.Unmarshal(resultBytes, &result)
	}

	t := &runtime.ActivityTask{
		ID:             taskID,
		ExecutionID:    execID,
		RuleName:       nodeID,
		ActivityName:   activity,
		Attempt:        attempt,
		Status:         runtime.TaskStatus(status),
		LockedBy:       leaseOwner,
		IdempotencyKey: idempotencyKey,
		Input:          input,
		Result:         result,
		Error:          errStr,
		CreatedAt:      createdAt.UTC(),
		UpdatedAt:      updatedAt.UTC(),
	}
	if notBefore != nil {
		t.NextAttemptAt = (*notBefore).UTC()
	}
	if leaseUntil != nil {
		t.LockedUntil = (*leaseUntil).UTC()
	}
	return t, nil
}

func cloneRuntimeExecution(e *runtime.Execution) *runtime.Execution {
	data, err := json.Marshal(e)
	if err != nil {
		return nil
	}
	var out runtime.Execution
	_ = json.Unmarshal(data, &out)
	ensureRuntimeExecution(&out)
	return &out
}

func ensureRuntimeExecution(e *runtime.Execution) {
	if e.Context == nil {
		e.Context = map[string]map[string]any{}
	}
	if e.Computed == nil {
		e.Computed = map[string]any{}
	}
	if e.Facts == nil {
		e.Facts = map[string]runtime.FactValue{}
	}
}

// Transactional store implementation

type txStore struct {
	tx    *sql.Tx
	store *Store
}

func (s *Store) BeginTransaction(ctx context.Context) (runtime.StoreTransaction, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	return &txStore{tx: tx, store: s}, nil
}

func (t *txStore) Commit() error   { return t.tx.Commit() }
func (t *txStore) Rollback() error { return t.tx.Rollback() }

func (t *txStore) CreateExecution(ctx context.Context, execution *runtime.Execution) error {
	return t.store.CreateExecution(ctx, execution)
}
func (t *txStore) GetExecution(ctx context.Context, id string) (*runtime.Execution, error) {
	return t.store.GetExecution(ctx, id)
}
func (t *txStore) SaveExecution(ctx context.Context, execution *runtime.Execution) error {
	return t.store.SaveExecution(ctx, execution)
}
func (t *txStore) AppendHistory(ctx context.Context, executionID string, entryType string, payload map[string]any) error {
	return t.store.AppendHistory(ctx, executionID, entryType, payload)
}
func (t *txStore) ListHistory(ctx context.Context, executionID string) ([]runtime.HistoryEntry, error) {
	return t.store.ListHistory(ctx, executionID)
}
func (t *txStore) EnqueueTask(ctx context.Context, task *runtime.ActivityTask) error {
	return t.store.EnqueueTask(ctx, task)
}
func (t *txStore) ListTasks(ctx context.Context, executionID string) ([]*runtime.ActivityTask, error) {
	return t.store.ListTasks(ctx, executionID)
}
func (t *txStore) PollTask(ctx context.Context, executionID string) (*runtime.ActivityTask, error) {
	return t.store.PollTask(ctx, executionID)
}
func (t *txStore) PollTaskWithLease(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*runtime.ActivityTask, error) {
	return t.store.PollTaskWithLease(ctx, executionID, workerID, leaseTTL)
}
func (t *txStore) HeartbeatTask(ctx context.Context, taskID string, workerID string) error {
	return t.store.HeartbeatTask(ctx, taskID, workerID)
}
func (t *txStore) RecoverExpiredLeases(ctx context.Context, executionID string, leaseTTL time.Duration) (int, error) {
	return t.store.RecoverExpiredLeases(ctx, executionID, leaseTTL)
}
func (t *txStore) CompleteTask(ctx context.Context, taskID string, result map[string]any) error {
	return t.store.CompleteTask(ctx, taskID, result)
}
func (t *txStore) FailTask(ctx context.Context, taskID string, errorMessage string) error {
	return t.store.FailTask(ctx, taskID, errorMessage)
}
func (t *txStore) UpdateTask(ctx context.Context, task *runtime.ActivityTask) error {
	return t.store.UpdateTask(ctx, task)
}
