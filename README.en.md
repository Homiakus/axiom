# Axiom

[Русский](README.md) · **English**

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom is a Go library for state transitions, workflows and decision tables.

It is intended for cases where changing an in-memory struct is not enough. One model can define:

- typed events;
- state transition rules;
- invariants checked before commit;
- external operations with retry and idempotency policies;
- transactional persistence;
- history, explanations and replay.

Definition files are optional. A process can be expressed as typed Go handlers, a declarative Go model, AXM or TOML. Declarative sources compile into the same `axiom.Plan` and run on the same engine.

## When to use it

Axiom fits state owned by a specific aggregate such as an order, approval request, device or production batch, especially when every transition must be validated and recorded.

Typical uses include:

- order and payment processing;
- approval workflows;
- device state control;
- background operations with retries;
- decision tables;
- state reconstruction from history;
- explaining why a rule fired.

## When not to use it

Axiom is unnecessary for plain CRUD without transitions or invariants. It is not a message broker, distributed scheduler or cross-process lock manager. Serialization for one `execution ID` is process-local to one `Engine`; distributed ownership must be provided separately.

## Install

Go 1.26 or newer is required.

```bash
go get github.com/Homiakus/axiom
```

## Main example: capture a payment and send a receipt

The example models one order and two events:

1. `OrderCreated` stores the order ID, customer email and total.
2. `PaymentCaptured` stores the payment ID and marks the order as paid.
3. A successful payment schedules the external `SendReceipt` operation.
4. Claims reject a paid order without a payment ID and a sent receipt without payment.
5. State and history are stored in Pebble and can be reconstructed by replay.

The core model is shown below. The complete runnable program is in [`examples/order/main.go`](examples/order/main.go).

```go
definition := model.New("Order").Version("1")

order := model.State[Order](definition, "Order").
    Default("Paid", false).
    Default("ReceiptSent", false)

created := model.Event[OrderCreated](definition, "OrderCreated")
captured := model.Event[PaymentCaptured](definition, "PaymentCaptured")

definition.Policy("receiptPolicy").
    Retry(3).
    Timeout(3 * time.Second).
    Concurrency("once").
    Idempotency("required")

definition.Activity("SendReceipt").
    Input("orderId", order.Field("ID")).
    Input("email", order.Field("CustomerEmail")).
    Input("paymentId", order.Field("PaymentID")).
    Output("sent", "Bool").
    Effect("external").
    IdempotencyKey(order.Field("PaymentID")).
    Policy("receiptPolicy")

definition.Rule("createOrder").
    On(created.Trigger()).
    Set(order.Field("ID"), created.Field("OrderID")).
    Set(order.Field("CustomerEmail"), created.Field("CustomerEmail")).
    Set(order.Field("Total"), created.Field("Total"))

definition.Rule("capturePayment").
    On(captured.Trigger()).
    Set(order.Field("PaymentID"), captured.Field("PaymentID")).
    Set(order.Field("Paid"), model.Lit(true))

definition.Rule("sendReceipt").
    On(order.Changed("Paid")).
    When(model.Eq(order.Field("Paid"), model.Lit(true))).
    Run("SendReceipt").
    Set(order.Field("ReceiptSent"), model.Ref("output.sent"))

definition.Claim(
    "paidOrderHasPaymentID",
    model.Implies(
        model.Eq(order.Field("Paid"), model.Lit(true)),
        model.Exists(order.Field("PaymentID")),
    ),
)

definition.Claim(
    "receiptRequiresPayment",
    model.Implies(
        model.Eq(order.Field("ReceiptSent"), model.Lit(true)),
        model.Eq(order.Field("Paid"), model.Lit(true)),
    ),
)
```

Compile, attach durable storage and dispatch typed events:

```go
plan, err := definition.Compile()
if err != nil {
    return err
}

store, err := axiom.OpenPebble("data/orders")
if err != nil {
    return err
}
defer store.Close()

engine, err := plan.New(
    axiom.WithStore(store),
    axiom.Act("SendReceipt", sendReceipt),
)
if err != nil {
    return err
}

run := engine.Execution("order-42")

if err := run.Dispatch(ctx, OrderCreated{
    OrderID:       "order-42",
    CustomerEmail: "customer@example.com",
    Total:         12900,
}); err != nil {
    return err
}

if err := run.Dispatch(ctx, PaymentCaptured{
    PaymentID: "pay-9001",
}); err != nil {
    return err
}

var state Order
if err := run.State(ctx, &state); err != nil {
    return err
}

history, err := run.History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
```

### What this example demonstrates

- Invalid field names, expression types, rules and claims fail during `Compile`, before event processing.
- If a transition violates a claim, the new state and history are not committed.
- External effects are separated from rules and receive explicit retry and idempotency configuration.
- `Dispatch` creates an execution on first use and drains inline work until it is idle.
- Concurrent calls for one `execution ID` are serialized and do not lose updates.
- History records events, state writes and activity outcomes.
- `ReplayFromHistory` reconstructs state with the same compiled plan version.

Run the full example:

```bash
go run ./examples/order
```

## Choose a modeling style

| Style | Use it when | Separate files | Static analysis |
|---|---|---:|---:|
| Typed Go Flow | A small local state machine needs arbitrary Go logic | No | No |
| Declarative Go model | Rules and invariants must be checked before startup | No | Full |
| AXM | A rich versioned model lives outside application code | Yes | Full |
| TOML | The workflow is easiest to maintain as a transition table | Yes | Full |
| Low-level API | Explicit `Start`, `Signal`, `Patch` and `RunUntilIdle` control is required | Optional | Depends on source |

## Minimal Typed Go Flow

A declarative model is not required for simple cases:

```go
type Counter struct {
    Count int
}

type Increment struct {
    By int
}

flow := axiom.NewFlow("counter", Counter{})

axiom.Handle(flow, func(
    _ context.Context,
    state Counter,
    event Increment,
) (axiom.FlowResult[Counter], error) {
    state.Count += event.By
    return axiom.Next(state), nil
})

engine, err := axiom.OpenFlow(flow)
if err != nil {
    return err
}

run := engine.Execution("counter-1")
if err := run.Dispatch(ctx, Increment{By: 2}); err != nil {
    return err
}

state, err := run.State(ctx)
```

A `Flow` contains arbitrary Go code, so Axiom cannot build a complete dependency graph for it. Its analysis level is `axiom.AnalysisOpaque`.

## AXM and TOML

AXM and TOML are only sources for `axiom.Plan`; the engine is format-independent.

```go
axmPlan, err := axm.Load("workflow.axm")
if err != nil {
    return err
}
axmEngine, err := axmPlan.New()

tablePlan, err := table.Load("workflow.toml")
if err != nil {
    return err
}
tableEngine, err := tablePlan.New()
```

## Execution API

```go
run := engine.Execution("order-42")

err := run.Dispatch(ctx, OrderCreated{OrderID: "order-42"})

var state OrderState
err = run.State(ctx, &state)

status, err := run.Status(ctx)
history, err := run.History(ctx)
pending, err := run.PendingActivities(ctx)
explanation, err := run.Explain(ctx)
err = run.Cancel(ctx)
```

The lower-level lifecycle remains available:

```go
err := engine.Start(ctx, "order-42", initialContext)
err = engine.Signal(ctx, "order-42", "OrderCreated", payload)
err = engine.Patch(ctx, "order-42", patch)
result, err := engine.Query(ctx, "order-42", "state")
err = engine.RunUntilIdle(ctx, "order-42")
```

## Pebble write modes

The compiled engine uses memory storage by default. Use Pebble for durable state.

```go
// fsync on every commit
store, err := axiom.OpenPebble("data/axiom")

// Faster, but recent writes may be lost after process or machine failure.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleNoSync(),
)

// Periodic flush instead of fsync on each operation.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleSyncEvery(10*time.Millisecond),
)
```

`WithProductionMode` enables the strict fast engine and requires a transactional store. Unsupported model constructs fail engine creation instead of silently using a slower path.

## Concurrency

These guarantees apply inside one process and one `Engine` instance:

- operations for one `execution ID` are serialized;
- different `execution ID` values may run concurrently;
- state and history commit atomically with a transactional store;
- Pebble transactions do not replace the shared store inside `Engine`;
- integers retain their type after Pebble persistence and reopen.

If multiple processes can modify the same `execution ID`, add ownership routing, a distributed lock or a store with equivalent guarantees.

## History, explanation and replay

```go
history, err := engine.Execution("order-42").History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
if err != nil {
    return err
}

explanation, err := engine.Execution("order-42").Explain(ctx)
```

Replay requires the same plan version that produced the history. A module hash mismatch is an error.

## Performance

The current baseline was measured on a shared GitHub runner: `linux/amd64`, Go 1.26.5, 4 logical CPUs and concurrency 8. The numbers are suitable for coarse regression detection, not as hardware-independent SLAs.

| Scenario | p95 | p99 | Throughput |
|---|---:|---:|---:|
| Go Flow, distinct executions | 3.841 ms | 4.788 ms | 9,028 ops/s |
| Go Flow, one contended execution | 20.777 ms | 24.880 ms | 772 ops/s |
| Compiled engine, distinct executions | 0.505 ms | 3.011 ms | 55,011 ops/s |
| Compiled engine, one contended execution | 1.085 ms | 1.437 ms | 50,938 ops/s |
| Cold memory execution | 0.800 ms | 4.058 ms | 40,239 ops/s |
| Pebble NoSync | 3.904 ms | 5.061 ms | 8,773 ops/s |
| Pebble Sync | 8.688 ms | 10.225 ms | 1,437 ops/s |
| Replay of 1,000 events | 1.977 ms | 2.541 ms | 761 replays/s |

Detailed report: [`benchmarks/latest.md`](benchmarks/latest.md).

Run locally:

```bash
go run ./cmd/axiombench \
  -memory-ops 20000 \
  -pebble-ops 1000 \
  -replay-events 1000 \
  -replay-runs 200 \
  -concurrency 8 \
  -strict=true \
  -json benchmark-results.json \
  -markdown benchmark-results.md
```

## CI coverage

- tests for all packages;
- the race detector for the engine and stores;
- concurrent updates to one execution;
- parallel Pebble transactions;
- rollback after a failed effect;
- integer type preservation;
- replay and final-state validation;
- a 16-worker, 8,000-operation soak test;
- an external consumer module that imports public packages only;
- strict p50, p95 and p99 benchmark runs.

## Packages

| Package | Purpose |
|---|---|
| `github.com/Homiakus/axiom` | `Plan`, engine, execution API and Typed Go Flow |
| `github.com/Homiakus/axiom/model` | Declarative Go model |
| `github.com/Homiakus/axiom/axm` | AXM loading |
| `github.com/Homiakus/axiom/table` | TOML transition tables |
| `github.com/Homiakus/axiom/store/pebble` | Public Pebble store package |
| `github.com/Homiakus/axiom/cmd/axiomgen` | Optional typed-boundary generation |
| `github.com/Homiakus/axiom/cmd/axiombench` | Load testing and percentile reporting |

## Development

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
```

## License

Apache-2.0. See [`LICENSE`](LICENSE).
