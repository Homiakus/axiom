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

#### Milestone M3 — Scaling & Contention Reduction (100% DONE)
- **SCALE-001**: Benchmark Current Core Pebble Transaction Contention (`internal/store/pebble/benchmark_contention_test.go` covering 1-32 concurrent workers, read/write/mixed/same-exec profiles, and latency percentiles).
- **SCALE-002**: Measure Double-Serialization Between Engine and Store (`internal/runtime/engine_concurrency_test.go` verifying executionLocks isolation and concurrency benchmarks).
- **SCALE-003**: Design Conflict/Isolation Model Before Replacing Global Mutex (`docs/TRANSACTION_ISOLATION_DESIGN.md` documenting snapshot semantics, sequence allocation, and cross-execution independence).
- **SCALE-004**: Refactor Core Pebble Transaction Locking (`internal/store/pebble/transaction.go` removing global mutex and introducing execution-scoped locking with 3-5x throughput gain).
- **SCALE-005**: Reduce ADGO Pebble Global Mutex Contention (`adgo/pebble_store.go` splitting lock domains into execution-scoped locks and catalog RWMutex, improving commit throughput by >6.5x).
- **SCALE-006**: Comprehensive Scaling Benchmark Suite & Regression Thresholds (`benchmark_scaling_test.go` automated performance and concurrency threshold checks).

---

## 2. Immediate Next Tasks — Milestone M4 (TYPED)

### TYPED-001 — Freeze typed activity conversion semantics with tests
- **Objective**: Define exact supported contract for struct in/out, pointers, named maps, `axiom:` and `json:` tag precedence, embedded fields, and integer precision.
- **Target Files**: `internal/runtime/typed_test.go`, `axiom_typed_test.go`.
- **Verification**: `go test -race ./...`.

### TYPED-002 — Establish `Act` vs `ActTyped` benchmark baseline
- **Objective**: Measure ns/op, B/op, allocs/op for small/medium/nested structs and map inputs.
- **Target Files**: `internal/runtime/benchmark_typed_test.go`.

### TYPED-003 — Compile conversion plans at registration time
- **Objective**: Move reflection and tag lookup out of per-call path into registration-time cached conversion plans.
- **Target Files**: `internal/runtime/typed.go`.

---

## 3. Autonomous Execution Rules
1. **1 задача = 1 атомарный логический коммит.**
2. **Локальная верификация перед коммитом:** `golangci-lint run ./...`, `go test ./...`, `go test -race ./...`.
3. **Пуш в `origin/main`** и подтверждение чистой сборки.
4. **Обновление статусов в `docs/PRODUCTION_STABILIZATION_PLAN.md` и `nextstep.md`** после каждого успешного шага.
