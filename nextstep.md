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

#### Milestone M4 — High-Performance Typed Runtime (100% DONE)
- **TYPED-001**: Freeze Typed Activity Conversion Semantics (`typed_contract_test.go` verifying tag precedence, omitted fields, pointers, maps, nested/embedded structs, >2^53 int64 precision).
- **TYPED-002**: Establish `Act` vs `ActTyped` Benchmark Baseline (`benchmark_typed_test.go` measuring ns/op, B/op, allocs/op across workloads).
- **TYPED-003**: Compile Conversion Plans at Registration Time (`internal/typedconv` ahead-of-time plan compilation and `sync.Map` type caching).
- **TYPED-004**: Remove Unnecessary JSON Round-Trip from Input Path (direct compiled field assignment in `typedconv.CompileInput[In]()`).
- **TYPED-005**: Remove Per-Call Output Reflection (pre-compiled field getters in `typedconv.CompileOutput[Out]()`).

#### Milestone M5 — Typed Error Taxonomy & Classification (100% DONE)
- **ERR-001**: Eliminate `AX505` Substring Retry Classification (`internal/runtime/retry_store.go` strict prefix/delimiter matching).
- **ERR-002**: Unify Transaction Commit-On-Error Classification (`internal/runtime/transaction.go` `DurableStateError` contract preserving domain flow-control state mutations).

#### Stress & Chaos Testing Framework (100% DONE)
- **Crash & Recovery Chaos**: Abrupt stoppage/panic between BeginTransaction and Commit leaves 0 trace upon reopen (`internal/store/pebble/chaos_test.go`); Flow outbox redelivery with exact same EffectID (`stress_chaos_test.go`).
- **High Concurrency & Contention Stress**: 100+ concurrent workers across 50 shared & independent executions with 0 races, 0 deadlocks, strict sequence order (`stress_chaos_test.go`).
- **Split-Brain & Stale Lock Takeover**: Worker lease loss and fencing tests (`stress_chaos_test.go`).
- **ADGO FileStore Multi-Routine Contention**: Competing inbox and ack routines under high load (`stress_chaos_test.go`).
- **Poison Pills & Malformed Data**: Injected corrupted records and future schema markers fail closed with clear diagnostics without database wipe (`internal/store/pebble/chaos_test.go`, `stress_chaos_test.go`).
- **Context Cancellation Boundaries**: Immediate fail-fast on pre-canceled context and non-interruptible atomic commits (`stress_chaos_test.go`).

---

## 2. All Tasks Completed & Verified
All requirements for Milestones M4 (TYPED), M5 (ERR), and the Comprehensive Stress & Chaos Testing Framework are 100% implemented, tested under `-race`, committed, and pushed to `main`.

---

## 3. Autonomous Execution Rules
1. **1 задача = 1 атомарный логический коммит.**
2. **Локальная верификация перед коммитом:** `golangci-lint run ./...`, `go test ./...`, `go test -race ./...`.
3. **Пуш в `origin/main`** и подтверждение чистой сборки.
4. **Обновление статусов в `docs/PRODUCTION_STABILIZATION_PLAN.md` и `nextstep.md`** после каждого успешного шага.
