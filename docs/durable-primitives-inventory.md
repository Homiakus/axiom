# Durable Primitive Contract Inventory

Status: **T-020 evidence / decision artifact**  
Baseline: `4c2a03f937640666687dbff9dc240b77a000a199`  
Scope: Core Axiom vs `adgo` durable primitives  
Non-goal: this document does **not** authorize extraction or engine/store unification.

## 1. Purpose and decision vocabulary

Core Axiom and ADGO both implement durable orchestration, so retry, leases, clocks, versions, persistence markers and errors can look duplicated. Similar names are not evidence of behavior-identical contracts.

The governing invariant is:

> Share only behavior-identical low-level primitives after executable characterization. Keep runtime state machines, store schemas and orchestration policy engine-owned unless equivalence is proved.

T-020 classifies candidates as:

- **SHARE CANDIDATE** — equivalent enough to design an acyclic common boundary in T-021.
- **PROVE FIRST** — overlapping concepts with materially different values/lifecycle/failure semantics.
- **KEEP SEPARATE** — apparent duplication is engine-specific policy or state.
- **DEFER** — belongs to another compatibility/API task.

No source behavior or persisted format changes in T-020.

---

## 2. Executive decision matrix

| Primitive class | Core contract | ADGO contract | Equivalence | Decision |
|---|---|---|---|---|
| Minimal semantic time source | `internal/runtime.Clock { Now() time.Time }` | `adgo.Clock { Now() time.Time }` | Structurally identical | **SHARE CANDIDATE** |
| Retry/backoff | fixed/exponential; default 100 ms; hard 30 s cap; no jitter; durable `NextAttemptAt` | exponential; policy max; deterministic jitter; retry-duration budget; failure classes | Same purpose, different math and controller | pure math **PROVE FIRST**; controller **KEEP SEPARATE** |
| Lease/fencing | `LockedBy`/`LockedUntil`, external-worker ownership validation, task attempt/lifecycle | `TaskRunning + WorkerID + Attempt + LeaseUntil`, stale-worker rejection | Same safety objective, different token/state model | predicates **PROVE FIRST**; state machines **KEEP SEPARATE** |
| Durability capability | explicit `StoreDurability` + `DurabilityProvider`; production requires synchronous durability | durability implicit in chosen store/assembly; no equivalent capability | Not duplicated | **KEEP SEPARATE** |
| Version/CAS | operation-oriented Store; transactions are separate capability | `Store.Commit(id, expectedVersion, mutate)` central to coordinator | Different transaction boundaries | **KEEP SEPARATE** |
| Persisted-format validation | Core Pebble schema + codec markers, incomplete/mismatch fail closed | ADGO store-format identity, unsupported identity fails closed | Same principle, different persisted contract | mechanics **PROVE FIRST**; schemas **KEEP SEPARATE** |
| Identity framing | Core execution/task/history/store/plan identities | ADGO plan digest, execution pinning, idempotency templates, scheduled IDs, path-safe IDs | Same need, different domains | pure framing **PROVE FIRST** |
| Lock ownership | process-local keyed execution locks + Core Pebble execution-scoped locking | ADGO FileStore owner-token files, heartbeat and stale-lock recovery | Different failure domains | **KEEP SEPARATE** |
| Error classification | typed sentinels + stable diagnostics; retryability recognizes `AX505` forms | explicit `FailureClass`/`DefaultClassify` plus coordinator errors | User-visible semantics differ | **DEFER** to T-051 |

### Net result

The evidence does **not** support a generic `internal/durable` mega-package containing stores, engines, retries and workers.

The justified common surface is intentionally small:

1. minimal semantic `Now()` time source;
2. possibly pure lease/fencing predicates after characterization;
3. possibly pure retry-delay math after characterization;
4. possibly schema-agnostic fail-closed marker helpers;
5. possibly canonical component framing after concrete duplicate code is identified.

Everything else remains engine-owned.

---

## 3. Clock abstraction

### Core

`internal/runtime/types.go` defines:

```go
type Clock interface {
    Now() time.Time
}
```

`Engine.SetClock` injects the semantic source. Retry deadline calculation uses this semantic time while waiting/timer creation is a separate Core concern.

### ADGO

`adgo/clock.go` defines the same minimal structural contract:

```go
type Clock interface {
    Now() time.Time
}
```

`WithClock` injects it into `Runtime`; durable decisions such as budget checks, retry deadlines, lease recovery and repair use semantic time where wired.

### Existing `internal/durabletime` nuance

`internal/durabletime/clock.go` already provides deterministic clock infrastructure, but its `Clock` is **strictly richer**:

```go
type Clock interface {
    Now() time.Time
    NewTimer(time.Duration) Timer
}
```

Therefore Core/ADGO `Clock` must **not** be directly aliased to `durabletime.Clock`: doing so would make every existing semantic-time implementation provide timer behavior it does not currently promise.

T-021 should prefer one of these acyclic options:

- introduce a minimal leaf `TimeSource`/`NowSource` in `internal/durabletime` and let the richer timer-capable `Clock` embed it; or
- keep the current engine interfaces and use compile-time/adaptor conformance without moving the type.

### Decision

**SHARE CANDIDATE — HIGH confidence for `Now()` only.** Timer/scheduler policy remains separate.

Required proof before extraction:

- compile-time conformance for both engines;
- deterministic `Now()` substitution unchanged;
- existing `durabletime.ManualClock` remains timer-capable;
- no dependency from the leaf package back into Core runtime or ADGO.

---

## 4. Retry and backoff

### Core contract

`internal/runtime/retry_store.go` owns durable retry around Core `Store`:

- retryability currently recognizes the `AX505` diagnostic contract;
- failed retryable tasks are durably returned to `TaskPending`;
- `NextAttemptAt` is persisted;
- retry scheduled/exhausted history is appended;
- `RetryScheduledError` is a typed persisted checkpoint with `ShouldCommitState() == true`;
- high-level `Run` may wait for the next deadline while low-level `RunUntilIdle` exposes it.

Core delay math:

- default base 100 ms;
- fixed or exponential;
- hard 30 s cap;
- no jitter in `retryDelay`;
- parameters originate in AXM/model policy expressions.

### ADGO contract

`adgo/runtime.go` / `adgo/engine.go` integrate retry into graph/task lifecycle:

- policy has max attempts, base delay, max delay and max retry duration;
- explicit failure class controls retryability;
- provider/rate-limit `RetryAfter` may impose a later durable retry time;
- `backoff` uses exponential growth plus deterministic jitter seeded by stable execution/node identity;
- retry interacts with task state, throttling, budgets and graph progress.

### Decision

**PROVE FIRST for pure arithmetic; KEEP SEPARATE for orchestration.**

A common retry engine or common public `RetryPolicy` would change semantics. A pure helper is allowed only if one parameterized function reproduces each engine’s existing outputs exactly for the subset it consumes.

Characterization required before T-022:

- attempt 0/1/N;
- zero/negative base;
- cap boundary and overflow-safe growth;
- Core fixed/exponential behavior;
- ADGO `MaxDelay` and deterministic jitter;
- `RetryAfter` remains controller policy, not generic exponential math;
- no changes to Core `NextAttemptAt`, history or ADGO failure classes.

---

## 5. Lease and fencing

### Core contract

Core external-worker ownership is represented in Core task/store state:

- `PollTaskWithLease(executionID, workerID, leaseTTL)` claims work;
- ownership uses `LockedBy` / `LockedUntil` plus task attempt/lifecycle;
- external-worker completion/heartbeat validates current ownership before mutation;
- inline execution is blocked for explicitly external-owned activities;
- retry-store lease memory is bookkeeping around the durable underlying claim, not a second durable fence.

### ADGO contract

ADGO fencing is a first-class task protocol:

- claim commits `TaskRunning + WorkerID + Attempt + LeaseUntil`;
- heartbeat only extends the current fenced lease;
- completion/failure must match the active task identity, worker and attempt;
- expired work can be recovered and reissued;
- stale workers are rejected after recovery/reissue;
- multiple engine processes can share the durable store.

### Decision

**PROVE FIRST for stateless predicates; KEEP SEPARATE for claim/heartbeat/recovery state machines.**

Potential leaf helpers may answer only questions such as “is lease expired at semantic time?” or “does this fence token match?”. Engine adapters own fields and transitions.

Required characterization:

- exactly at `LeaseUntil`;
- pre/post expiry;
- zero lease;
- worker mismatch;
- attempt mismatch;
- recovered/reissued task;
- late completion/heartbeat;
- deterministic clock behavior.

---

## 6. Durability capability

### Core

`internal/runtime/durability.go` deliberately separates durability from transaction support. The root facade exposes levels such as ephemeral, best-effort, buffered and synchronous; production-mode configuration requires sufficient synchronous durability for acknowledged commits.

### ADGO

ADGO chooses durability through concrete stores/production assembly. Its primary `Store` interface does not expose Core’s `StoreDurability` capability model.

### Decision

**KEEP SEPARATE.** This is not current duplication. Do not manufacture an ADGO durability enum merely to make APIs symmetric. Any future shared capability is a new behavioral/API task.

---

## 7. Version and CAS semantics

### Core

Core `internal/runtime.Store` is operation-oriented (`CreateExecution`, `SaveExecution`, history/task operations). `TransactionalStore` is a separate capability; Core Pebble implements execution-scoped locking/transaction behavior behind those contracts.

### ADGO

`adgo.Store` makes optimistic aggregate versioning explicit:

```go
Commit(context.Context, string, uint64, func(*Execution) error) (*Execution, error)
```

Expected-version conflict is part of normal coordinator retry behavior.

### Decision

**KEEP SEPARATE.** Do not create a shared Store/CAS interface. At most, a tiny dependency-free monotonic-version predicate can be reconsidered if it preserves errors, version increments and atomicity exactly.

---

## 8. Persisted-format validation

### Core

`internal/store/pebble/format.go` pins Core schema/codec metadata. Missing partial marker sets and mismatches fail closed. Codec identity matters because JSON is default and Gob is opt-in.

### ADGO

`adgo/pebble_format.go` pins ADGO-specific store-format identity and rejects unsupported values. ADGO records also carry their own plan/execution identity contract.

`internal/durableserial/inventory.go` already treats Core and ADGO persisted surfaces as separate machine-reviewable compatibility entries.

### Decision

**PROVE FIRST for schema-agnostic marker mechanics; schemas and marker identities KEEP SEPARATE.**

Allowed generic behavior:

- initialize absent marker set;
- reject partially present set;
- compare immutable expected values;
- return narrow mismatch errors without knowing engine schema.

Forbidden extraction:

- shared marker keys;
- shared schema versions/codecs;
- shared record structs;
- shared migration policy.

---

## 9. Identity framing

### Core

Core durable identity covers execution/task/history/store keys and canonical plan identity; filesystem and Pebble boundaries already have path/key confinement and serialized-surface inventories.

### ADGO

ADGO additionally owns immutable plan digest, execution PlanID/Version/Digest pinning, idempotency templates, deterministic scheduled execution IDs and path-safe encoded identities for file-backed state/locks.

### Decision

**PROVE FIRST for pure framing only.** Do not create one universal durable-ID type.

A common primitive must be limited to unambiguous framing of ordered components (for example length-prefixing or equivalent canonical encoding) and must be justified by concrete duplicated framing code. Existing public strings and persisted keys must remain unchanged.

Characterization must cover empty components, separators, Unicode, prefix ambiguity and repeatability.

---

## 10. Lock ownership

### Core

Core uses process-local keyed execution locks and Core Pebble execution-scoped locking/transactions to serialize same-execution mutation while allowing independent executions to progress concurrently.

### ADGO

ADGO FileStore additionally implements a cross-process crash-recoverable ownership protocol in `adgo/file_lock.go` / `adgo/file_lock_heartbeat.go`:

- owner-token lock file;
- heartbeat/freshness;
- stale-lock recovery;
- compatibility with older timestamp-only files;
- ownership-lost error;
- private-file and path-confinement invariants.

### Decision

**KEEP SEPARATE.** These mechanisms have different failure domains. Core `internal/syncx.KeyedLocker` is a generic process synchronization primitive, not equivalent to ADGO durable file ownership.

---

## 11. Error classification

### Core

Core combines typed errors/sentinels with stable diagnostic-code behavior. Durable retry currently recognizes `AX505` forms; `RetryScheduledError` carries persisted retry metadata; external worker paths own their stale/claim errors.

### ADGO

ADGO has explicit `FailureClass` / `DefaultClassify`, used by retries, compensation and provider health/routing, plus coordinator-specific conflict/stale/no-work/paused errors.

### Decision

**DEFER to T-051.** Classification is user-visible policy, not a pure durable primitive. T-020/T-022 must not normalize these into a shared enum or shared strings.

---

## 12. False-duplication findings

The audit materially narrows F-005:

1. **Store interfaces are intentionally different** — Core is operation/transaction-capability oriented; ADGO is versioned aggregate/CAS oriented.
2. **Durability capability is currently Core-specific**, not duplicated in ADGO.
3. **File lock ownership is ADGO cross-process recovery**, not Core keyed execution locking.
4. **Error classification is public policy**, not a low-level durable primitive.
5. **Persisted formats must remain separately versioned**, even if marker-validation mechanics can share code.
6. **The existing `internal/durabletime.Clock` is a timer-capable superset**, not a drop-in replacement for the two minimal engine `Clock` interfaces.

T-021 must therefore design a **small leaf dependency**, not a replacement runtime foundation.

---

## 13. Dependency constraint for T-021

Allowed direction:

```text
small dependency-free durable leaf
        ^                     ^
        |                     |
internal/runtime            adgo
        ^                     ^
        |                     |
Core stores/facades       ADGO stores/engine
```

Forbidden direction:

```text
shared leaf -> internal/runtime
shared leaf -> adgo
internal/runtime <-> adgo
shared Store interface wrapping both engines
shared retry/worker state machine wrapping both engines
```

The leaf should depend only on the Go standard library unless an existing lower-level internal package is demonstrably appropriate.

---

## 14. Extraction priority

### Tier A — justified now

1. **Minimal semantic time source (`Now()` only)** — highest-confidence equivalent contract. Prefer adapting/splitting the existing `internal/durabletime` hierarchy rather than adding another time package.

### Tier B — characterize first

2. Lease/fencing stateless predicates.
3. Retry/backoff pure arithmetic.
4. Schema-agnostic persisted-marker validation mechanics.
5. Canonical component framing, only after concrete duplication is identified.

### Tier C — not extraction targets

- Store interfaces or transaction engines;
- execution/task structs;
- retry controllers;
- worker claim/heartbeat/recovery;
- Core durability capability model;
- ADGO FileStore lock protocol;
- public error/failure taxonomy;
- record schemas / marker identities;
- scheduler/provider/budget/throttle policy.

---

## 15. Required proof before T-022

| Candidate | Required executable proof |
|---|---|
| Minimal time source | compile-time conformance; deterministic `Now()` substitution; timer-capable `durabletime.Clock` remains compatible |
| Lease predicate | exact expiry, pre/post expiry, zero time, wrong worker, wrong attempt, reissue |
| Backoff math | attempts, caps, zero/negative duration, overflow, deterministic jitter seed |
| Marker validation | absent, valid, partial, wrong schema/format/codec, reopen |
| Identity framing | empty components, separators, Unicode, prefix ambiguity, repeatability |

Mutation testing is appropriate for extracted branching arithmetic/predicate behavior. T-023 should then prevent an engine from reintroducing a second copy of a successfully extracted primitive.

---

## 16. T-020 completion criteria

T-020 is complete when:

- all nine classes have Core and ADGO evidence;
- false duplication is separated from behavior-identical duplication;
- the richer existing `durabletime.Clock` boundary is explicitly accounted for;
- no production/persisted behavior changes;
- T-021 has an acyclic dependency constraint;
- every proposed extraction has a proof requirement;
- Store/runtime/error-taxonomy mega-unification is explicitly rejected.

After qualification, the next task is **T-021 — define the acyclic shared durable boundary**. It should begin with the minimal time-source hierarchy, then model Tier B candidates as explicit contracts/tests before any T-022 extraction.
