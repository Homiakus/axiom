# Runtime semantics and guarantee boundaries

This document is the source of truth for runtime behavior that is implemented and covered by tests. Parser support alone is not a runtime guarantee.

## Execution

- An execution is identified by a string ID.
- Operations for one ID are serialized inside one `Engine`.
- Different IDs may run concurrently.
- Execution locking is not a distributed/process-wide ownership protocol.
- `Run.Dispatch` creates a missing execution automatically.
- `WithProductionMode()` requires a `TransactionalStore` and strict fast-runtime compatibility.

## Activities

External/local activities are represented by durable `ActivityTask` records. A task stores input, status, attempt budget, lease data, `NextAttemptAt`, result/error, and timestamps.

Application handlers can use either the dynamic `axiom.Act` boundary or `axiom.ActTyped`. Typed activity shape errors are rejected while the `Engine` is built (`AX507`).

### Durable retry

`retry: N` means at most `N + 1` handler attempts.

Each attempt owns one task lease. If a retryable handler failure occurs and budget remains, runtime atomically returns the task to `pending`, clears the lease, persists the error and `NextAttemptAt`, and appends `ActivityRetryScheduled`.

A later `Engine` using the same store can continue the task. Pebble persists the checkpoint across close/reopen. When the budget is exhausted, runtime appends `ActivityRetryExhausted` and processes the terminal failure.

Supported backoff forms:

```axiom
backoff: 250ms
backoff: fixed(250ms)
backoff: exponential(100ms)
```

The Go builder exposes `PolicyBuilder.Backoff` and `ExponentialBackoff`. Without explicit backoff, runtime uses deterministic exponential delay starting at `100ms`, capped at `30s`.

`Run.Dispatch`, `Run.Signal`, and `Run.Patch` keep synchronous ergonomics: they wait for due retries while the caller context remains alive. Low-level `Engine.RunUntilIdle` does not sleep until future work; after persisting a retry checkpoint it returns an error matching `axiom.ErrRetryScheduled`.

### Timeout

`timeout` creates a fresh `context.WithTimeout` for each handler attempt. A handler must observe `ctx`; arbitrary Go code that ignores cancellation cannot be forcibly stopped safely.

A per-attempt timeout is retryable. Cancellation of the parent caller context stops the synchronous drain.

## Concurrency policies

### `parallel`

Adds no activity-level serialization.

### `once`

Serializes calls of one activity inside one `Engine`. This is process-local and is not a distributed lock.

### `first`

The lane is `execution ID + activity name`. If a pending or running task already owns the lane, newly scheduled unkeyed work is persisted as `TaskSuperseded`. The earliest active task wins.

### `latest`

Older **pending** tasks in the same lane are superseded by the newest pending task. A currently running Go handler is never force-cancelled. Therefore the guarantee is **latest pending wins**, not arbitrary running-code cancellation.

Supersession is audited with `ActivitySuperseded`. In production the scheduling decision is made inside the `TransactionalStore` transaction. Pebble serializes its transactions on the store instance; a custom transactional store must provide sufficient isolation for correct scheduling.

Explicit non-empty idempotency keys take precedence over `first/latest`: the same external intent is deduplicated before supersession.

## Policy catch routing

A policy may map terminal activity failures to signals:

```axiom
policy payment:
  retry: 2
  backoff: exponential(100ms)
  timeout: 3s
  concurrency: parallel
  catch:
    PaymentDeclined -> PaymentDeclinedSignal
    * -> PaymentFailureSignal
```

Catch routing happens **only after retry budget is exhausted or a handler failure is otherwise terminal**. Intermediate retry attempts never emit catch signals.

### Stable error codes

Application code should return a typed domain failure:

```go
return nil, axiom.FailActivity("PaymentDeclined", err)
```

Custom errors may implement:

```go
type ActivityErrorCoder interface {
    ActivityErrorCode() string
}
```

Runtime does not infer catch keys from arbitrary error strings. Exact coded mapping wins; `*` is the fallback for unmatched or uncoded terminal handler failures.

The Go model builder exposes:

```go
policy.Catch("PaymentDeclined", "PaymentDeclinedSignal")
policy.CatchAll("PaymentFailureSignal")
```

### Catch signal payload

Runtime provides these metadata fields to the catch signal environment:

- `activity`
- `rule`
- `taskId`
- `errorCode`
- `error`
- `attempt`
- `maxAttempts`

A target signal only needs to declare fields its rules reference.

### Atomicity and failure

Terminal task failure plus catch signal/rule processing runs in the same store transaction. On success the task remains terminal failed for audit, `ActivityFailed` is marked `caught: true`, `ActivityCaught` is appended, catch rules apply their writes, and execution returns to `Waiting`.

If catch rule processing fails (for example because a claim rejects its write), runtime returns `AX511` and rolls the catch transaction back. No partial catch signal/history/context writes are committed. Because the handler attempt was leased before that transaction, the original task may remain `running` until normal lease recovery; external handlers must therefore remain idempotent.

Output-contract errors such as missing/wrong typed activity output (`AX503`/`AX504`) are deterministic runtime contract violations and are not routed through domain catch mappings.

## Idempotency

Task deduplication is a store/runtime guarantee, not exactly-once delivery to networks, payment systems, or equipment. Durable retry, lease recovery, and catch rollback can all result in another handler attempt after an external side effect may already have occurred.

For `effect: external`, the compiler requires `idempotency: required` and an `idempotencyKey`. The handler should forward the same business key to the external system or maintain its own deduplication record.

## Claims and writes

Claims are checked before and after writes. A violating write is restored in the working execution snapshot and the operation fails.

A local transaction can roll back persisted execution/history/task changes; it cannot roll back an already completed external side effect.

## History

Important runtime entries include:

- `ExecutionStarted`
- `ContextPatched`
- `SignalReceived`
- `RuleScheduled` / `RuleSkipped` (full trace)
- `RulesEvaluated` (aggregate trace)
- `ActivityScheduled`
- `ActivityDeduplicated`
- `ActivitySuperseded`
- `ActivityRetryScheduled`
- `ActivityRetryExhausted`
- `ActivityCompleted`
- `ActivityFailed`
- `ActivityCaught`
- `WriteApplied`
- `ExecutionReachedFixpoint`
- `ExecutionCanceled`

`ActivityFailed` is terminal. For a successfully caught domain failure it is still recorded for task audit but includes `caught: true`, followed by `ActivityCaught`.

## Lease recovery

If a process disappears after leasing a task but before complete/fail persistence, `RecoverExpiredLeases` can return the task to `pending`. The next lease increments `Attempt` because runtime cannot prove whether the previous handler already began an external effect.

## Runtime query namespace

Stable query fields are:

- `runtime.id`
- `runtime.domain`
- `runtime.status`
- `runtime.version`
- `runtime.createdAt`
- `runtime.updatedAt`
- `runtime.moduleHash`
- `runtime.compilerVersion`
- `runtime.planVersion`

Canonical Plan/Open/New paths reject unsupported projections with `AX001`. Go models should prefer the discoverable `model.Runtime.*` helpers.

## Production mode

`WithProductionMode()` currently guarantees:

1. a `TransactionalStore` is required;
2. strict fast runtime is enabled;
3. durable retry/backoff and per-attempt timeout are enabled;
4. `parallel`, `once`, `first`, and `latest` are accepted;
5. retry checkpoints, supersession decisions, and successful catch processing are persisted transactionally;
6. unknown concurrency values are rejected with `AX508`.

## Replay

Replay rebuilds execution state from history, verifies the module hash, and does not re-run already completed activity effects.

## Typed Go Flow

Typed Go Flow has a different durability boundary:

```text
load -> reducer -> claims -> effects -> save
```

Effects happen before `FlowStore.Save`, so a successful external effect followed by a save failure cannot be rolled back. Flow effect handlers must be idempotent.

## Remaining reliability work

1. distributed execution ownership/coordination across processes;
2. wall-clock timer scheduler semantics;
3. multi-file AXM resolver/linker;
4. operational recovery tooling for stuck leases and failed catch transactions;
5. root public API classification/cleanup before `v1.0.0`.
