# Durable wall-clock timers

Axiom can resolve and fire wall-clock timer rules for an existing execution without pretending to own a distributed scheduler.

The runtime is responsible for:

- resolving a timer expression against durable execution state;
- deciding whether it is due;
- recording a stable `TimerFired` key;
- running the timer rule;
- making firing + rule effects transactional when the store supports transactions;
- rebuilding the same schedule after process restart.

The application is responsible for deciding **which execution IDs this process owns**.

## Supported executable timer forms

### Absolute context deadline

```axiom
context Order:
  expiresAt: Time

rule expire:
  on timer(Order.expiresAt)
  write:
    Order.status = "expired"
```

The field may contain an RFC3339/RFC3339Nano string. A nil value means the timer is not currently scheduled.

`timer(at Order.expiresAt)` is equivalent.

### Duration after a context time

```axiom
rule remind:
  on timer(15m after Order.createdAt)
  write:
    Order.reminderDue = true
```

The duration uses Go duration syntax accepted by `time.ParseDuration`, such as `250ms`, `30s`, `15m`, or `24h`.

### Duration after runtime metadata

```axiom
rule expireSession:
  on timer(30m after runtime.createdAt)
  write:
    Session.expired = true
```

`runtime.createdAt` and `runtime.updatedAt` are supported timer bases.

## Inspect the next timer

```go
run := engine.Execution("order-42")

next, err := run.NextTimer(ctx)
if err != nil {
    return err
}
if next != nil {
    log.Printf("next timer %s due at %s", next.Rule, next.DueAt)
}
```

`TimerSchedule` contains:

- `Rule`;
- original `Expression`;
- resolved `DueAt`;
- deterministic `Key`.

The earliest timer that has not already fired is returned.

## Run due timers directly

```go
fired, err := run.RunDueTimers(ctx, time.Now())
```

Passing `time.Time{}` asks the Engine to use its configured clock.

All timers due at or before the supplied timestamp are evaluated in deterministic order.

## Durable one-shot identity

A timer is identified by:

```text
rule name + timer expression + resolved due time
```

The runtime hashes that tuple and stores it in a `TimerFired` history entry.

After restart, `NextTimer` / `RunDueTimers` derive schedules again from execution state and ignore keys already present in history. No separate in-memory timer registry is required.

## Rescheduling

Changing the referenced deadline creates a different resolved due time and therefore a different timer key.

Example:

```text
Order.expiresAt = 12:00 -> timer fires
Order.expiresAt = 13:00 -> new key -> timer may fire again at 13:00
```

This makes rescheduling explicit through durable context changes rather than hidden scheduler state.

## Timer worker

Axiom intentionally does not enumerate or claim ownership of executions on your behalf.

Provide the execution IDs currently owned by this process:

```go
errorsCh := engine.StartTimerWorker(
    ctx,
    func(ctx context.Context) ([]string, error) {
        return ownedExecutionIDs(ctx), nil
    },
    axiom.TimerWorkerOptions{
        PollInterval: time.Second,
    },
)

for err := range errorsCh {
    log.Printf("timer worker: %v", err)
}
```

The worker:

1. asks the callback for owned execution IDs;
2. sorts and deduplicates them;
3. calls `RunDueTimers` for each ID;
4. repeats until `ctx` is canceled.

This is a local polling worker, not a cluster leader election or distributed ownership service.

## Transaction semantics

With `TransactionalStore`, `TimerFired` and timer rule processing run through the same transaction path.

If a timer rule fails—for example a claim rejects a write—`RunDueTimers` returns `AX514`. On a transactional backend such as Pebble, the firing marker and the rule changes roll back together, so the timer remains due and can be retried after the underlying problem is fixed.

For production timer processing, use a transactional store and an explicit execution-ownership strategy.

## Non-transactional stores

The in-memory store is useful for tests and single-process development. It does not provide rollback across a sequence of runtime writes/history operations.

Production code that depends on atomic timer firing should use `WithProductionMode()` with Pebble or another correctly implemented `TransactionalStore`.

## Timer history

A successful due decision records `TimerFired` with:

- `rule`;
- `expression`;
- `dueAt`;
- deterministic `key`;
- `firedAt`.

History is the durable one-shot registry used to suppress refiring the exact same resolved timer.

## Invalid timer expressions

Executable timer resolution is intentionally strict. Unsupported expressions return `AX512` rather than being silently ignored.

Supported production forms are currently:

```text
timer(Context.deadline)
timer(at Context.deadline)
timer(15m after Context.createdAt)
timer(15m after runtime.createdAt)
timer(15m after runtime.updatedAt)
```

A referenced value must be nil/not-yet-scheduled, a `time.Time`, or an RFC3339/RFC3339Nano string.

## Distributed ownership boundary

Axiom does **not** currently guarantee that two separate processes cannot both believe they own the same execution.

If multiple processes run timer workers, the application must provide a single-owner routing/lease strategy for execution IDs. A shared transactional store protects local runtime transactions, but it is not by itself a complete distributed scheduler protocol.

This same ownership boundary applies to ordinary execution routing and is deliberately not hidden by the timer API.
