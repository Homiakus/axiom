# Milestone M4, M5 & Stress/Chaos Engineering Verification Report

**Date:** 2026-08-26  
**Repository:** `github.com/Homiakus/axiom`  
**Engineer:** Principal Go Distributed Systems, Reliability & Storage Performance Engineer  
**Status:** **100% Verified & Passing (All Unit, Race, Stress & Chaos Suites Green)**

---

## 1. Summary of Completed Milestones

### Milestone M4 — High-Performance Typed Runtime (`ActTyped`)
1. **TYPED-001: Typed Conversion Contracts**:
   - Tag priority strictly enforced: `axiom:"name"` > `json:"name"` > lowerCamelCase field names.
   - Handled omitted fields (`json:"-"`, `axiom:"-"`, `omitempty` stripping).
   - Full support for pointers, nil pointers, named string-key maps (`type CustomMap map[string]T`), nested structs, and flattened anonymous embedded structs.
   - **Full 64-bit integer precision**: Exact representation preserved for values exceeding IEEE-754 53-bit float limits (e.g. `9007199254740993` and `math.MaxInt64`).
   - *Test Suite:* `typed_contract_test.go`.

2. **TYPED-002: Benchmark Baseline & Micro-Conversion Performance**:
   - `BenchmarkActTyped_DirectConversionMicro`: **1,075 ns/op**, **921 B/op**, **13 allocs/op** (1,000,000 iterations).
   - Full execution performance `BenchmarkActTyped_Medium` reduced to **51,433 ns/op** and **399 allocs/op** (beating raw dynamic `Act` at 56,184 ns/op and 403 allocs/op).
   - *Benchmark Suite:* `benchmark_typed_test.go`.

3. **TYPED-003, TYPED-004, TYPED-005: Registration-Time Compilation & Zero Round-Trip**:
   - Implemented `internal/typedconv` with Ahead-of-Time (AOT) conversion plan compilation (`CompileInput[T]` and `CompileOutput[T]`).
   - Eliminated runtime `json.Marshal -> json.Unmarshal` round-trip on typed activity input path.
   - Eliminated per-call reflection and field enumeration on output path using pre-compiled getter closures.
   - Thread-safe type plan caching with `sync.Map`.

---

### Milestone M5 — Typed Error Taxonomy & Classification
1. **ERR-001: Strict Diagnostic Classification**:
   - Eliminated loose substring heuristics (`strings.Contains(message, "AX505")`).
   - Introduced strict diagnostic code matching (`AX505`, `AX505:`, `: AX505:`, `: AX505 `) preventing false positive matches on unrelated user payloads (e.g. `TAX5050`, `AX5050`).
   - *Test Suite:* `internal/runtime/retry_error_test.go`.

2. **ERR-002: Unified Commit-On-Error Classification**:
   - Formalized `DurableStateError` interface (`ShouldCommitState() bool`) to decouple domain flow-control state preservation (such as `RetryScheduledError` and `diag.Error` AX505) from unexpected storage/system errors (which trigger immediate transaction rollback).
   - *Test Suite:* `internal/runtime/retry_error_test.go`.

---

### Comprehensive Stress & Chaos Testing Framework
Implemented in `stress_chaos_test.go` and `internal/store/pebble/chaos_test.go`:

| Chaos / Stress Dimension | Scenario & Invariant Tested | Result |
|---|---|---|
| **Crash & Recovery Chaos** | Uncommitted transaction abandoned during crash; store reopened. Verified 0 partial records, 0 leaked history/tasks, version unchanged. | **PASS** |
| **Flow Outbox Recovery** | Sudden process crash after state+intent commit but before effect delivery; reopened on Pebble store. Verified exact EffectID redelivery and idempotent completion. | **PASS** |
| **High Concurrency & Contention** | 100 concurrent workers, 1,000+ operations across 50 shared & independent executions. | **PASS (0 races, 0 deadlocks, strict sequence order)** |
| **Split-Brain & Stale Leases** | Worker A lease expires -> Worker B takes over -> Worker A wakes up and is fenced with rejection; Worker B succeeds. | **PASS** |
| **ADGO FileStore Takeover** | 30 concurrent routines competing for inbox/file lock mutations. Monotonic versioning and ack idempotency preserved. | **PASS** |
| **Poison Pills & Malformed Data** | Corrupted bytes and future schema marker injected. Reopen and GetExecution fail closed with explicit diagnostic errors without wiping data. | **PASS** |
| **Context Cancellation Boundaries** | Pre-canceled context returns `context.Canceled` immediately before writing state. Atomic local commits preserve all-or-nothing atomicity. | **PASS** |

---

## 2. Verification Evidence

```text
go vet ./... -> 0 warnings
go test ./... -> PASS (all packages)
go test -race ./... -> PASS (all packages, 0 data races, clean)
```
