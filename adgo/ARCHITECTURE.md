# ADGO production architecture contract

This document defines the runtime invariants of Axiom ADGO. It is intentionally stricter than a feature overview: code changes that violate these rules are architectural regressions even if a happy-path test still passes.

## 1. Core principles

1. **Graph, not cursor.** Workflow position is derived from durable node states and dependencies.
2. **Committed state, not call stack.** A process may disappear at any instruction boundary.
3. **Deterministic control, probabilistic work.** LLMs/tools produce observations; deterministic code authorizes transitions.
4. **Persist before execute.** External work becomes durable before a worker may run it.
5. **At-least-once, never fake exactly-once.** Side effects use idempotency/reconciliation.
6. **Fencing over hope.** An expired worker cannot commit a late result.
7. **Hard invariants before utility.** Risk, permission, budget, concurrency and resources filter before scoring.
8. **Minimal repair.** Re-run only the dependency subgraph that can change the failed gate.
9. **Bound every loop.** Iteration, money, time and improvement bounds are mandatory for cycles/repair.
10. **Plan identity is immutable.** Executions pin ID/version/digest; migration is explicit.
11. **Human decisions are durable protocol messages.** They are not in-memory callbacks.
12. **Large data lives outside the control state.** Execution stores facts and artifact references.
13. **Recovery is normal execution.** Worker death, coordinator death, rate limits and restarts are routine paths.
14. **Infrastructure capabilities are interfaces.** Storage, archives, metrics transport and remote-worker transport are replaceable.
15. **Operator actions are auditable.** Pause, resume, patch, rewind, migration and retention leave durable evidence where appropriate.

---

## 2. Execution layers

### 2.1 `Runtime`: embedded deterministic kernel

`Runtime` owns the original super-step algorithm and remains useful for tests, local workflows and embedded orchestration. It can execute activities inline.

It is **not** the preferred distributed production boundary.

### 2.2 `Engine`: production coordinator/worker protocol

`Engine` reuses the deterministic internal-node logic but separates external work:

```text
Advance()
  |
  +-- ingest events
  +-- recover leases
  +-- budget / cancellation
  +-- run deterministic nodes
  +-- derive ready set
  +-- hard admission
  +-- schedule
  +-- COMMIT TaskPending
  |
  `-- return

worker Poll()
  |
  +-- find compatible TaskPending
  +-- CAS claim
  +-- COMMIT TaskRunning + WorkerID + LeaseUntil
  +-- execute handler
  +-- heartbeat
  `-- Complete/Fail with fencing validation
```

The coordinator never needs the worker goroutine stack to reconstruct execution state.

### 2.3 `Host`: many immutable plans

`Host` maps `PlanDigest -> Engine` over one execution store. An execution is routed to the exact engine matching its pinned digest.

Use Host when:

- old and new workflow versions coexist;
- child workflows use different plans;
- one worker fleet serves multiple workflow types.

### 2.4 `Production`: batteries-included assembly

`OpenProduction` wires:

- execution Store;
- Engine;
- durable AdaptiveRouter health;
- AdmissionController;
- ActivityCache;
- ScheduleStore + ScheduleRunner.

It does not hard-code application-specific activity implementations, secrets, network transport or object storage.

---

## 3. Plan compilation and identity

`Compile(Definition)` produces canonical immutable `Plan` and deterministic SHA-256 digest.

Static validation covers:

- duplicate/missing IDs;
- node kinds/outcomes;
- dependencies/transition targets;
- required data and producers;
- reachability;
- terminal existence;
- join/fan-out constraints;
- writer conflicts;
- permission policy;
- external-effect timeout/idempotency/retry;
- compensation requirements;
- strongly connected components;
- complete cycle/repair bounds.

An execution stores:

```text
PlanID
PlanVersion
PlanDigest
```

Normal execution refuses a different digest. `MigrateExecution` is the only intentional live repinning path.

---

## 4. Durable task state machine

```text
                         worker claim
NodePending ──enqueue──> TaskPending ──────────> TaskRunning
     ^                                             |
     |                                             | heartbeat
     |                                             v
     |                                       LeaseUntil += TTL
     |                                             |
     |              lease expiry                  |
     +<────────────────────────────────────────────+
     |
     | retry / repair
     |
     +-------------------------------+
                                     |
                              Complete / Fail
                                     |
                                     v
                              NodeCompleted
                                or NodeFailed
```

### 4.1 Enqueue invariant

A node may be marked `NodeRunning` only when the corresponding durable task is created in the same committed execution mutation.

### 4.2 Claim invariant

A worker claim atomically records:

- task status `running`;
- WorkerID;
- attempt;
- lease expiry.

### 4.3 Fencing invariant

`Complete`, `Fail` and `Heartbeat` require:

```text
same ExecutionID
AND same TaskID
AND same WorkerID
AND same Attempt
AND lease still valid
```

Otherwise `ErrStaleTask` is returned.

This is the zombie-worker boundary.

### 4.4 Lease recovery

Expired running tasks are recovered to pending work. Repeated recovery beyond the configured threshold quarantines the node for operator decision instead of retrying forever.

---

## 5. Atomic commit boundaries

An activity result commits, in one execution mutation:

- node completion state;
- facts;
- artifact refs;
- quality vector;
- budget usage;
- metrics;
- signature/oscillation state;
- compensation stack entry;
- downstream activation;
- durable history.

If that commit does not happen, redelivery is legal.

External systems therefore must not infer exactly-once from ADGO task status.

---

## 6. Ready-set semantics

An external node is eligible only when:

```text
activated
AND pending
AND NotBefore <= now
AND all dependencies satisfy the graph
AND required facts exist
AND strategy is not banned
AND risk approval satisfied
AND provider hard policy satisfied
AND throttle expired
AND resource/concurrency capacity exists
AND estimated budget can be reserved
```

Scheduler utility ranks only already-valid candidates.

Existing pending/running tasks reserve:

- global concurrency;
- activity concurrency;
- capability concurrency;
- resource keys;
- estimated cost.

Newly selected parallel candidates reserve cumulatively inside the same scheduling batch.

---

## 7. Budget model

`BudgetLimit` can bound:

- money/cost;
- tokens;
- wall duration;
- LLM calls;
- search queries;
- browser fetches.

Admission uses estimates; completion records actual `BudgetUsage`.

Speculative/ensemble execution records aggregate usage of all launched variants, never winner-only accounting.

---

## 8. Provider routing

Routing is two-phase.

### Phase A — hard constraints

Reject providers violating:

- availability;
- minimum quality;
- maximum cost;
- maximum latency;
- minimum privacy;
- maximum risk;
- required permissions.

### Phase B — adaptive utility

Rank the remaining set using:

- static quality/privacy/cost/latency/risk;
- EWMA observed quality;
- EWMA latency;
- EWMA cost;
- reliability posterior;
- exploration bonus;
- consecutive-failure penalty.

Circuit health may be durable and shared across coordinators through `ProviderHealthStore`.

A transient provider failure opens/penalizes that provider. The next retry re-runs capability resolution, allowing fallback.

---

## 9. Cross-execution admission

Workflow scheduler limits are not sufficient for a shared upstream API.

`AdmissionController` provides a separate cross-execution primitive:

```text
key: provider:openai
MaxConcurrent: 20
Rate: 100/minute
Burst: 20
```

A permit has a TTL and expires after process loss.

Denial becomes `FailureRateLimit`, so the normal durable retry path handles backpressure.

---

## 10. Failure classification

| Class | Meaning | Normal action |
|---|---|---|
| transient | retryable transport/runtime failure | bounded retry |
| rate_limit | upstream/admission backpressure | retry after durable delay |
| invalid_input | data cannot satisfy handler contract | repair / human |
| quality | output exists but fails requirements | targeted repair |
| permanent | retry cannot fix | fail + compensate |
| ambiguous_side_effect | external effect may have happened | durable reconciliation |

Retry is bounded by attempts **and** retry duration.

---

## 11. External effects

Compiler rejects unsafe external-effect definitions lacking required controls.

The idempotency boundary is:

```text
persist task + idempotency key
        BEFORE
invoke external system
```

Crash windows:

### Before provider accepted effect

Retry is safe.

### Provider accepted effect, ADGO commit succeeded

Execution progresses normally.

### Provider accepted effect, process died before ADGO commit

Redelivery may happen. The handler must use provider idempotency or reconcile. If outcome is unknown, return `FailureAmbiguousSideEffect`.

---

## 12. Human interrupt protocol

A waiting human node or approval is represented in committed state:

```text
NodeStatus = waiting
ExecutionStatus = awaiting_human
WaitingFor[node] = event/reconciliation/recovery reason
```

`ResolveHuman` commits patch/payload + decision + actor/reason before resuming.

Supported decision semantics are explicit; they do not permit arbitrary control-flow target strings from a UI or LLM.

---

## 13. Durable signal protocol

Inbox writes are durable and deduplicated by event ID.

`SignalDeterministic` strengthens external delivery:

- targeted events verify the target is waiting for that event type;
- untargeted event with one waiter resolves deterministically;
- multiple matching waiters cause an ambiguity error unless broadcast is explicitly allowed;
- optional payload is committed before inbox delivery.

`Awaitable` additionally binds callback identity to execution/node/event/revision/plan digest.

---

## 14. Targeted repair

Given failed gate `G` and repair roots `R`:

1. validate roots;
2. compute nodes reachable from roots that can affect `G`;
3. preserve completed nodes outside that set;
4. invalidate affected facts/artifacts;
5. increment revision counters;
6. reactivate affected nodes;
7. enforce loop bounds;
8. compare gate quality history for stagnation;
9. detect repeating strategy signatures.

Independent gates that repair the same work should use independent repair anchors.

---

## 15. Compensation

Successful reversible effects push compensation entries.

Normal failure/cancel executes stack LIFO.

`RunResilientCoordinator` treats `StatusCompensating` as recoverable durable state. If a coordinator dies mid-saga, another coordinator resumes the remaining stack.

`WithCompensationPolicy` adds bounded transient retry and per-attempt timeout.

Compensation itself is at-least-once and must be idempotent.

---

## 16. Time travel and forks

`VersionedStore` retains immutable committed snapshots.

Historical inspection never re-runs probabilistic work.

Forking:

- copies a committed snapshot;
- assigns a new execution ID/version timeline;
- clears active tasks and event dedup state;
- recovers in-flight nodes as pending;
- optionally patches data;
- records source execution/version.

This is a debugging/branching operation, not hidden mutation of history.

---

## 17. Plan migration

Migration requires a quiescent point.

Default validation rejects:

- source pin mismatch;
- active tasks;
- duplicate node mappings;
- missing mapped nodes;
- silent semantic changes to completed nodes.

Migration may add nodes and explicitly reset target-plan roots. Reset invalidates descendants/outputs and records an audit entry.

---

## 18. Continue-as-new

Infinite logical processes should not produce infinite single-execution history.

Continue-as-new:

1. requires quiescent source;
2. starts a fresh execution ID under the same plan;
3. carries selected facts/artifacts;
4. links both executions durably;
5. closes old execution.

---

## 19. Child workflows

Production child IDs are deterministic:

```text
<parent-execution>/<parent-node>/<item-id>
```

This makes parent redelivery idempotent.

A `Host` may run parent and child under different immutable plans.

Children are independent durable executions, not hidden goroutines inside parent state.

---

## 20. Storage capability model

Base `Store` owns:

- Create;
- Load;
- CAS Commit;
- durable inbox Put/List/Ack.

Optional capabilities:

```text
ExecutionCatalog       fleet listing / worker polling
VersionedStore         time travel / replay
ExecutionDeletionStore retention GC
VersionPruner          immutable-history compaction
ProviderHealthStore    shared routing health
AdmissionController    cross-execution permits
ActivityCache          shared pure-result reuse
ScheduleStore          durable triggers
```

This prevents every custom backend from implementing unrelated features before it can run basic workflows.

---

## 21. Built-in storage backends

### Memory

Ephemeral. Tests and local experiments.

### File

- atomic temp + fsync + rename;
- immutable commit files;
- execution lock files;
- stale-lock recovery;
- shared-filesystem multi-process use.

### Pebble

- high-throughput local KV;
- atomic batch latest + immutable version;
- inbox/catalog in same DB;
- sync writes by default;
- one database owner path enforced by Pebble locking.

For multi-host deployments without shared filesystem, implement `Store` over your transactional SQL/KV system and add optional capability interfaces as needed.

---

## 22. Schedules

Schedule firing identity is deterministic from schedule + fire timestamp.

Workflow start uses `StartOrLoad`, so the scheduler may safely retry after crashing between workflow creation and schedule cursor commit.

Catch-up is bounded by `MaxCatchUp`.

---

## 23. Pure-work acceleration

### Result cache

A content-addressed key covers namespace/activity/input facts/artifact refs.

Single-flight cache lease prevents duplicate expensive deterministic work across executions.

### Hedging

Preferred variant starts first; alternatives launch after delay. Early acceptable result cancels other launched work.

### Ensemble

Variants execute in bounded parallelism; deterministic maximum-quality result wins.

Both require explicit `Pure=true` because multiple executions would be unsafe for arbitrary side effects.

---

## 24. Observability and audit

History is a committed ordered log embedded in execution snapshots.

`Watch` resumes from history sequence number.

`Diagnostics` derives:

- ready set;
- waiting reasons;
- active tasks;
- lease health;
- budget/quality;
- provider health;
- invariant diagnostics.

`AuditExecution` is read-only and may run from a separate monitor process.

---

## 25. Retention

ADGO does not auto-delete state.

Explicit retention may delete only terminal executions selected by policy. Optional archive hook must succeed first.

Version pruning is a separate operation from deleting the execution.

This separation prevents an operational GC setting from silently destroying the only copy of a live workflow.

---

## 26. Security boundaries

The orchestration engine does **not** replace authentication, authorization or secret storage.

Production rules:

- raw credentials belong in worker secret management;
- execution data should contain references/non-secret facts;
- permissions/risk are hard workflow constraints;
- control APIs must authenticate operators externally;
- external effects require idempotency;
- user/operator patches cannot overwrite reserved `__adgo:` keys;
- adaptive proposals are validated before a new immutable plan is compiled;
- no LLM output may directly mutate live control state.

---

## 27. Recommended production topology

```text
                 ┌─────────────────────────────┐
                 │  API / webhook / operator   │
                 └──────────────┬──────────────┘
                                │
                     signals / human decisions
                                │
                 ┌──────────────v──────────────┐
                 │   resilient coordinator(s)  │
                 └──────────────┬──────────────┘
                                │ durable tasks
              ┌─────────────────┼──────────────────┐
              │                 │                  │
      ┌───────v───────┐ ┌──────v────────┐ ┌──────v────────┐
      │ LLM workers   │ │ search workers │ │ effect workers│
      └───────┬───────┘ └──────┬────────┘ └──────┬────────┘
              │                 │                  │
              └─────────────────┼──────────────────┘
                                │
                 ┌──────────────v──────────────┐
                 │ transactional durable Store │
                 ├─────────────────────────────┤
                 │ provider health / admission │
                 │ cache / schedules           │
                 └─────────────────────────────┘
                                │
                 ┌──────────────v──────────────┐
                 │ artifact / archive storage  │
                 └─────────────────────────────┘
```

The process layout may change. The durable protocol invariants do not.
