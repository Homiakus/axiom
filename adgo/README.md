# ADGO — Adaptive Durable Graph Orchestration

ADGO is a reference implementation of a durable, adaptive graph orchestrator for Axiom.
It is intended for complex workflows such as IRIS where deterministic control must coexist
with probabilistic workers (LLMs, search, extraction, agents and other external tools).

> **Deterministic control plane + adaptive execution plane.**

The runtime does not let an LLM mutate execution state or control flow directly. Activities
return facts and artifacts; compiled rules, gates, policies and validated child plans decide
what may happen next.

## What is implemented

- immutable, digest-pinned `Plan` compiled from a declarative `Definition`;
- graph execution instead of a stage cursor;
- typed node primitives: activity, decision, gate, fork, join, wait, human, subflow and compensation;
- static validation for missing references/data, unreachable nodes, unsafe external effects,
  conflicting writers, unbounded cycles, invalid joins and permission errors;
- dependency-derived ready sets and parallel super-steps;
- global, per-activity and per-capability concurrency limits plus resource-key exclusion;
- capability-based provider selection using quality/cost/latency/privacy/risk constraints;
- durable execution snapshots and a durable event inbox;
- cross-process compare-and-swap commits for the filesystem store;
- persisted activity leases and expired-lease recovery;
- bounded retries with exponential backoff and deterministic jitter;
- durable provider throttling after rate-limit failures;
- explicit failure classification: transient, rate-limit, invalid-input, quality,
  permanent and ambiguous-side-effect;
- risk-based human approval and persisted human/wait interrupts;
- durable timers and external events;
- budget enforcement for cost, tokens, duration, LLM calls, searches and browser fetches;
- quality vectors and hard quality gates;
- dependency-directed targeted repair rather than blind pipeline restart;
- bounded repair loops, gate-to-gate convergence detection and oscillation detection;
- compensation stack executed in reverse order on failure/cancellation;
- content-addressed artifact storage using SHA-256;
- validated adaptive `PlanProposal` -> immutable child `Plan` flow;
- bounded dynamic child execution/fan-out with `ALL`, `ANY`, `N_OF_M` and `QUORUM` joins;
- execution explanations (`Explain`) and built-in metrics;
- immutable snapshot audit replay (`VerifyReplay`).

## Package layout

```text
adgo/
├── artifact.go          content-addressed artifact storage
├── compiler.go          plan compiler + static graph validation
├── explain.go           explainability API
├── metrics.go           execution observability
├── registry.go          activities, capabilities, decisions, gates, compensations
├── repair.go            targeted repair + convergence + adaptive plan validation
├── replay.go            audit replay of committed snapshots
├── runtime.go           durable super-step runtime
├── scheduler.go         safe utility scheduler + concurrency/resource limits
├── store.go             Store interface, memory store and durable file store
├── subflow.go           bounded dynamic child executions / fan-out
├── types.go             public contracts and typed primitives
├── *_test.go            failure, recovery, repair and concurrency tests
└── examples/iris/       runnable IRIS-like workflow
```

## Core model

ADGO separates two kinds of state.

### Domain state

Large domain objects belong outside the orchestration runtime:

```text
Artifact
Sources
Evidence
Claims
Facts
Outline
Draft
Reviews
Exports
```

### Execution state

The orchestrator owns compact control state:

```text
ExecutionID
PlanID / PlanVersion / PlanDigest
Node runtime states
Active task leases
Attempts / revision counters
Quality vector
Budgets
Waiting events
Compensation stack
History
Artifact references
```

The runtime stores `ArtifactRef` values, not entire PDFs, source corpora or drafts.

## Execution lifecycle

Each call to `Step` is a durable super-step:

```text
RECOVER
   ↓
INGEST DURABLE EVENTS
   ↓
RECOVER EXPIRED LEASES
   ↓
CHECK BUDGET / CANCELLATION
   ↓
RUN DETERMINISTIC INTERNAL NODES
   ↓
DERIVE READY SET
   ↓
FILTER BY PERMISSION / RISK / RESOURCE / BUDGET / THROTTLE
   ↓
SCHEDULE SAFE PARALLEL WORK
   ↓
PERSIST TASK LEASES
   ↓
EXECUTE ACTIVITIES IN PARALLEL
   ↓
CLASSIFY RESULTS
   ↓
ATOMIC COMMIT
   ↓
QUALITY GATE / TARGETED REPAIR / WAIT / HUMAN / COMPENSATE / COMPLETE
```

The call stack is not the source of truth. The committed execution is.

## Minimal example

```go
plan, err := adgo.Compile(adgo.Definition{
    ID:      "example",
    Version: "1",
    Nodes: []adgo.Node{
        {
            ID:       "draft",
            Kind:     adgo.NodeActivity,
            Activity: "Draft",
            Produces: []string{"draft"},
            Loop: &adgo.LoopBound{
                MaxIterations: 3,
                MaxCost:       5,
                MaxDuration:   10 * time.Minute,
                Epsilon:       0.001,
            },
            Next: []adgo.Transition{{To: "gate"}},
        },
        {
            ID:        "gate",
            Kind:      adgo.NodeGate,
            DependsOn: []string{"draft"},
            Gate: &adgo.QualityGateSpec{
                HardFloors: map[string]float64{"factuality": .95},
                RepairFrom: []string{"draft"},
            },
        },
    },
})
if err != nil {
    log.Fatal(err)
}

registry := adgo.NewRegistry()
registry.Activity("Draft", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
    return adgo.ActivityResult{
        Facts:   map[string]any{"draft": "artifact://draft/sha256:..."},
        Quality: adgo.QualityVector{"factuality": .97},
    }, nil
})

store, _ := adgo.NewFileStore("./var/adgo")
runtime, _ := adgo.NewRuntime(plan, store, registry)
_, _ = runtime.Start(context.Background(), "article-1", nil, adgo.BudgetLimit{MaxCost: 5})
execution, err := runtime.Run(context.Background(), "article-1")
```

## Activities report facts; policies decide control flow

An activity should not return an arbitrary next-stage name. It returns observations:

```go
adgo.ActivityResult{
    Facts: map[string]any{
        "criticalErrors": 2,
    },
    Quality: adgo.QualityVector{
        "factuality":       .87,
        "evidenceCoverage": .92,
    },
}
```

A deterministic gate then chooses `PASS`, `REPAIR`, `FAIL` or `HUMAN`.
This keeps probabilistic output out of the control plane.

## Durable side effects and idempotency

ADGO intentionally does **not** claim exactly-once external effects.

Before an external activity is called, its task, attempt, lease and idempotency key are
committed. A crash may therefore cause an activity to be called again. The handler must:

1. use `ActivityRequest.IdempotencyKey` with the external provider when possible;
2. detect a previously completed side effect; or
3. return `FailureAmbiguousSideEffect` and reconcile before retrying.

External-effect nodes must declare a timeout, idempotency key and bounded retry policy;
the compiler rejects unsafe definitions.

## Failure classification

Wrap known failures with `adgo.Fail` or `adgo.FailAfter`:

```go
return adgo.ActivityResult{}, adgo.Fail(adgo.FailureTransient, err)
return adgo.ActivityResult{}, adgo.FailAfter(adgo.FailureRateLimit, err, 20*time.Second)
return adgo.ActivityResult{}, adgo.Fail(adgo.FailureQuality, err)
return adgo.ActivityResult{}, adgo.Fail(adgo.FailureInvalidInput, err)
return adgo.ActivityResult{}, adgo.Fail(adgo.FailurePermanent, err)
return adgo.ActivityResult{}, adgo.Fail(adgo.FailureAmbiguousSideEffect, err)
```

Transient and rate-limit failures may retry inside their declared bounds. Invalid input
and quality failures go to repair. Ambiguous external effects wait for reconciliation.
Permanent failures fail and trigger compensation when applicable.

## Targeted repair

A quality gate can name deterministic repair roots:

```go
Gate: &adgo.QualityGateSpec{
    HardFloors: map[string]float64{
        "factuality":       .95,
        "evidenceCoverage": .90,
    },
    MaxCriticalErrors: 0,
    RepairFrom:        []string{"draft"},
}
```

`DependencyRepairPlanner` computes the smallest affected subgraph from the repair root to
the failed gate. Completed nodes outside that subgraph stay completed. Outputs produced by
the affected nodes are invalidated, while unrelated artifacts are preserved.

Every repair root must declare a complete `LoopBound`:

```go
&adgo.LoopBound{
    MaxIterations: 4,
    MaxCost:       3,
    MaxDuration:   15 * time.Minute,
    Epsilon:       .001,
}
```

Convergence is evaluated gate-to-gate, not between unrelated workflow snapshots.
Repeated semantic signatures are also detected as oscillation and the responsible strategy
is banned for the execution.

## Capability-based routing

Graphs can depend on capabilities instead of concrete providers:

```go
Node{
    ID:         "search",
    Kind:       adgo.NodeActivity,
    Capability: "FindEvidence",
}
```

Providers are registered independently:

```go
registry.Provider("FindEvidence", adgo.Provider{
    Name:        "web",
    Activity:    "SearchWeb",
    Quality:     .95,
    Cost:        .08,
    Latency:     500 * time.Millisecond,
    Privacy:     .80,
    Risk:        adgo.RiskLow,
    Permissions: []string{"network"},
})
```

`Registry.Resolve` filters hard requirements first and then selects among valid providers.
An unsafe provider can never win merely because it has a higher utility score.

## Human-in-the-loop

`NodeHuman` is a first-class durable interrupt. External effect activities at or above the
runtime approval threshold also require approval automatically.

```go
_ = runtime.Signal(ctx, executionID, adgo.Event{
    ID:         "approval-17",
    Type:       "Approved",
    TargetNode: "publish",
})
```

The event is written to a durable inbox first and acknowledged only after its state change
has been committed. Duplicate delivery is deduplicated by event ID.

## Compensation

A reversible side-effect node declares its compensation handler:

```go
Node{
    ID:             "publish",
    Kind:           adgo.NodeActivity,
    Activity:       "Publish",
    ExternalEffect: true,
    Compensation:   "Unpublish",
    // timeout, retry and idempotency omitted here for brevity
}
```

Successful side effects push compensation records. Failure or cancellation executes the
stack in reverse order.

## Dynamic plans without giving an LLM control of the runtime

An LLM or planner may produce an inert `PlanProposal`. The proposal becomes executable only
after deterministic validation:

```text
PlanProposal
    ↓
ValidatePlanDelta(policy)
    ↓
ValidatedPlanDelta
    ↓
CompileValidatedPlanDelta
    ↓
immutable child Plan
```

The parent plan remains digest-pinned and unchanged. This prevents information-plane text
from editing the active control graph, permissions, budgets or gates.

## Dynamic fan-out

`RunFanout` creates deterministic child execution IDs and resumes already-completed children
rather than duplicating them. Fan-out is bounded by `MaxFanout` and a concurrency limit.
Supported aggregation modes are `ALL`, `ANY`, `N_OF_M` and `QUORUM`.

## Storage

### MemoryStore

For tests and ephemeral runs.

### FileStore

A dependency-free durable reference store. Every commit is an immutable full execution
snapshot:

```text
root/
├── executions/<execution>/commits/00000000000000000001.json
├── executions/<execution>/commits/00000000000000000002.json
├── executions/<execution>/inbox/<event>.json
└── locks/<execution>.lock
```

Commits use temporary files, fsync and atomic rename. A lock file protects optimistic
compare-and-swap commits across processes that share the same filesystem.

For a multi-host deployment on storage without shared filesystem locking, implement the
`Store` interface on a transactional database with a native version/CAS primitive.

## Artifact storage

`ContentAddressedStore` stores content by SHA-256 and returns a compact `ArtifactRef`.
The same bytes are automatically deduplicated.

```go
store, _ := adgo.NewContentAddressedStore("./var/artifacts")
ref, _ := store.Put("draft.md", "text/markdown", reader)
```

## Explainability

```go
explanation := adgo.Explain(plan, execution, "quality_gate")
```

It reports status, blockers, attempts, iterations, retry time, expected event, committed
outcome, plan digest and other evidence relevant to the node.

## Replay model

`VerifyReplay` audits immutable committed snapshots. It verifies plan pinning, version
continuity, history monotonicity, node identities and monotonic budget usage.

It deliberately does not re-run LLMs or other non-deterministic activities. Their committed
facts and artifact digests form the replay boundary.

## Safety model

ADGO separates the control plane from the information plane.

Control plane:

```text
Plan / gates / permissions / budgets / policies / registered capabilities
```

Information plane:

```text
web pages / PDFs / source text / user documents / LLM responses
```

Information-plane content cannot directly create an activity, grant a permission, increase
a budget, replace the pinned plan or disable a gate.

## Guarantees and deliberate limits

ADGO provides a concrete reference implementation of durable local orchestration, but the
following boundaries are intentional:

- external effects are at-least-once, not exactly-once;
- `FileStore` coordinates processes sharing one filesystem, not arbitrary distributed hosts;
- audit replay verifies committed deterministic state instead of re-executing probabilistic work;
- adaptive planning creates validated immutable child plans instead of mutating the parent plan;
- large artifact retention/garbage collection is a deployment policy, not hidden runtime magic;
- provider-specific distributed rate limiting should be backed by a shared store in a cluster.

These limits keep failure semantics explicit and testable.

## IRIS reference workflow

Run the complete example:

```bash
go run ./adgo/examples/iris
```

The example demonstrates:

- capability-based research;
- evidence extraction and drafting;
- parallel fact-check/editorial work;
- a failed first quality gate;
- targeted draft/fact-check repair while unrelated editorial work is preserved;
- content-addressed draft artifacts;
- a high-risk publication activity stopped by a durable approval interrupt;
- approval event and resume;
- idempotent publication key;
- final explanation and metrics.

## Validation

```bash
go test ./adgo/...
go vet ./adgo/...
go run ./adgo/examples/iris
```

The tests include plan validation, parallelism, retry, rate-limit throttling, durable timers,
human interrupts, targeted repair, convergence regression, expired lease recovery,
compensation order, artifact deduplication, child fan-out/resume and snapshot replay.
