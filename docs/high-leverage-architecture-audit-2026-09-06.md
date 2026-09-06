# High-leverage architecture audit — 2026-09-06

Status: **engineering evidence input to `MASTER_PLAN.md`; not a parallel roadmap**  
Audited branch: `main`  
Audited baseline: `1b8ec9991007288e04a2f034633277d758cfbfa8`  
Primary scope: Core compiled runtime, typed `Flow`, `adgo`, Store/transaction boundaries, typed `model`, clock semantics, public composition surface and current architecture FMEA.

## 1. Executive conclusion

Axiom's highest-value next work is no longer feature accumulation. The system already contains a broad set of durable-runtime mechanisms. The dominant architectural risk is now **semantic and composition surface area**: several individually valid mechanisms can be combined under the wrong durability, transaction, clock, runtime or deployment assumptions.

The strongest leverage therefore comes from making incorrect compositions difficult or impossible, turning implicit runtime conventions into executable contracts, and reducing duplicated semantic boundaries without merging Core, Flow and ADGO into a mega-runtime.

The current FMEA already points in the same direction:

- distributed Store semantics are the largest system-level risk (`R-002`);
- Core/Flow/ADGO semantic drift is explicitly tracked (`R-003`);
- production miscomposition is explicitly tracked (`R-004`);
- clock-domain errors are explicitly tracked (`R-005`);
- persisted-format compatibility and public API breadth remain active risks (`R-006`, `R-007`).

This audit sharpens those risks into concrete architecture pressure points and orders them by expected benefit per unit of engineering effort.

## 2. Highest-leverage effort map

| Rank | Pressure point | Expected leverage | Existing planning connection |
|---:|---|---|---|
| 1 | First-party PostgreSQL Store + executable multi-process conformance suite | Extreme | `T-084`, `T-086`, `R-002`, `R-006` |
| 2 | Remove Engine-global transaction serialization | Extreme | production scalability / integration closure |
| 3 | Replace error-classification-driven transaction disposition with an explicit typed outcome | Extreme | strengthens error taxonomy and transaction correctness |
| 4 | Introduce canonical semantic IR below frontend parsing/rendering | Very high | `T-081`, `T-086`, semantic integration |
| 5 | Publish a small set of supported production composition profiles | Very high | `T-082`, `T-083`, `R-004` |
| 6 | Make Core/Flow/ADGO capability differences executable | Very high | `T-081`, `T-086`, `R-003`, `R-005` |
| 7 | Define explicit Core completion semantics | High | lifecycle correctness / integration closure |
| 8 | Enforce clock domains mechanically | High | `T-081`, `T-086`, `R-005` |
| 9 | Promote external-effect idempotency/reconciliation to a first-class adapter contract | High | `T-083`, `R-001` |
| 10 | Tier and narrow the stable public API | High | `T-085`, `T-086`, `R-007` |

The first three items are the best immediate architecture return: they attack correctness and scalability at central choke points rather than adding more surface area.

---

## 3. Pressure point A — Store SPI is the largest correctness boundary

### Observation

Core and ADGO depend on a tightly coupled persistence protocol: execution identity/versioning, transaction atomicity, task claim state, leases, fencing, retry checkpoints, history ordering, deduplication and migration/version semantics.

An interface can describe methods but cannot prove that a third-party backend preserves the coupled invariants under independent processes, weak isolation, stale workers, reconnects or crashes.

The repository already recognizes this as `R-002` and plans a first-party PostgreSQL implementation in `T-084`.

### Why this is the highest-leverage investment

A first-party PostgreSQL Store should not be treated as merely another backend. It should become the **executable reference implementation of the Store protocol**.

The important deliverable is therefore a backend-independent conformance suite that can be run against Memory, Pebble, PostgreSQL and future stores.

Recommended conformance families:

```text
StoreConformance
  Atomicity
  CAS/version monotonicity
  execution isolation
  canonical history ordering
  task claim identity
  lease expiry/recovery
  fencing / stale-writer rejection
  retry checkpoint atomicity
  inbox/event deduplication
  crash/reconnect behavior
  migration/reopen compatibility
  contention across independent connections/processes
```

For PostgreSQL, acceptance must use **independent database connections and preferably independent OS processes**, not only goroutines around one Store object.

### Architectural acceptance condition

A new networked backend is not production-qualified because its unit tests pass. It is production-qualified only if it passes the same executable conformance contract under multi-connection contention, stale-worker, rollback, crash/reconnect, isolation and migration scenarios.

---

## 4. Pressure point B — Engine-global transaction serialization

### Evidence

`internal/runtime/transaction.go` currently wraps transactional Store work with the Engine-level `storeMu`:

```go
e.storeMu.Lock()
defer e.storeMu.Unlock()

tx, err := transactional.BeginTransaction(ctx)
```

At the same time Axiom already has keyed execution locks, and the Pebble transaction implementation has its own execution-level locking.

### Architectural effect

The effective lock stack can become:

```text
Run.executionLocks
  -> Engine.storeMu
    -> Store transaction
      -> tx-local mutex
        -> backend execution lock
```

This means independent executions routed through one Engine can be serialized at a global Store boundary even when the backend is capable of finer-grained progress.

The production stabilization documentation already notes this scalability debt for Pebble.

### Recommended direction

Transaction-concurrency ownership should live at the Store/backend layer, not as a universal Engine assumption.

Target model:

```text
execution A -> lock(A) -> tx(A) --+
execution B -> lock(B) -> tx(B) --+--> backend
execution C -> lock(C) -> tx(C) --+
```

A Store that truly requires global serialization may implement it internally. The Engine should not impose it on every transactional backend.

### Guardrail

Before removal, characterize all current dependencies on `storeMu`, especially retry wrappers and external-activity ownership wrappers. The replacement must preserve same-execution serialization and atomic history/task/execution updates while allowing independent executions to make concurrent progress.

---

## 5. Pressure point C — transaction durability currently depends on error classification

### Evidence

`internal/runtime/transaction.go` has a useful distinction between ordinary failures that should rollback and durable control-flow failures whose state must commit before an error is returned.

The current mechanism decides this through `shouldCommitTransactionError(err)`, including special recognition of `DurableStateError`, retry scheduling and diagnostic code `AX505`.

Conceptually the path is:

```text
returned error
  -> error taxonomy / wrapping / diagnostic code
    -> commit or rollback
      -> durable runtime state
```

### Why this is fragile

Commit/rollback disposition is a persistence semantic, not merely an error presentation semantic. A future developer can introduce a new failure type, wrap an existing error differently, or change a diagnostic code and accidentally change whether already-staged failed/retry state survives the transaction.

Several runtime paths set `execution.Status = StatusFailed`, stage history/store changes, and then return an error. Whether the persisted failure survives is therefore coupled to the transaction wrapper's interpretation of the returned error.

### Recommended direction

Make transaction disposition explicit in the internal control channel.

Conceptual model:

```go
type TxDisposition uint8

const (
    TxCommit TxDisposition = iota
    TxRollback
)

type TxOutcome struct {
    Disposition TxDisposition
    Err         error
}
```

Equivalent internal APIs such as `CommitWithError(err)` / `RollbackWithError(err)` are also acceptable.

The critical invariant is:

> error taxonomy may explain the failure, but it must not be the sole authority that determines persistence disposition.

### Expected payoff

- smaller semantic blast radius of error refactors;
- easier testing of every durable failure mode;
- simpler review of transaction correctness;
- clearer distinction between domain/control-flow failures and infrastructure failures.

---

## 6. Pressure point D — typed `model` lowers through AXM text

### Evidence

The public Go `model` frontend is typed at the API surface, but its internal expression representation includes textual expressions (`Expr{text string}` and `Raw`). `Definition.Compile()` calls `Definition.Source()`, produces AXM text and sends that text through `axiom.CompilePlan`.

The current path is therefore approximately:

```text
Go model
  -> rendered AXM text
    -> parser
      -> language AST
        -> validation/compiler
          -> Plan
```

TOML and TRIZ also converge toward the canonical compiler through textual/normalization paths.

### Strength of the current design

The current approach correctly preserves one parser/compiler validation authority and avoids implementing a second semantic compiler in the Go builder.

### Fragility

The typed frontend still becomes a small source-code generator at its most important boundary:

- `Raw` can bypass typed helpers;
- identifier and literal rendering remains syntax-sensitive;
- changes in AXM syntax can affect the Go builder even when semantic IR did not need to change;
- some builder mistakes surface only after reparsing generated text.

### Recommended direction

Do **not** create a separate model compiler. Instead move the convergence point below textual syntax:

```text
AXM parser --------+
TOML frontend -----+
Go model ----------+--> canonical semantic AST/IR --> validator/compiler --> Plan
TRIZ normalization +
```

`Definition.Source()` can remain as a deterministic pretty-printer/debug artifact, but it should cease being the only bridge from typed Go declarations to canonical semantics.

### Expected payoff

This is one of the most valuable long-term simplifications because it reduces frontend/compiler drift while preserving a single semantic compiler.

---

## 7. Pressure point E — three runtimes are correct, but semantic equivalence must never be implied

Axiom intentionally has three different execution surfaces:

```text
Core compiled runtime
Typed Flow runtime
ADGO runtime/coordinator
```

The architecture anti-drift tests correctly prevent Core and ADGO from importing each other. This should remain.

The danger is not the existence of three runtimes. The danger is similar terminology around retry, effects, history, completion, clocks, leases and idempotency creating an assumption of equivalent guarantees.

### Recommended direction

`T-081` should produce an executable **Algorithm/Capability Integration Matrix**, not only prose.

For every capability record at least:

```text
capability
owner runtime
public entry point
persistence requirement
clock domain
failure semantics
retry/redelivery semantics
idempotency boundary
replay behavior
supported production profile
conformance test
explicit non-equivalence notes
```

Examples:

- Core retry and ADGO retry must remain separately owned because their arithmetic and lifecycle semantics differ.
- Flow durable effects use an outbox model and must not be described as Core activity retry.
- Core and ADGO lease/fencing behavior must remain distinct unless exact equivalence is later proved.

### Architectural rule

Share behavior-identical leaf primitives only after executable proof. Do not deduplicate concepts merely because they have the same English name.

---

## 8. Pressure point F — production composition surface is too large

The repository exposes many valid pieces: `Engine`, `Flow`, ADGO `Runtime`, `Engine`, `Host`, `OpenProduction`, scheduling, admission, routing, storage and policy primitives.

For experts this is powerful. For production consumers it creates a large reachable configuration space in which every component can be individually valid while the resulting guarantee set is wrong.

### Recommended direction

Complete `T-082/T-083` around three canonical supported profiles. Names are illustrative:

```text
Embedded
  process-local / development-oriented
  ephemeral components explicitly allowed

Durable Single Node
  synchronous durable Store
  restart recovery
  durable effect/task semantics
  no false multi-host guarantees

Distributed Production
  network transactional Store
  multi-host CAS
  leases/fencing
  durable dedup/inbox
  migration/upgrade qualification
```

Each profile should be a constructor/assembly with explicit capability checks, not merely a documentation recipe.

Advanced manual composition can remain available, but common users should be funneled through a small number of supported paths.

---

## 9. Pressure point G — Core lifecycle lacks an explicit completion rule

Core exports `StatusCompleted`, and replay knows about `ExecutionCompleted`, but the currently verified Core execution path does not automatically transition an execution to `Completed`. The architecture documentation already acknowledges this limitation.

This is more than naming debt because terminal status influences:

- retention and archival;
- parent/child integration;
- operational dashboards;
- SLA accounting;
- cleanup;
- migration eligibility;
- reasoning about whether `Waiting` means quiescent terminal work or waiting for future input.

### Important non-solution

Do not define `no pending task == Completed`. A durable workflow can legitimately be waiting for a future signal.

### Recommended direction

Make completion part of the declarative model, for example through a terminal predicate/state contract that the compiler/runtime can validate and test.

Conceptually:

```text
complete when Order.status == "closed"
```

or an equivalent typed terminal-state declaration.

The exact syntax is secondary. The architectural requirement is that completion be explicit and model-owned rather than inferred from absence of work.

---

## 10. Pressure point H — clock domains are understood but not yet fully enforced by code shape

Axiom has already done unusually strong work here: semantic clocks, timer abstractions, lease wall-clock separation, clock inventory and a dedicated FMEA risk exist.

The remaining weakness is that production code still contains multiple direct wall-clock calls across stores and ADGO subsystems. Some are correct wall-clock uses; others participate in semantic scheduling or diagnostics.

### Recommended direction

For durable/runtime packages, progressively prohibit unclassified direct `time.Now()` in architecture-sensitive code.

Prefer explicit domain APIs such as:

```text
semanticClock.Now()
leaseClock.Now()
auditClock.Now()
```

The names are illustrative. The important property is that a code reviewer and an architecture test can identify the intended time domain directly from the call site.

Extend `internal/durabletime` guard tests so new time-dependent production features must declare their clock domain and deterministic boundary tests before being considered complete.

---

## 11. Pressure point I — layered locking needs an ownership invariant

Current synchronization is layered across Run/Engine/Store/transaction/backend locks. This is manageable while a transaction effectively belongs to one execution, but it becomes risky if future work introduces atomic cross-execution operations.

### Recommended invariant

> A normal Core Store transaction belongs to exactly one execution ID.

If cross-execution atomicity is ever required, expose it through a separate API with canonical lock ordering instead of allowing arbitrary acquisition of multiple execution locks through the ordinary transaction path.

This protects against:

- lock-order inversion;
- hidden global serialization;
- multi-execution deadlocks;
- backend-specific assumptions leaking into Engine code.

---

## 12. Pressure point J — external effects need a first-class reconciliation boundary

Axiom correctly documents external activity execution as at-least-once. The irreducible crash window remains:

```text
persist intent/task
  -> remote provider accepts effect
    -> process dies before local completion commit
      -> task is redelivered
```

The runtime is correct to redeliver because it cannot prove whether the external side effect happened.

### Recommended direction

`T-083` should make the adapter contract stronger than a prose requirement for idempotency.

A production external-effect adapter should receive stable execution metadata such as:

```text
ExecutionID
Task/Effect ID
IdempotencyKey
Attempt
Fencing/claim identity where applicable
```

For providers with ambiguous acknowledgements, support an explicit reconciliation operation conceptually equivalent to:

```go
Execute(ctx, request)
Reconcile(ctx, idempotencyKey)
```

This cannot create universal exactly-once semantics, but it can make the correct at-least-once integration path much harder to misuse.

---

## 13. Public API breadth and pre-v1 leverage

Axiom is still pre-v1, which is the cheapest time to narrow the accidental stable surface.

`T-085` should distinguish at least:

```text
recommended application API
advanced composition API
backend/SPI API
internal implementation detail
```

The goal is not deletion for its own sake. The goal is to prevent low-level infrastructure contracts from becoming de facto application-level promises.

A supported profile should expose the smallest API needed for its deployment class; advanced consumers can opt into lower-level contracts deliberately.

---

## 14. Recommended execution order

This audit recommends the following order **inside the existing authoritative planning system**:

```text
1. Explicit transaction outcome protocol
2. Characterize and remove Engine-global transaction serialization
3. T-084 PostgreSQL reference Store
4. Multi-process Store conformance/fault-injection suite
5. T-081 executable capability/integration matrix
6. T-082/T-083 supported production profiles + reference application/effect adapters
7. Canonical semantic IR below frontend syntax
8. Explicit Core completion semantics
9. Clock-domain enforcement guard
10. T-085 public API tiering
11. T-086 integration-completeness gate closes the loop
```

The exact task numbering remains owned by `MASTER_PLAN.md`. This document intentionally does not create a second task registry.

## 15. What should not be done

The audit explicitly rejects the following tempting simplifications:

- merging Core, Flow and ADGO into one mega-runtime;
- unifying retry/backoff/lease algorithms because the concepts have similar names;
- claiming exactly-once external effects;
- replacing a Store conformance protocol with interface-level documentation alone;
- removing the typed Go frontend's single compiler authority by building a second independent compiler;
- treating `Waiting` as equivalent to `Completed` merely because no task is currently runnable;
- solving transaction scalability by weakening same-execution serialization or atomic history/task/state guarantees.

## 16. Acceptance criteria for the convergence phase

The architecture should be considered materially stronger when all of the following become true:

1. Every supported deployment profile has one canonical constructor/assembly and executable capability validation.
2. A first-party PostgreSQL backend passes a backend-independent, multi-process Store conformance suite.
3. Independent executions can transact concurrently without a universal Engine-global mutex while same-execution correctness remains proved.
4. Commit-vs-rollback disposition is explicit and exhaustively tested rather than inferred primarily from error classification.
5. Every important runtime capability is represented in an executable Core/Flow/ADGO integration matrix with explicit non-equivalence where required.
6. The Go model frontend can reach canonical semantic IR without requiring text reparse as its only compilation route.
7. Core completion is model-owned and unambiguous.
8. New architecture-sensitive time usage is assigned to a declared clock domain and mechanically guarded.
9. External-effect reference adapters demonstrate idempotency and reconciliation under ambiguous crash windows.
10. The recommended public API surface is materially smaller than the total low-level capability surface.

## 17. Final assessment

Axiom's strongest next move is to become **harder to compose incorrectly**, not merely broader.

The central design strategy should be:

```text
reduce reachable invalid compositions
+ make persistence/clock/failure semantics explicit
+ prove backend/runtime capabilities mechanically
+ preserve separate runtimes where semantics genuinely differ
= higher production trust with less architectural fragility
```

The three highest-return interventions are:

1. **PostgreSQL Store as the reference semantic backend plus multi-process conformance tests.**
2. **Explicit transaction outcomes plus removal of unnecessary Engine-global serialization.**
3. **A canonical semantic IR and executable capability matrix that reduce frontend/runtime semantic drift.**

These changes improve the reliability and comprehensibility of almost every existing Axiom feature without requiring a corresponding increase in feature count.