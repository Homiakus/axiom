# Durable Flow effects

`axiom.Flow` supports two explicit effect-delivery modes. They have different failure semantics and must not be treated as interchangeable.

## 1. Default Flow mode

Without `WithDurableFlowEffects()`, `Dispatch` executes each effect handler before the new reducer state and history are saved.

```text
reduce event
    ↓
validate claims
    ↓
run external effects
    ↓
save state + history
```

This mode is intentionally small and synchronous. If an external effect succeeds and the process or store fails before `Save`, redelivery of the business event can repeat the external effect.

Use it when effects are naturally idempotent, are local/in-process, or the application accepts this boundary.

## 2. Durable outbox mode

Enable durable effects explicitly:

```go
store, err := axiom.OpenPebbleFlowStore("data/flow")
if err != nil {
    return err
}
defer store.Close()

engine, err := axiom.OpenFlow(
    flow,
    axiom.WithFlowStore(store),
    axiom.WithDurableFlowEffects(),
)
```

The configured store must implement `DurableFlowStore` and report `StoreDurabilitySynchronous`.

A durable dispatch uses this order:

```text
drain previously pending effects
    ↓
reduce event
    ↓
validate claims
    ↓
ATOMIC + SYNCHRONOUS COMMIT
  state
  EventHandled
  EffectPending[]
    ↓
deliver external effects
    ↓
append EffectCompleted after each successful delivery
```

The state transition and all effect intents therefore become durable before an external handler is allowed to run.

## 3. Delivery guarantee

Durable Flow effects are **at-least-once**, not exactly-once.

The remaining unavoidable boundary is:

```text
external system accepted effect
    ↓
process fails before EffectCompleted commit
```

After recovery Axiom cannot prove whether the external system accepted the previous attempt, so it may deliver the same intent again.

Every durable effect has a stable deterministic ID. Read it inside the effect handler:

```go
axiom.EffectHandler(flow, func(ctx context.Context, cmd SendEmail) error {
    effectID, ok := axiom.FlowEffectIDFromContext(ctx)
    if !ok {
        return errors.New("missing durable effect id")
    }

    return mailer.Send(ctx, cmd, effectID) // use as downstream idempotency key
})
```

For exactly-once **business semantics**, the downstream boundary must deduplicate this ID or provide an equivalent idempotency mechanism.

## 4. Stable effect identity

The built-in effect ID is deterministic from:

- Flow name;
- execution ID;
- handled-event history sequence;
- effect index within that reducer result.

The same pending intent recovered after a restart therefore receives the same ID.

Changing the business event by dispatching it again is not equivalent to retrying the pending effect. A failed durable delivery returns an error whose state is already committed; recover it with `DrainEffects`, not by re-dispatching the original event.

## 5. Recovery API

```go
err := engine.Execution("order-42").DrainEffects(ctx)
```

`DrainEffects`:

1. loads committed state and history;
2. validates the store-reported history length;
3. finds `EffectPending` entries without matching `EffectCompleted` entries;
4. rehydrates the typed command;
5. invokes the registered effect handler with the original stable effect ID;
6. durably appends `EffectCompleted` after success.

Before reducing a new business event, durable `Dispatch` drains older pending effects first. This preserves effect-delivery order across process restarts.

## 6. Failure matrix

| Failure point | State committed? | Effect may have happened? | Recovery |
|---|---:|---:|---|
| reducer returns error | No | No | fix/retry business event |
| claim fails | No | No | fix business event/state rule |
| atomic outbox commit fails | No | No | retry business event |
| effect handler returns error | Yes | handler-defined | `DrainEffects` |
| process dies during handler | Yes | Unknown | `DrainEffects` with downstream dedupe |
| `EffectCompleted` commit fails | Yes | Yes | `DrainEffects`; same effect ID may be delivered again |
| restart after completed acknowledgement | Yes | Yes | no redelivery |

`FlowEffectDeliveryError` and `FlowEffectAcknowledgeError` both expose `StateCommitted() == true` so callers can distinguish these failures from reducer/commit failures.

## 7. `DurableFlowStore` contract

A custom durable Flow store must implement:

```go
type DurableFlowStore interface {
    FlowStore
    IncrementalFlowStore
    DurabilityProvider
    AtomicFlowCommit()
}
```

The important semantic requirement is stronger than method compatibility:

`SaveStateAndAppend` must atomically persist the state bytes and every supplied history entry as one synchronously durable unit.

A store must not advertise `AtomicFlowCommit()` if it performs separate state/history writes that can become partially visible after a crash.

`OpenFlow(..., WithDurableFlowEffects())` rejects stores that do not implement this capability or do not report `StoreDurabilitySynchronous`.

## 8. Built-in `PebbleFlowStore`

`OpenPebbleFlowStore` is the built-in local durable implementation.

Properties:

- one Pebble batch per state/history append;
- `pebble.Sync` on acknowledged commits;
- injective encoded Flow/execution keys;
- append-only ordered history;
- contiguous sequence validation;
- metadata/history length consistency checks;
- full-range cleanup when compatibility `Save` replaces history;
- process restart/reopen recovery tests;
- context cancellation checked before store operations.

A single `PebbleFlowStore` instance currently serializes its operations with a mutex. It is a local durable backend, not a distributed transaction service.

## 9. History model

Durable effects add two history record types:

```text
EventHandled
EffectPending
EffectCompleted
```

`EffectPending` contains `FlowEffectIntent`:

```go
type FlowEffectIntent struct {
    ID      string
    Name    string
    Payload json.RawMessage
}
```

`EffectCompleted` contains the corresponding `FlowEffectCompletion.ID`.

History is the recovery source of truth for the outbox. Do not mutate or compact pending/completion entries independently of state unless the replacement store implementation preserves the same recovery invariant.

## 10. Choosing between Flow and ADGO

Durable Flow is still a typed reducer, not a graph orchestrator.

Use Flow when the core model is:

```text
event + state -> new state + a small ordered effect list
```

Use `adgo` when you need long-running dependency graphs, workers, timers, human approvals, callbacks, repair, fan-out/fan-in, plan migration, or orchestration across many durable steps.

Do not build a second ADGO inside Flow. The durable outbox exists only to close the reducer/external-effect crash boundary.

## 11. Required tests for custom stores

At minimum, verify:

- state and pending intents are atomically visible;
- encoding/commit failure leaves prior state untouched;
- history sequences remain contiguous;
- close/reopen preserves state and pending intents;
- a failed delivery recovers with the same effect ID;
- an acknowledgement failure can safely redeliver the same ID;
- completed intents are not delivered again;
- stale trailing history cannot survive a replacement `Save`;
- concurrent access does not create duplicate or reordered history.
