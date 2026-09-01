# Durable Primitive Contract Inventory

Status: **T-020 evidence / decision artifact**  
Baseline: `4c2a03f937640666687dbff9dc240b77a000a199`  
Scope: Core Axiom vs `adgo` durable primitives  
Non-goal: this document does **not** authorize extraction or engine/store unification.

## 1. Why this inventory exists

Core Axiom and ADGO both implement durable orchestration, so several mechanisms look similar by name: retry, leases, clocks, versions, persistence markers and errors. Similar names are not sufficient evidence that contracts are behavior-identical.

The architectural invariant is therefore:

> Share only behavior-identical low-level primitives after executable characterization. Keep runtime state machines, store schemas and orchestration policy owned by their current engine unless equivalence is proved.

T-020 classifies every candidate into one of four outcomes:

- **SHARE CANDIDATE** — contract is already equivalent enough to design a common acyclic boundary in T-021.
- **PROVE FIRST** — concepts overlap, but values, lifecycle or failure semantics differ; characterization/property tests are required before extraction.
- **KEEP SEPARATE** — apparent duplication is actually engine-specific policy/state and must not be merged.
- **DEFER** — the question belongs to another planned compatibility/API task.

No source or persisted-format behavior is changed by T-020.

---

## 2. Executive decision matrix

| Primitive class | Core contract | ADGO contract | Equivalence | T-020 decision |
|---|---|---|---|---|
| Clock abstraction | `internal/runtime.Clock { Now() time.Time }` | `adgo.Clock { Now() time.Time }` | Structurally identical semantic-time source | **SHARE CANDIDATE** |
| Retry/backoff math | fixed/exponential, default 100 ms, hard 30 s cap, no jitter | exponential, policy max delay, deterministic jitter, retry-duration budget | Similar purpose; materially different math/policy | **PROVE FIRST** for pure math; orchestration **KEEP SEPARATE** |
| Lease/fencing predicate | task `LockedBy`/`LockedUntil`, external-worker claim validation and retry-store lease memory | `TaskRunning + WorkerID + Attempt + LeaseUntil`, stale-worker rejection | Same safety objective; different token/state model | **PROVE FIRST** for pure stale/current predicate; state machines **KEEP SEPARATE** |
| Durability capability | explicit `StoreDurability` levels + `DurabilityProvider`; production requires synchronous durability | durability is implicit in chosen ADGO store implementation; no equivalent public capability contract | Not duplicated today | **KEEP SEPARATE**; do not invent parity merely to share code |
| Version/CAS semantics | Core `Store` exposes create/save/history/task operations; transaction support is a separate capability | `Store.Commit(id, expected uint64, mutate)` is central and returns `ErrConflict` on version mismatch | Different store contracts and transaction boundaries | **KEEP SEPARATE**; only tiny pure version predicates may be reconsidered |
| Persisted-format validation | Core Pebble pins schema version + codec and rejects incomplete/mismatched markers | ADGO Pebble pins its own store-format identity and rejects unsupported identity | Same fail-closed principle; different schema identity | **PROVE FIRST** only for generic marker-validation helper; formats **KEEP SEPARATE** |
| Identity framing | execution/task/history/store keys and plan identity are owned by Core-specific schemas | plan digest, execution version, idempotency templates, schedule execution IDs and path-safe file identity are ADGO-specific | Shared need for unambiguous framing, but different domains | **PROVE FIRST** for pure canonical byte/string framing only |
| Lock ownership | process-local keyed execution locking plus Core Pebble execution-scoped locks | FileStore owner-token locks, heartbeat, stale-lock recovery plus process locks | Different failure domains | **KEEP SEPARATE** |
| Error classification | typed sentinels plus diagnostic-code contracts; retryability currently recognizes `AX505` forms | explicit `FailureClass` and `DefaultClassify`, with transient/rate-limit/permanent policy | Semantically different public behavior | **DEFER** to T-051; no shared taxonomy in T-020/T-022 |

### Net result

The inventory does **not** support a generic `internal/durable` mega-package containing stores, engines, retries and workers.

The currently justified shared surface is very small:

1. a semantic `Clock` contract / clock adapter boundary;
2. possibly pure identity-framing helpers;
3. possibly pure retry-delay math only after equivalence tests identify a common parameter subset;
4. possibly pure lease/fencing predicates only after token semantics are explicitly modeled;
5. possibly generic fail-closed marker helpers that contain no Core/ADGO schema knowledge.

Everything else stays owned by its engine.

---

## 3. Contract-by-contract evidence

## 3.1 Clock abstraction

### Core

`internal/runtime/types.go` defines:

```go
type Clock interface {
    Now() time.Time
}
```

`Engine.SetClock` injects the semantic clock. Retry scheduling uses the engine clock, while timer waiting is handled separately by the engine timer abstraction. `internal/durabletime` already contains deterministic clock infrastructure and the machine-reviewed time inventory.

### ADGO

`adgo/clock.go` defines the same structural contract:

```go
type Clock interface {
    Now() time.Time
}
```

`WithClock` injects it into `Runtime`; runtime durability decisions such as budget checks, retry deadlines, lease recovery and repair paths use the injected semantic time where wired.

### Decision

**SHARE CANDIDATE — HIGH confidence.**

T-021 should define an acyclic location for the minimal semantic clock interface/adapters. It must not merge timer/scheduler policy: Core has additional timer waiting behavior and ADGO has its own scheduling lifecycle.

Required proof before extraction:

- compile-time interface conformance for both engines;
- deterministic-clock behavior remains unchanged;
- no new dependency from low-level durable helpers back into either runtime package.

---

## 3.2 Retry and backoff

### Core

`internal/runtime/retry_store.go` owns durable retry orchestration around the Core `Store`:

- a retryable activity failure is currently recognized through the `AX505` diagnostic contract;
- retry state is materialized by returning the task to `TaskPending`;
- `NextAttemptAt` is persisted;
- `ActivityRetryScheduled` / `ActivityRetryExhausted` history is appended;
- `RetryScheduledError` is a typed persisted-retry checkpoint and `ShouldCommitState() == true`;
- `Run` may wait until the persisted retry deadline while low-level `RunUntilIdle` exposes the boundary.

Core delay math:

- default base: 100 ms;
- fixed or exponential policy;
- hard cap: 30 s;
- no jitter in `retryDelay`;
- delay derives from AXM/model policy expressions.

### ADGO

`adgo/runtime.go` / `adgo/engine.go` own retry as part of the graph/task lifecycle:

- retry policy includes max attempts, base delay, max delay and retry-duration constraints;
- failure class participates in retryability;
- `RetryAfter` can override the calculated delay for rate-limit/provider semantics;
- `backoff` uses exponential growth and deterministic jitter seeded by execution/node identity;
- retry is integrated with ADGO task state, throttling, budgets and graph progress.

### Decision

**PROVE FIRST for pure math. KEEP SEPARATE for orchestration.**

The two retry controllers are not behavior-identical. A common `RetryPolicy` or common retry engine would silently change public semantics.

T-021/T-022 may only consider a pure helper if a common parameterized function can reproduce current outputs exactly for the subset each engine uses. Characterization must cover:

- attempt 0/1/N;
- zero/negative base delay;
- cap boundaries and overflow-safe growth;
- fixed vs exponential Core behavior;
- ADGO deterministic jitter and `MaxDelay`;
- `RetryAfter` being orchestration policy, not part of generic exponential math;
- no change to `NextAttemptAt`, history or failure-class behavior.

---

## 3.3 Lease and fencing

### Core

Core external-worker execution is implemented through task ownership in the Core store:

- `PollTaskWithLease(executionID, workerID, leaseTTL)` claims work;
- task ownership uses `LockedBy` and `LockedUntil` plus the task attempt/lifecycle;
- `internal/runtime/external_worker.go` rejects stale/wrong ownership before completion/heartbeat mutations;
- inline activity execution is blocked for activities declared as externally worker-owned;
- retry-store wrapping remembers the leased task locally only to drive retry bookkeeping; durable ownership remains in the underlying task/store state.

### ADGO

ADGO makes fencing a first-class task protocol:

- claim commits `TaskRunning + WorkerID + Attempt + LeaseUntil`;
- heartbeat extends only the currently fenced lease;
- completion/failure must match current task identity, worker and attempt;
- expired workers are recovered and old workers are rejected with stale/fenced errors;
- multiple engine processes may share the same durable store.

### Decision

**PROVE FIRST for pure predicates. KEEP SEPARATE for claim/recovery state machines.**

The invariant is common — a stale worker must never commit — but the tokens and transitions are not identical.

A future shared helper is allowed only if it is a stateless predicate/value helper such as “is lease expired at semantic time?” or “does this fence token match?”, with engine-owned adapters supplying their own fields. It must not own claim, heartbeat, retry or recovery transitions.

Required characterization:

- boundary at exactly `LeaseUntil`;
- zero lease;
- worker mismatch;
- attempt mismatch;
- recovered/reissued task;
- late completion and late heartbeat;
- clock injection behavior.

---

## 3.4 Durability capability

### Core

`internal/runtime/durability.go` defines an explicit durability capability separate from transaction support. The root facade exposes levels including:

- ephemeral;
- best effort;
- buffered;
- synchronous.

Production-mode validation requires an acknowledged store to declare sufficient synchronous durability. This is a public behavioral contract.

### ADGO

ADGO currently selects durability through concrete stores and production assembly. Its primary `Store` interface does not expose the Core `StoreDurability` capability model.

### Decision

**KEEP SEPARATE.**

This is not current duplication. T-022 must not create an ADGO durability enum merely to make the code look symmetric. If a future production requirement needs a shared durability capability, that is a new behavioral/API task and must be separately characterized.

---

## 3.5 Version and CAS semantics

### Core

Core `internal/runtime.Store` is operation-oriented (`CreateExecution`, `SaveExecution`, history/task operations). Transaction support is modeled separately through `TransactionalStore`. Core Pebble then adds execution-scoped transaction/locking behavior behind that contract.

### ADGO

`adgo.Store` makes optimistic versioning explicit:

```go
Commit(context.Context, string, uint64, func(*Execution) error) (*Execution, error)
```

The caller supplies the expected execution version; commit conflict is part of the normal coordinator/retry path.

### Decision

**KEEP SEPARATE.**

Do not introduce a shared store/CAS interface. The transaction boundaries and persisted state ownership are different.

At most, T-021 may define a dependency-free internal helper for monotonic-version validation if both implementations can consume it without changing errors, version increments or transaction atomicity. This is lower priority than Clock and fencing/backoff characterization.

---

## 3.6 Persisted-format version handling

### Core

`internal/store/pebble/format.go` stores and verifies explicit Core format metadata. Core Pebble currently pins schema version and codec; incomplete markers and mismatches fail closed. JSON is the default codec and Gob is an opt-in alternative, so codec identity is part of the compatibility check.

### ADGO

`adgo/pebble_format.go` stores ADGO-specific format identity and rejects unsupported identities. ADGO execution records also carry plan identity/version information as part of their own persisted contract.

`internal/durableserial/inventory.go` is already the canonical machine-reviewable registry of serialized durable surfaces. It correctly keeps Core and ADGO records as separate persisted surfaces.

### Decision

**PROVE FIRST for generic marker mechanics; persisted schemas KEEP SEPARATE.**

A shared helper may only cover schema-agnostic mechanics such as:

- initialize marker if absent;
- reject partially present marker sets;
- compare expected immutable marker values;
- return a typed mismatch without knowing Core/ADGO schema fields.

Do not merge marker keys, schema versions, codecs, record structs or migration policy.

---

## 3.7 Identity framing

### Core

Core durable identity spans execution IDs, task IDs, history sequence keys, store prefixes and canonical plan identity. Filesystem and Pebble boundaries already include path/key confinement work and serialized-surface inventory.

### ADGO

ADGO additionally owns:

- immutable plan digest (`sha256:` canonical digest);
- execution `PlanID` / `PlanVersion` / `PlanDigest` pinning;
- idempotency-key templates containing execution/node/attempt/revision/plan identity;
- deterministic scheduled execution IDs;
- path-safe encoded IDs for file-backed stores and locks.

### Decision

**PROVE FIRST for pure framing only.**

Do not create one universal “durable ID” type. Domain identities intentionally differ.

A potential shared primitive must be limited to canonical collision-resistant framing of ordered components (for example length-prefixing or an equivalent unambiguous byte encoding) and must be justified by concrete duplicate implementations. Existing public string forms and persisted keys must not change as a side effect.

---

## 3.8 Lock ownership

### Core

Core uses process-local keyed execution locks and Core Pebble execution-scoped locking/transactions to serialize mutation while allowing independent executions to progress concurrently.

### ADGO

ADGO additionally has cross-process filesystem ownership semantics in `adgo/file_lock.go` and `adgo/file_lock_heartbeat.go`:

- owner-token lock file;
- heartbeat/freshness;
- stale lock recovery;
- backward compatibility for older timestamp-only lock files;
- explicit ownership-lost error;
- private file permissions and path-confinement invariants.

### Decision

**KEEP SEPARATE.**

These mechanisms operate in different failure domains. Extracting ADGO’s FileStore lock protocol into a generic durable package would broaden its contract without a Core consumer. Core’s process-local `internal/syncx.KeyedLocker` is already a small generic synchronization primitive and is not equivalent to ADGO crash-recoverable file ownership.

---

## 3.9 Error classification

### Core

Core combines typed sentinel/errors with stable diagnostic-code behavior. Durable retry currently recognizes retryability from `AX505` forms, and `RetryScheduledError` carries persisted retry metadata. External-worker paths have their own stale/ownership errors.

### ADGO

ADGO has explicit `FailureClass` values and `DefaultClassify`, used by retries, compensation recovery and provider health/routing. It also has coordinator-specific errors such as conflict/stale-task/no-work/paused states.

### Decision

**DEFER to T-051.**

The taxonomies are not equivalent and classification is user-visible policy. T-020/T-022 must not normalize these into a shared enum or shared error strings.

Low-level future helpers may return their own narrow internal errors, but mapping those errors into Core diagnostics or ADGO `FailureClass` remains engine-owned.

---

## 4. False-duplication findings

The audit materially narrows F-005. Several items previously described as “duplicated durable primitives” are only conceptually adjacent:

1. **Store interfaces are intentionally different.** Core is operation/transaction-capability oriented; ADGO is versioned aggregate/CAS oriented.
2. **Durability capability is currently a Core contract, not a duplicate ADGO contract.**
3. **File lock ownership is ADGO-specific cross-process recovery, not the same thing as Core keyed execution locks.**
4. **Error classification is public policy, not a pure durable primitive.**
5. **Persisted formats must remain separately versioned even if marker-validation mechanics can share code.**

This means T-021 should design a **small leaf dependency**, not a replacement runtime foundation.

---

## 5. Candidate dependency direction for T-021

T-020 does not create the package, but it constrains the acceptable graph.

Allowed direction:

```text
small dependency-free internal durable leaf
        ^                     ^
        |                     |
internal/runtime            adgo
        ^                     ^
        |                     |
Core stores/facades       ADGO stores/engine
```

Forbidden direction:

```text
internal/durable -> internal/runtime
internal/durable -> adgo
internal/runtime <-> adgo
shared store interface wrapping both engines
shared retry/worker state machine wrapping both engines
```

The shared leaf should depend only on the Go standard library unless an existing lower-level internal package is demonstrably appropriate.

---

## 6. Extraction priority for T-021/T-022

### Tier A — justified now

1. **Semantic Clock contract/adapters** — highest-confidence equivalent contract.

### Tier B — characterization required first

2. **Lease/fencing pure predicates** — only stateless time/token checks.
3. **Retry/backoff pure math** — only if a parameterized helper preserves both current algorithms exactly; otherwise leave separate.
4. **Generic persisted-marker validation mechanics** — only schema-agnostic fail-closed behavior.
5. **Canonical component framing** — only after concrete duplicate framing code is identified and persisted/public representations remain unchanged.

### Tier C — explicitly not extraction targets

- Store interfaces or transaction engines;
- execution/task structs;
- retry controllers;
- worker polling/claim/heartbeat/recovery;
- Core durability capability model;
- ADGO FileStore lock ownership protocol;
- public error/failure taxonomy;
- record schemas and persisted marker identities;
- scheduler/provider/budget/throttle policy.

---

## 7. Required executable proof before T-022

T-021 must turn each selected Tier B candidate into an explicit behavior table. T-022 may extract only after tests prove both old implementations agree with the proposed leaf primitive.

Minimum proof matrix:

| Candidate | Required proof |
|---|---|
| Clock | compile-time conformance + deterministic `Now()` substitution in both engines |
| Lease predicate | exact-expiry, pre-expiry, post-expiry, zero time, wrong worker, wrong attempt |
| Backoff math | attempts, caps, zero/negative duration, overflow, deterministic jitter seed behavior |
| Marker validation | absent, complete valid, partial marker, wrong schema/format/codec, reopen |
| Identity framing | empty components, separators, Unicode, prefix ambiguity, deterministic repeatability |

Mutation testing is appropriate when the new shared function contains branching arithmetic/predicate behavior. Architecture tests in T-023 should then prevent either engine from reintroducing a second copy of an extracted primitive.

---

## 8. T-020 completion criteria

T-020 is complete when:

- all nine audited classes have Core and ADGO evidence;
- false duplication is separated from behavior-identical duplication;
- no production/persisted-format behavior is changed;
- T-021 has an explicit acyclic dependency constraint;
- every proposed extraction has a proof requirement;
- store/runtime/error-taxonomy mega-unification is explicitly rejected.

The next task after qualification is **T-021 — define the acyclic shared durable boundary**. It should start with Clock and model Tier B candidates as contracts/tests before moving code.
