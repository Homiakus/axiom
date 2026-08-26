# Next Steps & Production Stabilization Status

**Updated:** 2026-08-26  
**Repository:** `github.com/Homiakus/axiom` (`main`)  
**Commit HEAD:** `e6cec70` (100% CI Green, 0 linter issues, full race detector clean)

---

## 1. Current Codebase State

### Completed Milestones

#### Milestone M0 — Core Safety & Governance (100% DONE)
- **P0-001**: Pebble Transaction Pointer & Mutation Isolation (`internal/store/pebble`).
- **P0-002**: JSON Pointer Mutation Safety (`internal/runtime`, `internal/jsonx`).
- **P0-003**: ADGO Defensive Copy & Clone Safety (`adgo.Store`, `adgo.ActivityCache`).
- **P0-004**: CI Guard & Linter Architecture Verification.
- **GOV-001**: Code Review & Multi-Process Concurrency Guidelines.

#### Milestone M1 — Durable Time & Semantic Clocks (100% DONE)
- **TIME-001**: Exhaustive Clock Usage Inventory (`internal/durabletime/inventory.go`, `docs/clock-inventory.md`).
- **TIME-002**: Canonical Semantic Clock Architecture Guard (`TestArchitectureSemanticClockGuard`).
- **TIME-003**: Deterministic Timer & Schedule Semantics (`ManualClock`, zero-drift virtual time).
- **TIME-004**: ADGO Admission Controller & File Lock Clock Injection (`WithMemoryAdmissionClock`, `WithFileAdmissionClock`).
- **TIME-005**: Durable Time Integration Test Suite.

#### Milestone M2 — Storage Integrity & Format Pinning (100% DONE)
- **STORE-001**: ADGO Store Conformance Suite (`RunADGOStoreConformanceSuite` for MemoryStore, FileStore, PebbleStore).
- **STORE-002**: Formalize Context-Cancellation Semantics (`ctx.Err()` fail-fast pre-checks, non-interruptible atomic commits across all stores, `docs/runtime-semantics.md`).
- **STORE-003**: ADGO Pebble Persisted-Format Identity (`meta/adgo-store-schema`, `meta/adgo-store-format`, fail-fast future versions, legacy adoption validation).
- **STORE-004**: Durable Serialized Surfaces Inventory (`internal/durableserial/inventory.go`, `docs/serialized-surfaces.md` covering all 19 persistent structures).
- **STORE-005**: Golden Compatibility Fixtures (`testdata/compat/*`, `internal/durableserial/compat_test.go` verifying backward compatibility and error handling).
- **STORE-006**: Expanded FileStore & Admission Multi-Process Subprocess Tests (`adgo/file_lock_subprocess_test.go` verifying competing committers, process death, stale recovery, and takeover isolation).

#### Milestone M3 — Scaling & Contention Reduction (IN PROGRESS)
- **SCALE-001**: Benchmark Current Core Pebble Transaction Contention (`internal/store/pebble/benchmark_contention_test.go` covering 1-32 concurrent workers, read/write/mixed/same-exec profiles, and latency percentiles).
- **SCALE-002**: Measure Double-Serialization Between Engine and Store (`internal/runtime/engine_concurrency_test.go` verifying executionLocks isolation and concurrency benchmarks).
- **SCALE-003**: Design Conflict/Isolation Model Before Replacing Global Mutex (`docs/TRANSACTION_ISOLATION_DESIGN.md` documenting snapshot semantics, sequence allocation, and cross-execution independence).

---

## 2. Immediate Next Tasks — Milestone M3 (SCALE)

### SCALE-004 — Refactor core Pebble transaction locking
- **Objective**: Refactor PebbleStore to remove transaction-lifetime global mutex holding, using execution-scoped operations while preserving 100% store contracts.
- **Target Files**: `internal/store/pebble/store.go`, `internal/store/pebble/transaction.go`.
- **Verification**: `go test -race ./internal/store/pebble/...` and `TestPebbleStoreContract`.

### SCALE-003 — MemoryStore lock granularity evaluation
- **Objective**: Evaluate per-execution sharded mutexes / sync.Map vs global lock to eliminate goroutine lock contention on high-throughput in-memory execution engines.
- **Target Files**: `internal/store/memory/store.go`, `adgo/store.go`.

### SCALE-004 — ADGO FileStore directory sharding
- **Objective**: Add 2-level directory sharding (e.g. `executions/ab/cd/<encoded_id>/...`) for workspaces containing >100,000 executions to prevent filesystem directory inode scan degradation.
- **Target Files**: `adgo/store.go`, `adgo/store_test.go`.

### SCALE-005 — Flow store batching and incremental append performance
- **Objective**: Ensure `SaveStateAndAppend` batches state snapshot + incremental history + outbox intents in a single synchronous Pebble batch without redundant disk flushes.
- **Target Files**: `flow.go`, `flow_pebble_store.go`.

### SCALE-006 — Comprehensive scaling benchmark suite and regression thresholds
- **Objective**: Implement automated throughput and memory allocation threshold checks in `cmd/axiombench` or root test suite.

---

## 3. Autonomous Execution Rules
1. **1 задача = 1 атомарный логический коммит.**
2. **Локальная верификация перед коммитом:** `golangci-lint run ./...`, `go test ./...`, `go test -race ./...`.
3. **Пуш в `origin/main`** и подтверждение чистой сборки.
4. **Обновление статусов в `docs/PRODUCTION_STABILIZATION_PLAN.md` и `nextstep.md`** после каждого успешного шага.
