package adgo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	corepg "github.com/Homiakus/axiom/internal/store/postgres"
)

type PostgresStoreOption func(*PostgresStore)

func WithPostgresAutoMigrate(auto bool) PostgresStoreOption {
	return func(s *PostgresStore) { s.autoMigrate = auto }
}

// PostgresStore is a high-throughput multi-host durable backend for ADGO.
// It uses row-level locking (SELECT ... FOR UPDATE) for transactional CAS,
// immutable version tables for auditability, and supports full execution catalog,
// inbox deduplication, retention pruning, and multi-process coordination.
type PostgresStore struct {
	db          *sql.DB
	autoMigrate bool
}

// OpenPostgresStore opens an ADGO PostgreSQL store from a driver name and connection string.
func OpenPostgresStore(driverName, dataSourceName string, options ...PostgresStoreOption) (*PostgresStore, error) {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("adgo/postgres: open database: %w", err)
	}
	return OpenPostgresStoreFromDB(db, options...)
}

// OpenPostgresStoreFromDB wraps an existing *sql.DB connection pool as an ADGO PostgreSQL store.
func OpenPostgresStoreFromDB(db *sql.DB, options ...PostgresStoreOption) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("adgo/postgres: db cannot be nil")
	}
	s := &PostgresStore{
		db:          db,
		autoMigrate: true,
	}
	for _, opt := range options {
		opt(s)
	}
	if s.autoMigrate {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := corepg.Migrate(ctx, s.db); err != nil {
			return nil, fmt.Errorf("adgo/postgres: auto-migrate: %w", err)
		}
	}
	return s, nil
}

func (s *PostgresStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *PostgresStore) DB() *sql.DB {
	return s.db
}

func (s *PostgresStore) Create(ctx context.Context, execution *Execution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if execution == nil || execution.ID == "" {
		return fmt.Errorf("adgo: execution is required")
	}
	copy, err := cloneExecution(execution)
	if err != nil {
		return err
	}
	state, err := json.Marshal(copy)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	execQuery := `
INSERT INTO axiom_executions (id, version, status, plan_id, plan_digest, state, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err = tx.ExecContext(ctx, execQuery, copy.ID, copy.Version, string(copy.Status), copy.PlanID, copy.PlanDigest, state, now, now)
	if err != nil {
		return ErrExecutionExists
	}

	vQuery := `
INSERT INTO axiom_execution_versions (id, version, state, committed_at)
VALUES ($1, $2, $3, $4)`
	if _, err := tx.ExecContext(ctx, vQuery, copy.ID, copy.Version, state, now); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *PostgresStore) Load(ctx context.Context, id string) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := "SELECT state FROM axiom_executions WHERE id = $1"
	var raw []byte
	err := s.db.QueryRowContext(ctx, query, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}

	var execution Execution
	if err := json.Unmarshal(raw, &execution); err != nil {
		return nil, fmt.Errorf("adgo/postgres: decode execution: %w", err)
	}
	ensureExecution(&execution)
	return &execution, nil
}

func (s *PostgresStore) Commit(ctx context.Context, id string, expected uint64, mutate func(*Execution) error) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	lockQuery := "SELECT version, state FROM axiom_executions WHERE id = $1 FOR UPDATE"
	var curVersion int64
	var rawState []byte
	err = tx.QueryRowContext(ctx, lockQuery, id).Scan(&curVersion, &rawState)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}

	if uint64(curVersion) != expected {
		return nil, ErrConflict
	}

	var cur Execution
	if err := json.Unmarshal(rawState, &cur); err != nil {
		return nil, err
	}
	ensureExecution(&cur)

	next, err := cloneExecution(&cur)
	if err != nil {
		return nil, err
	}
	if err := mutate(next); err != nil {
		return nil, err
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC()

	nextState, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}

	updateQuery := `
UPDATE axiom_executions
SET version = $1, status = $2, state = $3, updated_at = $4
WHERE id = $5 AND version = $6`
	res, err := tx.ExecContext(ctx, updateQuery, next.Version, string(next.Status), nextState, next.UpdatedAt, next.ID, curVersion)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrConflict
	}

	vQuery := `
INSERT INTO axiom_execution_versions (id, version, state, committed_at)
VALUES ($1, $2, $3, $4)`
	if _, err := tx.ExecContext(ctx, vQuery, next.ID, next.Version, nextState, next.UpdatedAt); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cloneExecution(next)
}

func (s *PostgresStore) PutInbox(ctx context.Context, id string, e Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.ID == "" {
		return fmt.Errorf("adgo: event id is required")
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	checkQuery := "SELECT EXISTS(SELECT 1 FROM axiom_executions WHERE id = $1)"
	if err := tx.QueryRowContext(ctx, checkQuery, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrExecutionNotFound
	}

	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}

	query := `
INSERT INTO axiom_inbox (execution_id, event_id, event_type, payload, received_at)
VALUES ($1, $2, $3, $4, $5)`
	_, _ = tx.ExecContext(ctx, query, id, e.ID, e.Type, payload, e.At.UTC())
	return tx.Commit()
}

func (s *PostgresStore) ListInbox(ctx context.Context, id string) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var exists bool
	checkQuery := "SELECT EXISTS(SELECT 1 FROM axiom_executions WHERE id = $1)"
	if err := s.db.QueryRowContext(ctx, checkQuery, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrExecutionNotFound
	}

	query := `
SELECT event_id, event_type, payload, received_at
FROM axiom_inbox
WHERE execution_id = $1
ORDER BY received_at ASC, event_id ASC`
	rows, err := s.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var eventID, eventType string
		var rawPayload []byte
		var receivedAt time.Time
		if err := rows.Scan(&eventID, &eventType, &rawPayload, &receivedAt); err != nil {
			return nil, err
		}
		var ev Event
		if err := json.Unmarshal(rawPayload, &ev); err != nil {
			return nil, fmt.Errorf("decode inbox event %s: %w", eventID, err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortEvents(out)
	return out, nil
}

func (s *PostgresStore) AckInbox(ctx context.Context, id string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	query := "DELETE FROM axiom_inbox WHERE execution_id = $1 AND event_id = $2"
	for _, eid := range ids {
		if _, err := tx.ExecContext(ctx, query, id, eid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) ListExecutionIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := "SELECT id FROM axiom_executions ORDER BY id ASC"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *PostgresStore) ListVersions(ctx context.Context, id string) ([]*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Verify execution exists
	var exists bool
	checkQuery := "SELECT EXISTS(SELECT 1 FROM axiom_executions WHERE id = $1)"
	if err := s.db.QueryRowContext(ctx, checkQuery, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrExecutionNotFound
	}

	query := "SELECT state FROM axiom_execution_versions WHERE id = $1 ORDER BY version ASC"
	rows, err := s.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Execution{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var e Execution
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		ensureExecution(&e)
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func (s *PostgresStore) DeleteExecution(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM axiom_inbox WHERE execution_id = $1", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM axiom_execution_versions WHERE id = $1", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM axiom_executions WHERE id = $1", id); err != nil {
		return err
	}
	return tx.Commit()
}

// PostgresProviderHealthStore implements ProviderHealthStore on PostgreSQL.
type PostgresProviderHealthStore struct {
	db *sql.DB
}

func NewPostgresProviderHealthStore(db *sql.DB) *PostgresProviderHealthStore {
	return &PostgresProviderHealthStore{db: db}
}

func (s *PostgresProviderHealthStore) LoadProviderHealth(ctx context.Context, capability, provider string) (ProviderHealth, error) {
	if err := ctx.Err(); err != nil {
		return ProviderHealth{}, err
	}
	query := "SELECT state FROM axiom_provider_health WHERE capability = $1 AND provider = $2"
	var raw []byte
	err := s.db.QueryRowContext(ctx, query, capability, provider).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderHealth{}, ErrProviderHealthNotFound
	}
	if err != nil {
		return ProviderHealth{}, err
	}
	var health ProviderHealth
	if err := json.Unmarshal(raw, &health); err != nil {
		return ProviderHealth{}, err
	}
	return health, nil
}

func (s *PostgresProviderHealthStore) UpdateProviderHealth(ctx context.Context, capability, provider string, mutate func(*ProviderHealth)) (ProviderHealth, error) {
	if err := ctx.Err(); err != nil {
		return ProviderHealth{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ProviderHealth{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var health ProviderHealth
	query := "SELECT state FROM axiom_provider_health WHERE capability = $1 AND provider = $2 FOR UPDATE"
	var raw []byte
	err = tx.QueryRowContext(ctx, query, capability, provider).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		health = ProviderHealth{Capability: capability, Provider: provider}
	} else if err != nil {
		return ProviderHealth{}, err
	} else {
		_ = json.Unmarshal(raw, &health)
	}

	mutate(&health)
	now := time.Now().UTC()
	stBytes, err := json.Marshal(health)
	if err != nil {
		return ProviderHealth{}, err
	}

	if health.Capability == "" {
		health.Capability = capability
	}
	if health.Provider == "" {
		health.Provider = provider
	}

	if raw == nil {
		insertQuery := `
INSERT INTO axiom_provider_health (capability, provider, state, updated_at)
VALUES ($1, $2, $3, $4)`
		if _, err := tx.ExecContext(ctx, insertQuery, capability, provider, stBytes, now); err != nil {
			return ProviderHealth{}, err
		}
	} else {
		updateQuery := `
UPDATE axiom_provider_health
SET state = $1, updated_at = $2
WHERE capability = $3 AND provider = $4`
		if _, err := tx.ExecContext(ctx, updateQuery, stBytes, now, capability, provider); err != nil {
			return ProviderHealth{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return ProviderHealth{}, err
	}
	return health, nil
}

func (s *PostgresProviderHealthStore) ListProviderHealth(ctx context.Context) ([]ProviderHealth, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := "SELECT state FROM axiom_provider_health"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ProviderHealth{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var h ProviderHealth
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortProviderHealth(out)
	return out, nil
}

func (s *PostgresProviderHealthStore) DeleteProviderHealth(ctx context.Context, capability, provider string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	query := "DELETE FROM axiom_provider_health WHERE capability = $1 AND provider = $2"
	_, err := s.db.ExecContext(ctx, query, capability, provider)
	return err
}

// PostgresScheduleStore implements ScheduleStore on PostgreSQL.
type PostgresScheduleStore struct {
	db *sql.DB
}

func NewPostgresScheduleStore(db *sql.DB) *PostgresScheduleStore {
	return &PostgresScheduleStore{db: db}
}

func (s *PostgresScheduleStore) Create(ctx context.Context, schedule *Schedule) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if schedule == nil || schedule.ID == "" {
		return errors.New("adgo: schedule is required")
	}
	raw, err := json.Marshal(schedule)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	query := `
INSERT INTO axiom_schedules (id, version, state, updated_at)
VALUES ($1, $2, $3, $4)`
	_, err = s.db.ExecContext(ctx, query, schedule.ID, schedule.Version, raw, now)
	return err
}

func (s *PostgresScheduleStore) Load(ctx context.Context, id string) (*Schedule, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := "SELECT state FROM axiom_schedules WHERE id = $1"
	var raw []byte
	err := s.db.QueryRowContext(ctx, query, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScheduleNotFound
	}
	if err != nil {
		return nil, err
	}
	var schedule Schedule
	if err := json.Unmarshal(raw, &schedule); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (s *PostgresScheduleStore) List(ctx context.Context) ([]*Schedule, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := "SELECT state FROM axiom_schedules"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Schedule{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var sched Schedule
		if err := json.Unmarshal(raw, &sched); err != nil {
			return nil, err
		}
		out = append(out, &sched)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *PostgresScheduleStore) Commit(ctx context.Context, id string, expected uint64, mutate func(*Schedule) error) (*Schedule, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	query := "SELECT version, state FROM axiom_schedules WHERE id = $1 FOR UPDATE"
	var curVersion int64
	var raw []byte
	err = tx.QueryRowContext(ctx, query, id).Scan(&curVersion, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScheduleNotFound
	}
	if err != nil {
		return nil, err
	}
	if uint64(curVersion) != expected {
		return nil, ErrConflict
	}

	var cur Schedule
	if err := json.Unmarshal(raw, &cur); err != nil {
		return nil, err
	}
	if err := mutate(&cur); err != nil {
		return nil, err
	}
	cur.Version++
	now := time.Now().UTC()
	nextRaw, err := json.Marshal(&cur)
	if err != nil {
		return nil, err
	}

	updateQuery := `
UPDATE axiom_schedules
SET version = $1, state = $2, updated_at = $3
WHERE id = $4 AND version = $5`
	res, err := tx.ExecContext(ctx, updateQuery, cur.Version, nextRaw, now, id, curVersion)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return nil, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &cur, nil
}
