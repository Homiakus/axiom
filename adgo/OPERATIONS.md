# ADGO production operations runbook

This runbook is for operators and developers responsible for a live ADGO deployment.

It assumes production code uses `Engine`/`Host`, durable storage and `RunResilientCoordinator` rather than only embedded `Runtime.Run`.

## 1. First checks

For one execution:

```go
report, err := engine.Diagnostics(ctx, executionID)
```

Inspect in this order:

1. `Summary.Status` and `Summary.Failure`;
2. `Diagnostics` errors/warnings;
3. `ActiveTasks` + `LeaseLeft`;
4. `Waiting`;
5. `Ready`;
6. `Budget` / `BudgetLimit`;
7. `ProviderHealth`;
8. durable `History` / `Watch`.

For a fleet:

```go
report, err := adgo.AuditFleet(ctx, store, plansByDigest)
```

Never repair an execution by editing store files/keys manually. Use the control/migration/rewind APIs so invariants and history remain correct.

---

## 2. Execution status guide

### `running`

Normal if it has:

- ready work;
- pending/running durable tasks;
- deterministic internal progress.

Suspicious when diagnostics reports `RUNNING-NO-TASK` and no ready nodes exist.

### `waiting`

Normal for:

- durable timer;
- retry `NotBefore`;
- callback/event;
- explicit execution pause.

Check `WaitingFor` and node `NotBefore`.

### `awaiting_human`

Requires an explicit operator/user decision. Use `ResolveHuman`, not direct store mutation.

### `compensating`

The resilient coordinator should resume the compensation stack automatically after restart.

If it stays here, inspect:

- registered compensation handler;
- handler timeout/retry policy;
- last compensation history entry;
- external idempotency behavior.

### `deadlocked`

Read `Failure` and `Explain`. A deadlock means no runnable work/timer/event path exists for still-activated incomplete nodes.

Do not blindly set nodes pending. Correct the plan/data or use an intentional rewind/migration.

### terminal statuses

`completed`, `failed`, `canceled`, `deadlocked` may be selected for retention once audit/archival requirements permit.

---

## 3. Worker disappeared

Symptoms:

- running task;
- lease deadline passed;
- diagnostics `ADG-DIAG-LEASE-EXPIRED`.

Expected recovery:

1. resilient coordinator sees expired lease;
2. old task is recovered;
3. node returns to pending;
4. next durable attempt is enqueued;
5. another worker claims it;
6. old worker is fenced and receives `ErrStaleTask` if it returns late.

No operator action is normally required.

If the same node repeatedly loses workers, ADGO quarantines it after `MaxLeaseRecoveries` and waits for `OperatorRecoveryDecision`.

Resolve only after investigating resource exhaustion, process crashes, provider hangs or handler bugs.

---

## 4. Worker rolling deployment

Preferred sequence:

```go
service.BeginDrain()
err := service.Drain(shutdownCtx)
```

`BeginDrain` is the synchronous no-new-claims barrier.

After it returns:

- no new task will be claimed by this service;
- already claimed work continues heartbeat;
- `Drain` waits for active handlers.

Only hard-cancel the process context if you intentionally accept task retry/recovery.

---

## 5. Provider outage

Inspect `ProviderHealth`:

- `ConsecutiveFailures`;
- `CircuitOpenUntil`;
- `LastFailure`;
- `LastError`;
- EWMA latency/quality/cost.

A transient or rate-limit failure penalizes/opens the provider circuit. On the next node retry, capability resolution may select another healthy provider.

If the provider has recovered and you must clear history manually:

```go
err := router.ResetContext(ctx, capability, provider)
```

Prefer natural cooldown unless you have external evidence the outage is over.

---

## 6. Global provider saturation

If activities return durable `rate_limit` failures from `WithAdmission`, inspect the admission key/policy.

```go
snapshot, err := production.Admission.Snapshot(ctx, "provider:openai")
```

Look at:

- `InFlight`;
- token count;
- refill time.

Do not increase concurrency merely because a workflow is waiting. The limiter protects the external dependency across workflows/processes.

Expired permits recover automatically after crashed workers.

---

## 7. Activity keeps retrying

Check:

- failure class;
- node `Attempts`;
- `FirstAttemptAt`;
- retry `NotBefore`;
- `RetryPolicy.MaxAttempts`;
- `MaxRetryDuration`;
- provider health;
- admission state.

A workflow must not retry forever. Compiler/runtime bounds are part of the safety model.

If the underlying issue requires corrected input, prefer an operator patch + rewind rather than artificially increasing retry count.

---

## 8. Ambiguous external side effect

Example: payment/provider accepted the request, but worker lost the HTTP response before ADGO committed completion.

Expected state:

```text
ExecutionStatus = awaiting_human
WaitingFor[node] = Reconcile:<node>
```

Investigate the provider using the ADGO activity idempotency key.

Then choose:

### Provider confirms effect happened

```go
engine.ResolveHuman(ctx, id, node, adgo.HumanResolution{
    Decision: adgo.HumanConfirm,
    Actor:    operator,
    Reason:   "provider transaction tx-123 exists",
})
```

### Provider confirms effect did not happen

```go
Decision: adgo.HumanRetry
```

### Effect cannot be safely accepted

```go
Decision: adgo.HumanAbort
```

Never guess and blindly retry an irreversible effect.

---

## 9. Human approval/edit/reject

Use `ResolveHuman`.

Operator corrections belong in `Patch` so they are committed with the decision.

```go
_, err := engine.ResolveHuman(ctx, id, node, adgo.HumanResolution{
    Decision: adgo.HumanEdit,
    Actor:    "alice",
    Reason:   "corrected destination",
    Patch: map[string]any{
        "destination": "safe-target",
    },
})
```

Reserved `__adgo:` keys cannot be patched through the public operator API.

---

## 10. External callback never arrives

Obtain/inspect the current awaitable:

```go
awaitable, err := engine.Awaitable(ctx, id, node)
```

Important: callback token includes current repair revision. A callback from an older revision is intentionally stale.

Check:

- external system received the current token;
- correct event type;
- callback target execution/node;
- webhook auth at your application boundary.

`ResolveAwaitable` commits payload before inbox event. Retrying the same callback is safe.

---

## 11. Ambiguous signal target

`SignalDeterministic` rejects an untargeted event when several nodes wait for the same event type.

Fix by either:

- setting `Event.TargetNode`;
- intentionally enabling broadcast.

Do not fall back to nondeterministic map-order routing.

---

## 12. Quality gate cannot converge

Inspect:

- gate quality snapshots;
- repair roots;
- revision counters;
- `LoopBound`;
- oscillation signatures;
- preserved vs invalidated artifacts.

ADGO can escalate when:

- iteration bound exhausted;
- cost bound exhausted;
- duration bound exhausted;
- improvement falls below epsilon;
- strategy oscillation repeats.

Operator options:

1. patch missing/corrected facts;
2. rewind from a deterministic repair root;
3. approve a recovery only when the new strategy/input can actually change the result;
4. migrate to a corrected plan if the graph itself is wrong.

Increasing the bound without changing the cause is usually not a repair.

---

## 13. Manual rewind

Only at a quiescent point:

```go
_, err := engine.RewindFrom(ctx, id, "factcheck", "source fixed", operator)
```

Effects:

- root + descendants invalidated;
- produced facts/artifacts removed;
- unrelated completed nodes preserved;
- revision epochs incremented;
- execution returns to running.

Do not use rewind as a substitute for plan migration when node semantics changed.

---

## 14. Plan changed while execution is live

Normal execution refuses a different digest.

For long-lived workflows:

1. load source/target immutable plans;
2. wait for quiescent execution (no active tasks);
3. call `ValidatePlanMigration`;
4. review `MigrationReport.Problems`;
5. explicitly choose node mapping/reset roots;
6. call `MigrateExecution`.

By default completed activity semantics cannot silently change.

Do not edit `PlanDigest` in storage.

---

## 15. History became too large

Use continue-as-new for a logically endless workflow:

```go
fresh, err := engine.ContinueAsNew(ctx, oldID, newID, adgo.ContinueOptions{
    CarryData: []string{"baseline", "configuration"},
    Reason:    "monthly rollover",
})
```

The old execution remains terminal/auditable. The new execution starts with fresh control history.

Version pruning is independent and optional.

---

## 16. Time-travel debugging

For a `VersionedStore`:

```go
snapshot, err := adgo.InspectVersion(ctx, store, id, version)
```

To branch an alternative trajectory:

```go
fork, info, err := adgo.ForkExecution(...)
```

Do not mutate historical snapshots.

Do not reconstruct history by rerunning probabilistic activities.

---

## 17. Scheduled workflow duplicated externally

ADGO schedule firing itself uses deterministic execution IDs + `StartOrLoad`.

If the same external side effect appears twice, inspect the activity side-effect idempotency contract rather than the schedule ID first.

The scheduler can retry creation safely; external effects still remain at-least-once.

---

## 18. Cache contention

`WithResultCache` may produce a transient durable rate-limit when another execution already owns the single-flight cache lease.

Expected behavior:

- first execution computes;
- second execution waits/retries;
- completed cache result is reused.

If a computing process crashes, lease expiration permits recomputation.

Do not manually delete active cache state unless you have confirmed the owner is gone and normal TTL recovery is unacceptable.

---

## 19. Speculative/ensemble cost spike

Speculation is opt-in and requires `Pure=true`.

Metrics/budget include all launched variants. If cost is too high:

- reduce `MaxParallel`;
- increase `HedgeDelay`;
- raise quality threshold only when it reduces downstream repair enough to justify cost;
- prefer shared result caching for repeat inputs.

Never speculate irreversible effects.

---

## 20. Coordinator crash during compensation

Run the recommended coordinator:

```go
engine.RunResilientCoordinator(ctx)
```

or:

```go
production.Engine.RunResilientCoordinator(ctx)
```

If the prior process died after committing `StatusCompensating`, the next coordinator calls `RecoverCompensation` and continues the remaining LIFO stack.

Compensation handlers must still be idempotent.

---

## 21. Storage choice

### Memory

Use for tests only.

### File

Use when several processes can share a reliable local/network filesystem and filesystem locking/atomic rename semantics are acceptable.

### Pebble

Use for high-throughput local durable service. One process owns a Pebble database path; scale worker goroutines within that process.

### Custom transactional store

Use for true multi-host/cloud deployments when a shared filesystem is not appropriate. Implement `Store` + `ExecutionCatalog`; add `VersionedStore` and other optional capabilities as needed.

---

## 22. Retention

Nothing is deleted automatically.

Recommended order:

1. query/select terminal executions;
2. archive when required;
3. delete terminal execution;
4. prune old immutable versions separately if desired.

```go
result, err := adgo.CollectExecutions(ctx, store, adgo.RetentionPolicy{
    TerminalFor: 30 * 24 * time.Hour,
    Archive:     adgo.JSONArchive(upload),
})
```

Archive failure aborts deletion.

---

## 23. Security incident / credential rotation

Credentials should not be in durable execution data.

Rotate them in your worker secret manager/provider configuration. New activity attempts use the new credential without changing PlanDigest when the credential itself is not part of workflow semantics.

If raw credentials were accidentally persisted:

- treat execution snapshots/history/backups as exposed;
- rotate the credential;
- follow application retention/security procedure;
- do not rely on version pruning as the only incident response because backups/archives may still contain it.

---

## 24. Deployment health checklist

Before declaring an ADGO service healthy:

- `go test ./...` green;
- race tests green;
- `go vet ./...` green;
- production example builds/runs;
- all loaded plans compile;
- fleet audit has no `error` diagnostics;
- no unexpected long-lived expired leases;
- provider circuits reflect real upstream health;
- admission limits match provider quotas;
- worker drain works in deployment automation;
- human/callback endpoints can resolve durable waits;
- retention/archival policy is explicit;
- external effects use stable idempotency keys.
