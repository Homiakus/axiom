# Transaction Isolation & Concurrency Model Design

**Status**: Approved Architecture Design (SCALE-003)  
**Package**: `internal/store/pebble`, `internal/store/memory`, `internal/runtime`, `adgo`  
**Date**: 2026-08-26

---

## 1. Overview & Problem Statement

In the baseline implementation of `internal/store/pebble.Store`, `BeginTransaction` acquired the global store mutex `s.mu` and held it until `Commit()` or `Rollback()`. While this guaranteed absolute serializability across all operations, it created a severe contention bottleneck when multiple independent execution workflows executed concurrently (SCALE-001 benchmarks showed latency degradation under concurrent workers).

Furthermore, the `runtime.Engine` maintains per-execution locks (`executionLocks *syncx.KeyedLocker`), ensuring that operations targeting the same execution ID are already strictly serialized at the runtime level.

This document defines the formal transaction isolation, conflict handling, and concurrency model that enables execution-scoped independence in the storage layer while preserving 100% store contract invariants.

---

## 2. Core Isolation & Concurrency Invariants

### 2.1 Keyspace Namespacing & Partitioning

All data stored in Pebble is strictly partitioned by `executionID`:

| Entity | Key Format | Isolation Scope |
|---|---|---|
| Execution Record | `exec/<executionID>` | Execution-scoped |
| Task Record | `task/<executionID>/<taskID>` | Execution-scoped |
| Task Status Index | `tstatus/<executionID>/<status>/<time>/<taskID>` | Execution-scoped |
| Task Dedup Index | `tdedup/<executionID>/<rule>/<activity>/<key>` | Execution-scoped |
| Task ID Index | `taskid/<taskID>` | Task-scoped (unique task ID) |
| History Sequence | `hseq/<executionID>` | Execution-scoped |
| History Entry | `hist/<executionID>/<seq:020d>` | Execution-scoped |
| Task Sequence | `tseq/<executionID>` | Execution-scoped |
| Store Format Marker | `meta/store-format` / `meta/schema-version` | Global (immutable after Open) |

Because entity mutations and queries are partitioned by `executionID`, transactions operating on different `executionID`s do not have conflicting data dependencies.

---

## 3. Transaction Mechanics & Lifecycle

### 3.1 Snapshot Read & Staging Semantics (Read-Your-Own-Writes)

1. **Transaction Initialization (`BeginTransaction`)**:
   - Allocates an isolated transaction context (`txStore`) containing a dedicated `pebbledb.Batch`.
   - Initializes local staging maps: `executions`, `tasks`, `history`, `historySeq`.
   - Does NOT hold a global store-wide lock for the lifetime of the transaction.

2. **Read Path within Transaction**:
   - `GetExecution(ctx, id)`: Checks `tx.executions[id]` first. If present, returns a deep defensive copy (`cloneExecution`). Otherwise, reads from underlying Pebble DB.
   - `ListTasks(ctx, executionID)`: Reads persisted tasks from Pebble DB, merges with staged modifications in `tx.tasks`, and sorts canonically.
   - `ListHistory(ctx, executionID)`: Reads persisted history from Pebble DB, appends staged uncommitted entries from `tx.history`, returning sorted chronological entries.

3. **External Read Isolation**:
   - Uncommitted writes in `tx.batch` and local staging maps are completely invisible to external readers and other transactions until commit.

---

## 4. Sequence Allocation Strategy

- **History Sequence**: Staged locally in `tx.historySeq[executionID]`. The first append in a transaction reads the current persisted sequence `hseq/<executionID>`, increments it, and records the new value in the batch and local map. Subsequent appends within the same transaction increment the local sequence.
- **Task Sequence**: Same pattern using `tseq/<executionID>`.
- Because sequences are strictly scoped to `executionID`, concurrent transactions for distinct executions generate sequences independently without lock contention.

---

## 5. Conflict Handling & Concurrency Boundaries

### 5.1 Same-Execution Synchronization
- **Runtime Level**: `engine.executionLocks.Lock(executionID)` ensures only one worker goroutine executes workflow steps for a given execution ID at any point in time.
- **Storage Level**: Optimistic CAS version validation (`execution.Version`). `SaveExecution` checks that the existing execution version matches expected state and increments monotonically on write. Concurrent modifications to the same execution produce a version conflict error.

### 5.2 Cross-Execution Independence
- Concurrent transactions for execution $A$ and execution $B$ allocate separate `pebbledb.Batch`es.
- Pebble batches write to independent key ranges without mutual exclusion on Go mutexes.
- Concurrent commits to Pebble are serialized only by Pebble's internal commit queue / WAL append, which is optimized for multi-goroutine throughput.

---

## 6. Commit Atomicity & Rollback Guarantees

### 6.1 Commit Atomicity
1. `tx.batch.Commit(writeOptions)` commits all staged writes (execution, tasks, history, indices, sequence counters) in a single atomic WAL + memtable operation.
2. If `Commit` fails (e.g. disk full, context canceled), the batch is closed, none of the changes are applied, and an error is returned.
3. Once committed, the transaction is marked closed; subsequent calls to `Commit` or `Rollback` are safe no-ops.

### 6.2 Rollback Guarantees
1. `Rollback()` closes `tx.batch` without calling `Commit()`.
2. Staged memory maps are discarded.
3. No partial state or orphan keys are written to Pebble.

---

## 7. Locking Refactoring Plan (SCALE-004 / SCALE-005)

### Core Pebble Store (`internal/store/pebble`)
- Replace the coarse global `s.mu` held across entire transaction lifetimes with:
  - Fine-grained sequence/read synchronization where needed.
  - Per-execution striped locking or execution-keyed locking for direct non-transactional mutation helpers if needed.
  - Direct atomic `pebbledb.Batch` committing.

### ADGO Store (`adgo/pebble_store.go`)
- Split full-store mutex into execution domain, inbox domain, and catalog domain.
- Preserve version CAS and deduplication guarantees under concurrency.

---

## 8. Verification Strategy

1. **Contract Conformance**: Run full `RunStoreContract` and `RunADGOStoreConformanceSuite` against modified stores.
2. **Race Detector**: `go test -race ./...` must be 100% green with count >= 20.
3. **Contention Benchmarks**: Measure throughput improvement across 1, 2, 4, 8, 16, 32 workers in `benchmark_contention_test.go`.
