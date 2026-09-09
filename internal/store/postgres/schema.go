package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	CurrentSchemaVersion = 1
	advisoryLockKey      = 0x4158494F4D // "AXIOM"
)

type Migration struct {
	Version int
	Name    string
	SQL     string
}

var Migrations = []Migration{
	{
		Version: 1,
		Name:    "initial_core_and_adgo_tables",
		SQL: `
CREATE TABLE IF NOT EXISTS axiom_schema_migrations (
    version INT PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS axiom_executions (
    id TEXT PRIMARY KEY,
    version BIGINT NOT NULL,
    status TEXT NOT NULL,
    plan_id TEXT,
    plan_digest TEXT,
    state JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_axiom_executions_status ON axiom_executions(status);
CREATE INDEX IF NOT EXISTS idx_axiom_executions_plan ON axiom_executions(plan_id, plan_digest);

CREATE TABLE IF NOT EXISTS axiom_execution_versions (
    id TEXT NOT NULL,
    version BIGINT NOT NULL,
    state JSONB NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id, version)
);

CREATE TABLE IF NOT EXISTS axiom_inbox (
    execution_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (execution_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_axiom_inbox_order ON axiom_inbox(execution_id, received_at ASC, event_id ASC);

CREATE TABLE IF NOT EXISTS axiom_history (
    execution_id TEXT NOT NULL,
    seq BIGINT NOT NULL,
    entry_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (execution_id, seq)
);

CREATE TABLE IF NOT EXISTS axiom_tasks (
    task_id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    activity TEXT NOT NULL,
    attempt INT NOT NULL,
    status TEXT NOT NULL,
    lease_owner TEXT,
    lease_until TIMESTAMPTZ,
    not_before TIMESTAMPTZ,
    idempotency_key TEXT,
    input JSONB,
    result JSONB,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_axiom_tasks_exec_status ON axiom_tasks(execution_id, status);
CREATE INDEX IF NOT EXISTS idx_axiom_tasks_poll ON axiom_tasks(status, not_before, lease_until, attempt, created_at);

CREATE TABLE IF NOT EXISTS axiom_schedules (
    id TEXT PRIMARY KEY,
    version BIGINT NOT NULL,
    state JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS axiom_provider_health (
    capability TEXT NOT NULL,
    provider TEXT NOT NULL,
    state JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (capability, provider)
);

CREATE TABLE IF NOT EXISTS axiom_admission_leases (
    resource TEXT NOT NULL,
    lease_id TEXT NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (resource, lease_id)
);
`,
	},
}

// Migrate executes all pending schema migrations inside a transactional lock.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("axiom/postgres: begin migration tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Acquire advisory transaction lock to coordinate multi-host migrations
	_, _ = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey)

	// Ensure migrations tracking table exists
	createMigrationTableSQL := `
CREATE TABLE IF NOT EXISTS axiom_schema_migrations (
    version INT PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL
);`
	if _, err := tx.ExecContext(ctx, createMigrationTableSQL); err != nil {
		return fmt.Errorf("axiom/postgres: create schema migrations table: %w", err)
	}

	for _, m := range Migrations {
		var exists bool
		query := "SELECT EXISTS(SELECT 1 FROM axiom_schema_migrations WHERE version = $1)"
		if err := tx.QueryRowContext(ctx, query, m.Version).Scan(&exists); err != nil {
			return fmt.Errorf("axiom/postgres: check migration %d: %w", m.Version, err)
		}
		if exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("axiom/postgres: apply migration %d (%s): %w", m.Version, m.Name, err)
		}
		recordSQL := "INSERT INTO axiom_schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)"
		if _, err := tx.ExecContext(ctx, recordSQL, m.Version, m.Name, time.Now().UTC()); err != nil {
			return fmt.Errorf("axiom/postgres: record migration %d: %w", m.Version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("axiom/postgres: commit migration tx: %w", err)
	}
	return nil
}
