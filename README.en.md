# Axiom

[Русский](README.md) · **English**

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom is a Go library for state transitions, workflows and decision tables.

Use it when a state change must be validated, persisted and replayable rather than merely applied to an in-memory struct. A model can connect:

- typed events;
- transition rules;
- state invariants;
- external activities with retry and idempotency policies;
- transactional persistence;
- history, explanation and replay.

Definition files are optional. Processes can be described with typed Go handlers, the declarative Go model, AXM or TOML. Declarative sources compile into the same `axiom.Plan` and use the same runtime.

## When Axiom fits

Axiom is useful when state belongs to a specific aggregate such as an order, approval request, device, production batch or payment, and every change must be checked and recorded.

Typical uses include equipment state, money-flow accounting, orders, approvals, retryable background operations, decision tables, replay and audit.

## When it does not fit

Axiom is unnecessary for plain CRUD with no transitions or invariants. It is not a message broker, distributed scheduler or cross-process lock manager. Serialization of one `execution ID` is process-local to one `Engine` instance.

## Install

Go 1.26 or newer is required.

```bash
go get github.com/Homiakus/axiom
```

# Main example: coffee vending machine

The example models one physical coffee machine that accepts money, tracks customer credit, checks stock, dispenses drinks and change, accounts for revenue, persists history in Pebble and rebuilds state through replay.

The complete runnable program is in [`examples/coffee-machine/main.go`](examples/coffee-machine/main.go).

## Menu

Money is stored as integer kopecks. `14000` means `140.00 RUB`; `float64` is not used for accounting.

| Drink | Price | Water | Beans | Milk | Cup |
|---|---:|---:|---:|---:|---:|
| Espresso | 90.00 RUB | 40 ml | 8 g | — | 1 |
| Cappuccino | 140.00 RUB | 60 ml | 10 g | 120 ml | 1 |

Prices and recipes are part of the model, not event payloads. A client cannot request a cappuccino while supplying a lower price or recipe.

## Machine state

| Field | Meaning |
|---|---|
| `CreditKopecks` | Money owned by the current customer session |
| `AcceptedKopecks` | Lifetime accepted money |
| `ReturnedKopecks` | Lifetime change and refunds |
| `RevenueKopecks` | Completed drink sales |
| `CashboxKopecks` | Physical money retained by the machine |
| `WaterML`, `BeansG`, `MilkML`, `Cups` | Consumable stock |
| `DrinksServed` | Number of completed drinks |
| `LastDrink`, `LastChangeKopecks` | Display and audit data |

```go
type Machine struct {
    CreditKopecks   int `json:"creditKopecks"`
    AcceptedKopecks int `json:"acceptedKopecks"`
    ReturnedKopecks int `json:"returnedKopecks"`
    RevenueKopecks  int `json:"revenueKopecks"`
    CashboxKopecks  int `json:"cashboxKopecks"`

    WaterML int `json:"waterML"`
    BeansG  int `json:"beansG"`
    MilkML  int `json:"milkML"`
    Cups    int `json:"cups"`

    DrinksServed      int    `json:"drinksServed"`
    LastDrink         string `json:"lastDrink"`
    LastChangeKopecks int    `json:"lastChangeKopecks"`
    LastDispensed     bool   `json:"lastDispensed"`
}
```

## Transition table

| Event | Guard | State changes | Activity |
|---|---|---|---|
| `MoneyInserted` | `amount > 0` | Increase credit, accepted total and cashbox | None |
| `EspressoRequested` | Credit and stock are sufficient | Add 90 RUB revenue, deduct stock, return change | `DispenseEspresso` |
| `CappuccinoRequested` | Credit and stock are sufficient | Add 140 RUB revenue, deduct stock, return change | `DispenseCappuccino` |
| `CancelRequested` | Credit is positive | Refund all current credit | `ReturnMoney` |

A failed guard means the rule does not run. For example, a cappuccino request at 100 RUB credit does not alter accounting or stock.

## Accepting money

```go
definition.Rule("acceptMoney").
    On(moneyInserted.Trigger()).
    When(model.GT(moneyInserted.Field("AmountKopecks"), model.Lit(0))).
    Set(
        machine.Field("CreditKopecks"),
        add(machine.Field("CreditKopecks"), moneyInserted.Field("AmountKopecks")),
    ).
    Set(
        machine.Field("AcceptedKopecks"),
        add(machine.Field("AcceptedKopecks"), moneyInserted.Field("AmountKopecks")),
    ).
    Set(
        machine.Field("CashboxKopecks"),
        add(machine.Field("CashboxKopecks"), moneyInserted.Field("AmountKopecks")),
    )
```

## Selling a cappuccino

```go
cappuccinoChange := sub(
    machine.Field("CreditKopecks"),
    model.Lit(cappuccinoPriceKopecks),
)

definition.Rule("sellCappuccino").
    On(cappuccinoRequested.Trigger()).
    When(
        model.GTE(machine.Field("CreditKopecks"), model.Lit(14000)),
        model.GTE(machine.Field("WaterML"), model.Lit(60)),
        model.GTE(machine.Field("BeansG"), model.Lit(10)),
        model.GTE(machine.Field("MilkML"), model.Lit(120)),
        model.GTE(machine.Field("Cups"), model.Lit(1)),
    ).
    Run("DispenseCappuccino").
    Set(machine.Field("CreditKopecks"), model.Lit(0)).
    Set(
        machine.Field("ReturnedKopecks"),
        add(machine.Field("ReturnedKopecks"), cappuccinoChange),
    ).
    Set(
        machine.Field("RevenueKopecks"),
        add(machine.Field("RevenueKopecks"), model.Lit(14000)),
    ).
    Set(
        machine.Field("CashboxKopecks"),
        sub(machine.Field("CashboxKopecks"), cappuccinoChange),
    ).
    Set(machine.Field("WaterML"), sub(machine.Field("WaterML"), model.Lit(60))).
    Set(machine.Field("BeansG"), sub(machine.Field("BeansG"), model.Lit(10))).
    Set(machine.Field("MilkML"), sub(machine.Field("MilkML"), model.Lit(120))).
    Set(machine.Field("Cups"), sub(machine.Field("Cups"), model.Lit(1))).
    Set(machine.Field("LastDispensed"), model.Ref("output.dispensed"))
```

## Activity policy and idempotency

```go
definition.Policy("hardwarePolicy").
    Retry(2).
    Timeout(10 * time.Second).
    Concurrency("once").
    Idempotency("required")

definition.Activity("DispenseCappuccino").
    Input("purchaseId", cappuccinoRequested.Field("PurchaseID")).
    Input("priceKopecks", model.Lit(14000)).
    Input("changeKopecks", cappuccinoChange).
    Output("dispensed", "Bool").
    Effect("external").
    IdempotencyKey(cappuccinoRequested.Field("PurchaseID")).
    Policy("hardwarePolicy")
```

If the hardware handler fails, accounting and inventory writes from that transition are not committed.

## Accounting invariants

After each transition Axiom checks:

```text
accepted = returned + revenue + current credit
cashbox = revenue + current credit
```

The second equation assumes no opening change float. A real machine can add `OpeningFloatKopecks` to the right side.

```go
definition.Claim(
    "moneyIsConserved",
    model.Eq(
        machine.Field("AcceptedKopecks"),
        add(
            machine.Field("ReturnedKopecks"),
            add(machine.Field("RevenueKopecks"), machine.Field("CreditKopecks")),
        ),
    ),
)
```

## Example accounting sequence

| Step | Action | Credit | Accepted | Returned | Revenue | Cashbox |
|---:|---|---:|---:|---:|---:|---:|
| 0 | Initial state | 0 | 0 | 0 | 0 | 0 |
| 1 | Insert 200 RUB | 200 | 200 | 0 | 0 | 200 |
| 2 | Cappuccino, 60 RUB change | 0 | 200 | 60 | 140 | 140 |
| 3 | Insert 100 RUB | 100 | 300 | 60 | 140 | 240 |
| 4 | Espresso, 10 RUB change | 0 | 300 | 70 | 230 | 230 |
| 5 | Insert 50 RUB | 50 | 350 | 70 | 230 | 280 |
| 6 | Cancel and refund 50 RUB | 0 | 350 | 120 | 230 | 230 |

Final checks:

```text
350 = 120 + 230 + 0
230 = 230 + 0
```

## Run and replay

```go
plan, err := definition.Compile()
store, err := axiom.OpenPebble("data/coffee-machine")

engine, err := plan.New(
    axiom.WithStore(store),
    axiom.WithProductionMode(),
    axiom.Act("DispenseEspresso", dispenseEspresso),
    axiom.Act("DispenseCappuccino", dispenseCappuccino),
    axiom.Act("ReturnMoney", returnMoney),
)

run := engine.Execution("coffee-machine-01")
_ = run.Dispatch(ctx, MoneyInserted{AmountKopecks: 20000})
_ = run.Dispatch(ctx, CappuccinoRequested{PurchaseID: "sale-0001"})

var state Machine
_ = run.State(ctx, &state)

history, err := run.History(ctx)
replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
```

Run the complete program:

```bash
go run ./examples/coffee-machine
```

## What the example demonstrates

| Axiom property | Result in the machine |
|---|---|
| Model compilation | Field, type and expression errors are found before accepting money |
| Claims | Broken accounting or negative stock aborts a transition |
| Transactional store | Money, stock and history cannot be partially committed |
| Idempotent activity | Repeated work does not intentionally dispense a second drink |
| Per-execution serialization | Concurrent signals for one machine do not lose updates |
| History | Accepted money, sales, change and refunds can be audited |
| Replay | State can be rebuilt after controller replacement or audit |

# API choices

| API | Use when | Files | Static analysis |
|---|---|---:|---:|
| Typed Go Flow | Small local state machine with arbitrary Go logic | No | Opaque |
| Declarative Go model | Rules and invariants must be checked before runtime | No | Full |
| AXM | A versioned model is stored outside the application | Yes | Full |
| TOML | Transition tables are configuration | Yes | Full |
| Low-level runtime | Explicit `Start`, `Signal`, `Patch` and `RunUntilIdle` control is required | Optional | Depends on source |

## AXM and TOML

```go
axmPlan, err := axm.Load("workflow.axm")
axmEngine, err := axmPlan.New()

tablePlan, err := table.Load("workflow.toml")
tableEngine, err := tablePlan.New()
```

## Execution API

```go
run := engine.Execution("coffee-machine-01")
err := run.Dispatch(ctx, MoneyInserted{AmountKopecks: 10000})

var state Machine
err = run.State(ctx, &state)

status, err := run.Status(ctx)
history, err := run.History(ctx)
pending, err := run.PendingActivities(ctx)
explanation, err := run.Explain(ctx)
err = run.Cancel(ctx)
```

## Pebble modes

```go
store, err := axiom.OpenPebble("data/axiom")
store, err = axiom.OpenPebble("data/axiom", axiom.PebbleNoSync())
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleSyncEvery(10*time.Millisecond),
)
```

## Concurrency

Within one process and one `Engine`:

- operations for one `execution ID` are serialized;
- different execution IDs can run concurrently;
- state and history are atomic with a transactional store;
- Pebble transactions do not replace the shared store object;
- integer values remain integers after Pebble reopen.

Multiple processes need an ownership router, distributed lock or a store with equivalent guarantees.

## Performance baseline

Measured on a shared GitHub-hosted `linux/amd64` runner, Go 1.26.5, 4 logical CPUs, concurrency 8. These figures are for coarse regression detection, not hardware-independent SLAs.

| Scenario | p95 | p99 | Throughput |
|---|---:|---:|---:|
| Go Flow, distinct executions | 3.841 ms | 4.788 ms | 9,028 ops/s |
| Go Flow, one contended execution | 20.777 ms | 24.880 ms | 772 ops/s |
| Compiled runtime, distinct executions | 0.505 ms | 3.011 ms | 55,011 ops/s |
| Compiled runtime, one contended execution | 1.085 ms | 1.437 ms | 50,938 ops/s |
| Cold memory execution | 0.800 ms | 4.058 ms | 40,239 ops/s |
| Pebble NoSync | 3.904 ms | 5.061 ms | 8,773 ops/s |
| Pebble Sync | 8.688 ms | 10.225 ms | 1,437 ops/s |
| Replay of 1,000 events | 1.977 ms | 2.541 ms | 761 runs/s |

See [`benchmarks/latest.md`](benchmarks/latest.md).

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
