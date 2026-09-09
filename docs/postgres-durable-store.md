# PostgreSQL Durable Store

## Overview

Axiom provides a first-party networked PostgreSQL durable store for multi-host, highly-available production deployments. It provides authoritative distributed persistence for both the Core Axiom runtime (`runtime.Store`) and the ADGO workflow engine (`adgo.Store`).

## Architecture & Core Guarantees

| Capability | PostgreSQL Mechanism | Guarantees |
|---|---|---|
| **Transactional CAS** | `SELECT ... FOR UPDATE` + optimistic version check | Atomic, conflict-free state mutations per execution ID. Stale mutations fail with `ErrConflict`. |
| **Multi-Host Worker Fencing** | `FOR UPDATE SKIP LOCKED` on `axiom_tasks` | Non-blocking worker task polling; prevents lock convoying. Stale/expired workers are fenced out on heartbeat or completion. |
| **Immutable Version Audit** | `axiom_execution_versions` | Every single committed execution version is preserved immutably with timestamp and state snapshot. |
| **Inbox Deduplication** | `PRIMARY KEY (execution_id, event_id)` on `axiom_inbox` | Idempotent event receipt; duplicates are ignored without duplicate processing. |
| **Schedules & Triggers** | `axiom_schedules` with CAS updates | Multi-host distributed cron and interval schedules. |
| **Adaptive Provider Routing** | `axiom_provider_health` | Durable shared provider health and latency metrics across coordinator instances. |
| **Distributed Migrations** | `pg_advisory_xact_lock` | Automatic forward-only schema migrations safely coordinated across all cluster nodes on startup. |

## Schema Design

### Core Tables
1. **`axiom_schema_migrations`**: Tracks applied schema versions.
2. **`axiom_executions`**: Current execution state snapshot, plan identity, and version counter.
3. **`axiom_execution_versions`**: Append-only immutable version ledger.
4. **`axiom_inbox`**: Event buffer with idempotent deduplication.
5. **`axiom_history`**: Sequentially numbered historical event entries.
6. **`axiom_tasks`**: Activity tasks with worker lease owner, lease deadline, retry backoff (`not_before`), and result payloads.
7. **`axiom_schedules`**: Durable schedule definitions and fire times.
8. **`axiom_provider_health`**: Adaptive routing health, successes, failures, and EWMA metrics.
9. **`axiom_admission_leases`**: Concurrent admission quota leases.

## Usage

### 1. Standard Driver Setup (e.g. pgx or lib/pq)

Consumers may use any standard Go PostgreSQL driver with `database/sql`:

```go
import (
    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/Homiakus/axiom/store/postgres"
)

// Open from DSN
store, err := postgres.Open("pgx", "postgres://user:pass@localhost:5432/axiom?sslmode=disable")
if err != nil {
    log.Fatal(err)
}
defer store.Close()
```

Or wrap an existing `*sql.DB` connection pool (e.g. from cloud RDS / Cloud SQL):

```go
store, err := postgres.OpenFromDB(existingDB, postgres.WithLeaseTTL(30*time.Second))
```

### 2. Using with ADGO Engine

```go
import (
    "github.com/Homiakus/axiom/adgo"
)

pgStore, err := adgo.OpenPostgresStore("pgx", connStr)
if err != nil {
    log.Fatal(err)
}
defer pgStore.Close()

// Create production configuration using PostgreSQL
config := adgo.DefaultProductionConfig("")
config.Store = pgStore

engine, err := adgo.NewEngine(plan, pgStore, registry)
```

### 3. Worker Fencing Semantics

- Tasks are claimed atomically using `FOR UPDATE SKIP LOCKED`.
- While running, workers must heartbeat within `leaseTTL`.
- If a worker becomes unresponsive or experiences network partition, another worker claims the expired task after `lease_until`.
- When the partitioned worker resumes, any attempt to heartbeat or complete the task is rejected with stale worker fencing.
