# ADGO — Adaptive Durable Graph Orchestration

ADGO is Axiom's production durable orchestration engine for long-running graphs, agents, LLM/tool workflows, human approvals and recoverable business processes.

> **Deterministic control plane. Adaptive execution plane. Durable state is the source of truth.**

Activities may be probabilistic. Control flow is not. Workers return facts, artifacts, quality and resource usage; immutable plans, deterministic gates, bounded repair and explicit operator decisions decide what happens next.

ADGO is intentionally **at-least-once** for external effects. It does not claim magical exactly-once network behavior. The engine instead gives every activity a stable idempotency key, durable task state, leases, heartbeats, fencing and reconciliation primitives.

---

## Which API should I use?

| Layer | Use when | Executes activities | Durable workers | Multiple plan versions |
|---|---|---:|---:|---:|
| `Runtime` | embedded tests / compact single-process orchestration | inline | no | no |
| `Engine` | production workflow service for one immutable plan | workers | yes | one plan digest |
| `Host` | one service hosts many plan versions / child workflows | workers | yes | yes |
| `OpenProduction` | batteries-included deployment | workers | yes | one plan per returned `Production` |

For new production services, start with `OpenProduction` or `Host`.

`Runtime` remains supported because a deterministic embedded kernel is useful, but production code should normally separate coordinator state transitions from worker execution through `Engine`.

---

## What is implemented

### Plan and compiler

- immutable SHA-256 digest-pinned `Plan`;
- typed nodes: activity, decision, gate, fork, join, wait, human, subflow, compensation;
- graph reachability and dependency analysis;
- Tarjan SCC cycle detection;
- bounded-cycle enforcement;
- missing data/reference validation;
- conflicting parallel writer detection;
- permission checks;
- external-effect timeout/idempotency/retry validation;
- high-risk compensation requirements;
- deterministic plan identity.

### Durable production execution

- coordinator/worker split;
- durable `TaskPending -> TaskRunning -> commit` protocol;
- worker claim leases;
- automatic and explicit heartbeats;
- stale-worker fencing;
- expired-lease recovery;
- recovery quarantine after repeated worker loss;
- optimistic CAS execution commits;
- durable inbox and event deduplication;
- durable timers and waits;
- execution-level idempotency through `StartOrLoad`;
- graceful worker drain for rolling deployments;
- resilient compensation recovery after coordinator crash.

### Scheduling and resource control

- utility scheduling;
- global concurrency;
- per-activity concurrency;
- per-capability concurrency;
- resource-key mutual exclusion;
- active-task reservations;
- cumulative batch cost admission;
- duration/deadline pressure;
- cross-process concurrency admission;
- token-bucket rate limiting;
- crash-expiring admission permits.

### Adaptive execution

- capability-based provider selection;
- hard privacy/risk/permission/cost/latency constraints;
- provider fallback;
- EWMA latency/quality/cost feedback;
- reliability scoring;
- exploration bonus;
- provider circuit breaking;
- durable shared provider-health store;
- rate-limit `Retry-After` handling;
- opt-in hedged execution for pure activities;
- opt-in ensemble execution with deterministic best-result selection;
- conservative aggregate budget accounting for speculative work.

### Reliability and recovery

- bounded exponential retry + deterministic jitter;
- failure classes: transient, rate-limit, invalid-input, quality, permanent, ambiguous-side-effect;
- targeted dependency repair;
- independent repair anchors and revision epochs;
- iteration/cost/duration/epsilon bounds;
- convergence and oscillation detection;
- reverse compensation stack;
- bounded compensation retry/timeout wrapper;
- ambiguous side-effect reconciliation;
- pause/resume/cancel;
- operator rewind of affected subgraphs;
- continue-as-new for bounded history growth.

### Human and external interaction

- risk-based approval interrupts;
- generic `NodeHuman` decisions;
- approve/edit/reject/retry/confirm/abort;
- durable operator patches and payloads;
- targeted/broadcast-safe signals;
- deterministic signal routing;
- callback/awaitable tokens bound to plan + revision;
- stale callback rejection;
- durable payload-before-event ordering.

### Long-lived systems

- durable fixed-interval schedules;
- catch-up policy;
- deterministic schedule execution IDs;
- immutable historical snapshots;
- time-travel inspection;
- execution forks from historical versions;
- conservative compatible plan migration;
- multi-plan `Host`;
- durable child workflow handles;
- deterministic fan-out child IDs;
- child joins;
- retention and terminal execution GC;
- immutable-version pruning;
- optional archive hook before deletion.

### Observability

- durable ordered history;
- resumable `Watch` stream;
- `Explain` API;
- execution metrics;
- diagnostics with ready/waiting/active tasks;
- lease health;
- provider health;
- invariant audit;
- fleet audit;
- execution querying;
- content-addressed artifact references.

---

## Production quick start

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/Homiakus/axiom/adgo"
)

func main() {
    plan, err := adgo.Compile(adgo.Definition{
        ID:      "article",
        Version: "1",
        Nodes: []adgo.Node{
            {
                ID:       "draft",
                Kind:     adgo.NodeActivity,
                Activity: "Draft",
                Produces: []string{"draft"},
                Next:     []adgo.Transition{{To: "publish"}},
            },
            {
                ID:             "publish",
                Kind:           adgo.NodeActivity,
                Activity:       "Publish",
                DependsOn:      []string{"draft"},
                ExternalEffect: true,
                Risk:           adgo.RiskHigh,
                Timeout:        30 * time.Second,
                IdempotencyKey: "{execution}:{node}",
                Retry: &adgo.RetryPolicy{
                    MaxAttempts:      3,
                    MaxRetryDuration: 5 * time.Minute,
                },
                Compensation: "Unpublish",
            },
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    registry := adgo.NewRegistry()
    registry.Activity("Draft", draftHandler)
    registry.Activity("Publish", publishHandler)
    registry.Compensation("Unpublish", unpublishHandler)

    production, err := adgo.OpenProduction(
        plan,
        registry,
        adgo.DefaultProductionConfig("./var/adgo"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer production.Close()

    ctx := context.Background()
    _, err = production.Engine.StartOrLoad(
        ctx,
        "article-42",
        map[string]any{"topic": "Durable orchestration"},
        adgo.BudgetLimit{MaxCost: 10},
    )
    if err != nil {
        log.Fatal(err)
    }

    // Usually coordinator and workers are service goroutines/processes.
    go production.Engine.RunResilientCoordinator(ctx)
    go production.Engine.RunWorker(ctx, adgo.WorkerSpec{
        ID:          "worker-a",
        Concurrency: 8,
    })

    final, err := production.Engine.Await(ctx, "article-42", adgo.AwaitOptions{
        AcceptHuman: true,
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = final
}
```

A runnable no-network example lives at:

```bash
go run ./adgo/examples/production
```

---

## Production topology

### Single process

```text
                 +-----------------------+
                 |     immutable Plan    |
                 +-----------+-----------+
                             |
                   +---------v---------+
                   | resilient          |
                   | coordinator        |
                   +---------+---------+
                             |
                    durable task queue
                             |
             +---------------+---------------+
             |               |               |
        +----v----+      +----v----+      +---v-----+
        | worker 1|      | worker 2| ...  | worker N|
        +----+----+      +----+----+      +---+-----+
             |               |               |
             +---------------+---------------+
                             |
                  +----------v----------+
                  | Store / inbox /     |
                  | immutable versions  |
                  +---------------------+
```

Use:

```go
production.Serve(ctx, workers...)
```

or run coordinator and worker services separately when you need independent lifecycle control.

### Multiple processes on a shared filesystem

`FileStore` uses cross-process lock files + optimistic versions. Multiple coordinator/worker processes may share the same durable filesystem.

```text
coordinator A ----+
coordinator B ----+---- shared FileStore
worker A ---------+
worker B ---------+
```

Late workers are still fenced by task identity + worker ID + attempt + lease expiry.

### High-throughput local deployment

Use `BackendPebble` (the default in `DefaultProductionConfig`).

Pebble stores in one atomic KV database:

- latest execution state;
- immutable versions;
- inbox events;
- execution catalog.

Pebble's DB lock means one process owns that database path. Scale worker goroutines inside the process, or provide another shared-database `Store` implementation when you need multi-host state storage.

---

## The durable worker protocol

The production engine deliberately separates **scheduling** from **execution**.

```text
coordinator
  |
  | 1. derive ready set
  | 2. hard admission
  | 3. persist TaskPending
  v
Store
  ^
  | 4. worker Poll + CAS claim
  | 5. TaskRunning + WorkerID + LeaseUntil
  |
worker
  |
  | 6. handler executes
  | 7. heartbeat extends lease
  | 8. Complete / Fail validates fencing token
  v
Store
```

A worker result is accepted only when all of these still match:

- execution ID;
- task ID;
- worker ID;
- attempt;
- unexpired lease.

If a lease expires, the coordinator recovers the node to `pending`. A new worker gets a new attempt. The old worker becomes stale and receives `ErrStaleTask` if it tries to commit.

This prevents zombie workers from overwriting a newer result.

---

## Heartbeats

Automatic worker services heartbeat every `LeaseTTL/3`.

Long-running handlers can also publish explicit progress:

```go
registry.Activity("Render", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
    if err := adgo.ActivityHeartbeat(ctx, map[string]any{
        "page": 42,
    }); err != nil {
        return adgo.ActivityResult{}, err
    }
    // ...
})
```

Heartbeat does not mutate domain facts. It only extends the current fenced lease and optionally writes audit history.

---

## Graceful worker drain

For rolling deployments:

```go
service, _ := adgo.NewWorkerService(engine, adgo.WorkerSpec{
    ID:          "worker-7",
    Concurrency: 16,
})

go service.Run(ctx)

// Synchronous barrier: after return, this service cannot claim new work.
service.BeginDrain()

shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
_ = service.Drain(shutdownCtx)
```

Do not cancel the handler context merely to deploy a new binary unless you explicitly want the activity classified as a failure/retry.

---

## Failure semantics

| Failure | Default behavior |
|---|---|
| `transient` | bounded retry + backoff |
| `rate_limit` | bounded retry + `RetryAfter` + durable throttle |
| `invalid_input` | targeted repair or human escalation |
| `quality` | targeted repair |
| `permanent` | fail + compensation |
| `ambiguous_side_effect` | durable reconciliation interrupt |
| worker disappeared | lease recovery + new attempt |
| repeated worker loss | operator recovery quarantine |
| coordinator died during compensation | resilient coordinator resumes compensation |

Classify known errors:

```go
return adgo.ActivityResult{}, adgo.Fail(adgo.FailureTransient, err)
return adgo.ActivityResult{}, adgo.RateLimited(30*time.Second, err)
return adgo.ActivityResult{}, adgo.Fail(adgo.FailureQuality, err)
return adgo.ActivityResult{}, adgo.Fail(adgo.FailureAmbiguousSideEffect, err)
```

---

## External effects and idempotency

ADGO does not claim exactly-once I/O.

Before external work starts, its task and idempotency key are durable. A process crash can therefore cause redelivery.

External handlers should do at least one of:

1. pass `ActivityRequest.IdempotencyKey` to the provider;
2. persist a provider transaction ID and detect prior completion;
3. reconcile an ambiguous effect before retry.

Compiler requirements for external-effect nodes include timeout, idempotency and bounded retry. High-risk reversible effects may also require compensation.

---

## Adaptive provider routing

Register capability providers:

```go
registry.Provider("llm", adgo.Provider{
    Name:       "primary",
    Activity:   "OpenAIPrimary",
    Quality:    .95,
    Cost:       .8,
    Latency:    2 * time.Second,
    Privacy:    .9,
    Risk:       adgo.RiskLow,
    Permissions: []string{"network:llm"},
})
```

`AdaptiveRouter` filters hard constraints first. Only valid providers are scored.

Online feedback updates:

- reliability;
- latency EWMA;
- quality EWMA;
- cost EWMA;
- consecutive failures;
- circuit-open deadline.

`OpenProduction` uses durable provider health, so a restarted coordinator does not immediately forget that a provider is failing.

When a selected provider fails transiently, the failure is reported. The next durable retry re-resolves the capability and can select a healthy fallback.

---

## Global admission and rate limiting

Local scheduler limits protect one plan execution. `AdmissionController` protects a provider or resource **across executions/processes**.

```go
limited := adgo.WithAdmission(
    production.Admission,
    "provider:openai",
    adgo.AdmissionPolicy{
        MaxConcurrent: 20,
        Rate:          100,
        Period:        time.Minute,
        Burst:         20,
    },
    30*time.Second,
    handler,
)
```

A denied permit becomes `FailureRateLimit`, so normal durable retry/backoff handles it.

Permits expire after crashes.

---

## Shared activity-result cache

For pure deterministic work:

```go
cached := adgo.WithResultCache(
    production.Cache,
    "ExtractStructuredFacts",
    adgo.CachePolicy{
        Namespace: "extractor-v3",
        TTL:       24 * time.Hour,
    },
    handler,
)
```

The cache key is content-addressed from namespace + activity + sorted input facts + artifact refs.

It supports a single-flight lease: when another execution is already computing the same key, the second activity receives a durable rate-limit retry instead of paying for duplicate work.

**Do not wrap irreversible external side effects in result cache unless replaying the cached result is explicitly part of your idempotency contract.**

---

## Hedged and ensemble execution

For pure LLM/tool calls with tail-latency or quality variance:

```go
activity, err := adgo.NewEnsembleActivity(
    []adgo.ActivityVariant{
        {Name: "fast", Handler: fastModel},
        {Name: "strong", Handler: strongModel},
    },
    adgo.SpeculationPolicy{
        Pure:        true,
        MaxParallel: 2,
        MinQuality:  .85,
    },
)
```

`NewHedgedActivity` starts alternatives progressively after `HedgeDelay`.

`NewEnsembleActivity` runs variants and picks the maximum `QualityUtility` result with deterministic tie-breaking.

Budget usage is the sum of **all launched variants**, not merely the winner.

Speculation refuses to initialize unless `Pure=true`.

---

## Targeted repair

A failed quality gate does not restart the entire workflow.

```text
failed gate
    |
    v
repair roots
    |
    v
dependency intersection
    |
    v
minimal affected subgraph
    |
    +--> invalidate affected facts/artifacts
    +--> preserve unrelated completed work
    +--> increment durable revision epoch
    +--> rerun
```

Every repair cycle must be bounded by:

```go
&adgo.LoopBound{
    MaxIterations: 4,
    MaxCost:       3,
    MaxDuration:   10 * time.Minute,
    Epsilon:       .001,
}
```

ADGO additionally detects gate-to-gate stagnation and short-period strategy oscillation.

When multiple gates repair the same downstream work, use independent repair anchors so each gate owns its own durable budget and revision identity.

---

## Human-in-the-loop

High-risk activities can pause before execution. Generic `NodeHuman` can pause anywhere in a graph.

Resolve with:

```go
_, err := engine.ResolveHuman(ctx, executionID, nodeID, adgo.HumanResolution{
    Decision: adgo.HumanEdit,
    Actor:    "reviewer@example",
    Reason:   "corrected recipient",
    Patch: map[string]any{
        "recipient": "new@example.com",
    },
})
```

Supported decisions:

- approve;
- edit;
- reject;
- retry;
- confirm;
- abort.

Operator patch/payload is committed before execution resumes.

---

## Durable signals and callback tokens

Prefer `SignalDeterministic` for external webhooks.

If more than one node waits for the same untargeted event type, ADGO rejects ambiguity unless broadcast is explicitly enabled.

For provider callbacks:

```go
awaitable, err := engine.Awaitable(ctx, executionID, "remote_job")
// send awaitable.ID to the external system

err = engine.ResolveAwaitable(ctx, awaitable, callbackPayload)
```

The token is bound to:

- execution;
- node;
- expected event;
- revision;
- plan digest.

A late callback from an obsolete repair iteration is rejected as stale.

---

## Child workflows and fan-out

`Host` serves multiple immutable plan versions.

```go
host, _ := adgo.NewHost(store)
host.Register(parentPlan, parentRegistry)
host.Register(childPlan, childRegistry)

handle, child, err := host.StartChild(
    ctx,
    "parent-42",
    "research",
    "source-7",
    adgo.PlanRef{Digest: childPlan.Digest},
    adgo.ChildOptions{Initial: map[string]any{"url": url}},
)
```

Child ID is deterministic from parent execution + parent node + item ID. Parent activity redelivery cannot create a duplicate child.

`StartChildren` supports bounded production fan-out; `InspectChildren` applies `ALL`, `ANY`, `N_OF_M` or `QUORUM` joins.

---

## Time travel and forks

Every durable file/Pebble commit can be retained as an immutable version.

```go
snapshot, err := adgo.InspectVersion(ctx, store, "execution-1", 17)

fork, info, err := adgo.ForkExecution(
    ctx,
    store,
    plan,
    "execution-1",
    17,
    "execution-1-alternative",
    adgo.ForkOptions{
        DataPatch: map[string]any{"strategy": "alternative"},
    },
)
```

The fork clears active worker leases/events, preserves committed facts and completed work, and starts under a new execution ID.

No probabilistic activity is re-run merely to reconstruct history.

---

## Plan migration

Long-lived workflows can outlive a deployment version.

`MigrateExecution` supports conservative migration at a quiescent point:

- no active tasks;
- source plan pin must match;
- old-to-new node mapping must be unique;
- completed node semantics cannot silently change by default;
- newly added nodes are initialized;
- selected reset roots invalidate target-plan descendants;
- migration is recorded in durable history.

Use `AllowSemanticChange` only when you intentionally accept reinterpretation of completed work.

---

## Continue-as-new

For executions that logically continue forever:

```go
fresh, err := engine.ContinueAsNew(
    ctx,
    "monitor-2026-08",
    "monitor-2026-09",
    adgo.ContinueOptions{
        CarryData: []string{"model", "baseline"},
        Reason:    "monthly history compaction",
    },
)
```

The old execution closes; the new one receives fresh control state and selected durable facts/artifacts.

---

## Durable schedules

```go
schedule, err := production.ScheduleRunner.Register(ctx, adgo.Schedule{
    ID:         "hourly-analysis",
    Every:      time.Hour,
    StartAt:    time.Now().UTC(),
    CatchUp:    true,
    MaxCatchUp: 24,
    Initial:    map[string]any{"mode": "scheduled"},
})
```

A firing uses a deterministic execution ID derived from schedule ID + fire time. If the scheduler crashes after starting the execution but before advancing its schedule cursor, retry resumes the same execution instead of duplicating it.

---

## Pause, rewind and operator recovery

```go
_, _ = engine.Pause(ctx, id, "maintenance")
_, _ = engine.Resume(ctx, id, "operator")

_, err := engine.RewindFrom(
    ctx,
    id,
    "factcheck",
    "source corrected",
    "operator",
)
```

`RewindFrom` requires a quiescent execution and invalidates only the selected node plus its descendants. Every affected node gets a new revision epoch, preventing stale idempotency-cache reuse.

---

## Compensation and crash recovery

Compensations execute in reverse order.

For flaky compensators:

```go
registry.Compensation("Refund", adgo.WithCompensationPolicy(
    adgo.DefaultCompensationPolicy(),
    refund,
))
```

Use `RunResilientCoordinator` / `ServeResilient` in production. If a process dies while the execution is `compensating`, the next coordinator resumes the remaining stack.

Compensation is also at-least-once; compensation handlers need idempotency.

---

## Storage matrix

| Backend | Durable | Immutable versions | Catalog | Cross-process | Best use |
|---|---:|---:|---:|---:|---|
| `MemoryStore` | no | no | yes | no | tests / ephemeral |
| `FileStore` | yes | yes | yes | shared filesystem | simple multi-process |
| `PebbleStore` | yes | yes | yes | one DB owner | high-throughput local |
| custom `Store` | depends | optional | optional | depends | SQL/KV/cloud adapter |

Production polling requires `ExecutionCatalog`.

Time-travel inspection requires `VersionedStore`.

Retention deletion requires `ExecutionDeletionStore`.

Version pruning requires `VersionPruner`.

These are intentionally capability interfaces rather than one monolithic storage contract.

---

## Diagnostics and audit

```go
report, err := engine.Diagnostics(ctx, executionID)
```

Diagnostics include:

- ready nodes;
- durable waits;
- active tasks;
- worker IDs;
- lease deadlines;
- budget usage;
- quality;
- provider health;
- invariant violations.

`AuditExecution` detects states such as:

- plan pin mismatch;
- orphan task/node;
- running task without worker/lease;
- expired lease;
- node running without task;
- waiting node without reason;
- completed node with active task;
- broken history sequence;
- negative budget;
- terminal execution with active work.

`AuditFleet` checks all cataloged executions against loaded plans.

---

## Watch and query

Durable history can be streamed with resume-by-sequence:

```go
events, errs := engine.Watch(ctx, executionID, adgo.WatchOptions{
    FromSeq:        lastSeen,
    StopOnTerminal: true,
})
```

`QueryExecutions` filters the catalog by plan, digest, status and update window.

These are projections of committed state, not volatile callbacks.

---

## Retention and archival

Nothing is deleted automatically.

Explicit policy:

```go
result, err := adgo.CollectExecutions(ctx, store, adgo.RetentionPolicy{
    TerminalFor: 30 * 24 * time.Hour,
    Archive: adgo.JSONArchive(uploadToObjectStore),
})
```

The archive hook must succeed before deletion.

Immutable versions can be compacted separately with `VersionPruner.PruneVersions`.

---

## Security model

ADGO treats durable execution storage as sensitive application state.

Recommended rules:

- keep credentials in worker environment / secret manager, not `Execution.Data`;
- persist secret references, not raw tokens;
- use `RequiredPermissions` and provider permissions as hard constraints;
- use risk thresholds and human approval for dangerous effects;
- use resource keys for exclusive external resources;
- never derive external side-effect idempotency from random process state;
- validate adaptive plan proposals before compiling them;
- never let an LLM directly edit a live parent plan or execution state;
- protect FileStore/Pebble directories with OS-level access controls;
- archive before retention deletion when audit requirements demand it.

`PatchData` and human patches reject reserved `__adgo:` keys.

---

## Production checklist

Before deploying a workflow:

1. `Compile` succeeds with no structural violations.
2. Every external effect has timeout + idempotency + bounded retry.
3. Reversible high-risk effects have compensation.
4. All repair loops have iteration/cost/duration/epsilon bounds.
5. Global + capability/activity/resource limits are declared.
6. Shared provider limits use `WithAdmission` where necessary.
7. Pure expensive work uses cache only when semantics permit reuse.
8. Workers use finite leases and heartbeat long calls.
9. Deployments use graceful drain.
10. Production coordinator is compensation-aware (`RunResilientCoordinator`).
11. Human/callback paths use durable signals or awaitables.
12. Long-lived executions have continue-as-new / retention policy.
13. Secrets remain outside durable facts.
14. Operator tooling exposes diagnostics/history.
15. Failure/recovery tests cover worker death, retries, repair and side-effect ambiguity.

---

## Package map

```text
adgo/
├── compiler.go                immutable plan compiler
├── types.go                   public contracts
├── runtime.go                 embedded deterministic kernel
├── engine.go                  production coordinator/worker engine
├── host.go                    multi-plan host
├── production.go              batteries-included setup
├── service.go                 resilient serve + graceful worker drain
├── store.go                   memory/file stores
├── pebble_store.go            high-throughput durable store
├── catalog.go                 execution catalog
├── scheduler.go               utility + hard admission scheduler
├── admission.go               cross-process concurrency/rate limits
├── registry.go                activity/gate/provider registry
├── router.go                  adaptive provider routing
├── router_store.go            durable provider-health state
├── repair.go                  targeted repair / convergence
├── compensation_recovery.go   resilient saga recovery
├── control.go                 operator + HITL control plane
├── signal_safe.go             deterministic durable signals
├── awaitable.go               external callback tokens
├── child_workflow.go          production child workflows
├── subflow.go                 embedded fan-out
├── time_travel.go             historical inspection/forks
├── migration.go               compatible live migration
├── lifecycle.go               continue-as-new
├── schedule.go                durable schedules
├── cache.go                   shared activity result cache
├── speculation.go             hedged/ensemble pure execution
├── operations.go              query/await/rewind
├── diagnostics.go             execution/fleet invariant audit
├── watch.go                   resumable history stream
├── retention.go               GC/archive/version pruning
├── artifact.go                content-addressed artifact store
├── replay.go                  immutable snapshot replay audit
├── explain.go                 explainability
├── metrics.go                 execution metrics
└── examples/
    ├── iris/
    └── production/
```

---

## Validation

Repository CI runs:

```bash
go mod tidy
go test ./...
go test -race ./...
go vet ./...
```

plus dependency scanning, fuzz smoke tests, external-consumer compilation and performance benchmarks.

For ADGO-specific development:

```bash
go test ./adgo/... -count=1
go test -race ./adgo -count=1
go vet ./adgo/...
go run ./adgo/examples/production
```

The test suite contains explicit regressions for worker fencing, heartbeat recovery, durable routing, repair anchors, plan migration, historical forks, callback awaitables, schedules, Pebble reopen, compensation recovery, activity caching, deterministic signal routing, operator rewind, speculation, graceful drain, retention and multi-plan hosting.

---

## Non-goals and boundaries

ADGO is an orchestration engine, not a message broker or secret manager.

A production deployment still chooses:

- network transport for remote workers if workers are not linked into the process;
- authentication/authorization around your control API;
- a custom shared database Store when shared-filesystem storage is insufficient;
- object storage for large artifacts;
- metrics/log export backend;
- application-specific provider clients.

The engine deliberately exposes narrow interfaces for these boundaries instead of hard-coding one infrastructure stack.
