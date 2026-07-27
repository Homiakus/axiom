# Axiom

[Русский](README.md) · **English**

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom is a Go library for state transitions, workflows and decision tables.

Use it when a state change must be validated, connected to typed events and external operations, persisted transactionally, explained from history and replayed after failure or during an audit.

Processes can be defined in Go, AXM or TOML. Declarative sources compile into the same `axiom.Plan` and use the same runtime.

## Where Axiom fits

Axiom is designed for aggregates with their own lifecycle: orders, approval requests, machines, vending devices, production batches and payment operations.

Typical uses include:

- equipment control;
- money and inventory accounting;
- approvals;
- external-operation orchestration;
- retries and idempotency;
- state invariants;
- history and replay.

Axiom is unnecessary for plain CRUD without transitions or invariants. It is not a message broker, distributed scheduler or cross-process lock manager. Operations for one `execution ID` are serialized inside one `Engine`; distributed ownership must be provided separately.

## Install

Go 1.26 or newer is required.

```bash
go get github.com/Homiakus/axiom
```

# Example: coffee vending machine with money accounting

Complete runnable source: [`examples/coffee-machine/main.go`](examples/coffee-machine/main.go).

The machine must:

1. accept money;
2. retain the current customer's credit;
3. check the price and ingredient stock;
4. execute a hardware operation;
5. account for the confirmed price and change;
6. deduct ingredients;
7. verify monetary invariants;
8. persist state and history in Pebble;
9. rebuild state through replay.

## Menu

Money is stored as integer kopecks. `14000` means `140.00 RUB`; `float64` is not used for accounting.

| Drink | Price | Water | Beans | Milk | Cup |
|---|---:|---:|---:|---:|---:|
| Espresso | 90.00 RUB | 40 ml | 8 g | — | 1 |
| Cappuccino | 140.00 RUB | 60 ml | 10 g | 120 ml | 1 |

Prices and recipes belong to the model. A drink-selection event does not carry a price, so a client cannot request cappuccino at the espresso price.

## Machine state

| Field | Meaning |
|---|---|
| `CreditKopecks` | Current customer's money not yet recognized as revenue |
| `AcceptedKopecks` | All accepted money |
| `ReturnedKopecks` | Change and refunds |
| `RevenueKopecks` | Value of dispensed drinks |
| `CashboxKopecks` | Physical money retained by the machine |
| `WaterML`, `BeansG`, `MilkML`, `Cups` | Consumable stock |
| `DrinksServed` | Number of dispensed drinks |
| `LastDrink`, `LastChangeKopecks` | Display and audit data |

```go
type Machine struct {
    CreditKopecks int `json:"creditKopecks"`

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

## Events

| Event | Source | Meaning |
|---|---|---|
| `MoneyInserted` | Coin acceptor or payment terminal | Customer inserted money |
| `EspressoRequested` | Espresso button | Espresso purchase requested |
| `CappuccinoRequested` | Cappuccino button | Cappuccino purchase requested |
| `CancelRequested` | Cancel button | Return all current credit |

```go
type MoneyInserted struct {
    AmountKopecks int `json:"amountKopecks"`
}

func (MoneyInserted) AxiomEventName() string {
    return "MoneyInserted"
}

type CappuccinoRequested struct {
    // This ID binds the event to one physical dispense operation.
    PurchaseID string `json:"purchaseId"`
}

func (CappuccinoRequested) AxiomEventName() string {
    return "CappuccinoRequested"
}
```

## Transition table

| Event | Conditions | State changes | External operation |
|---|---|---|---|
| `MoneyInserted` | Amount is positive | Increase credit, accepted money and cashbox | None |
| `EspressoRequested` | Credit ≥ 90 RUB; sufficient water, beans and cups | Account for 90 RUB revenue, return change, deduct recipe | `DispenseEspresso` |
| `CappuccinoRequested` | Credit ≥ 140 RUB; sufficient water, beans, milk and cups | Account for 140 RUB revenue, return change, deduct recipe | `DispenseCappuccino` |
| `CancelRequested` | Credit is positive | Refund credit and reduce cashbox | `ReturnMoney` |

A failed condition means the rule does not run. A cappuccino cannot be dispensed with only 100 RUB of credit or with an empty milk tank.

## Building the model

```go
definition := model.New("CoffeeMachine").Version("1")

machine := model.State[Machine](definition, "Machine").
    Default("CreditKopecks", 0).
    Default("AcceptedKopecks", 0).
    Default("ReturnedKopecks", 0).
    Default("RevenueKopecks", 0).
    Default("CashboxKopecks", 0).
    Default("WaterML", 2000).
    Default("BeansG", 500).
    Default("MilkML", 1000).
    Default("Cups", 50).
    Default("DrinksServed", 0)

moneyInserted := model.Event[MoneyInserted](definition, "MoneyInserted")
espressoRequested := model.Event[EspressoRequested](definition, "EspressoRequested")
cappuccinoRequested := model.Event[CappuccinoRequested](definition, "CappuccinoRequested")
cancelRequested := model.Event[CancelRequested](definition, "CancelRequested")
```

## Accepting money

All three counters are written by one transition.

```go
definition.Rule("acceptMoney").
    On(moneyInserted.Trigger()).
    When(model.GT(
        moneyInserted.Field("AmountKopecks"),
        model.Lit(0),
    )).
    Set(
        machine.Field("CreditKopecks"),
        add(
            machine.Field("CreditKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    ).
    Set(
        machine.Field("AcceptedKopecks"),
        add(
            machine.Field("AcceptedKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    ).
    Set(
        machine.Field("CashboxKopecks"),
        add(
            machine.Field("CashboxKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    )
```

The example uses small helpers to construct declarative arithmetic expressions:

```go
func add(left, right model.Expr) model.Expr {
    return model.Raw(fmt.Sprintf("(%s + %s)", left, right))
}

func sub(left, right model.Expr) model.Expr {
    return model.Raw(fmt.Sprintf("(%s - %s)", left, right))
}
```

## Hardware-operation policy

Dispensing a drink affects pumps, heater, grinder and change mechanism. Such operations receive an explicit policy.

```go
definition.Policy("hardwarePolicy").
    Retry(2).
    Timeout(10 * time.Second).
    Concurrency("once").
    Idempotency("required")
```

| Setting | Value | Purpose |
|---|---:|---|
| `Retry` | 2 | Up to two retries after the first attempt |
| `Timeout` | 10 s | Maximum duration of one attempt |
| `Concurrency` | `once` | Do not run the same task in parallel |
| `Idempotency` | `required` | Require an operation key |

## Cappuccino activity

Price and change are computed before hardware access. The handler returns confirmed values, and the rule accounts for those outputs.

```go
cappuccinoChange := sub(
    machine.Field("CreditKopecks"),
    model.Lit(cappuccinoPriceKopecks),
)

definition.Activity("DispenseCappuccino").
    Input("purchaseId", cappuccinoRequested.Field("PurchaseID")).
    Input("priceKopecks", model.Lit(cappuccinoPriceKopecks)).
    Input("changeKopecks", cappuccinoChange).
    Output("dispensed", "Bool").
    Output("priceKopecks", "Int").
    Output("changeKopecks", "Int").
    Effect("external").
    IdempotencyKey(cappuccinoRequested.Field("PurchaseID")).
    Policy("hardwarePolicy")
```

Registering the handler:

```go
axiom.Act("DispenseCappuccino", func(
    ctx context.Context,
    input axiom.Input,
) (axiom.Output, error) {
    // Real code sends commands to the hardware controller.
    // It must remember PurchaseID and never dispense a second drink
    // for an operation that has already completed.
    return axiom.Output{
        "dispensed":     true,
        "priceKopecks":  input["priceKopecks"],
        "changeKopecks": input["changeKopecks"],
    }, nil
})
```

If the handler fails, the rule must not account for revenue or deduct ingredients.

## Selling cappuccino

```go
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
        add(
            machine.Field("ReturnedKopecks"),
            model.Ref("output.changeKopecks"),
        ),
    ).
    Set(
        machine.Field("RevenueKopecks"),
        add(
            machine.Field("RevenueKopecks"),
            model.Ref("output.priceKopecks"),
        ),
    ).
    Set(
        machine.Field("CashboxKopecks"),
        sub(
            machine.Field("CashboxKopecks"),
            model.Ref("output.changeKopecks"),
        ),
    ).
    Set(machine.Field("WaterML"), sub(machine.Field("WaterML"), model.Lit(60))).
    Set(machine.Field("BeansG"), sub(machine.Field("BeansG"), model.Lit(10))).
    Set(machine.Field("MilkML"), sub(machine.Field("MilkML"), model.Lit(120))).
    Set(machine.Field("Cups"), sub(machine.Field("Cups"), model.Lit(1))).
    Set(
        machine.Field("DrinksServed"),
        add(machine.Field("DrinksServed"), model.Lit(1)),
    ).
    Set(machine.Field("LastDrink"), model.Lit("cappuccino")).
    Set(
        machine.Field("LastChangeKopecks"),
        model.Ref("output.changeKopecks"),
    ).
    Set(
        machine.Field("LastDispensed"),
        model.Ref("output.dispensed"),
    )
```

## Monetary invariants

Two equations are checked after each transition.

### Conservation of money

```text
accepted = returned + revenue + current credit
```

```go
definition.Claim(
    "moneyIsConserved",
    model.Eq(
        machine.Field("AcceptedKopecks"),
        add(
            machine.Field("ReturnedKopecks"),
            add(
                machine.Field("RevenueKopecks"),
                machine.Field("CreditKopecks"),
            ),
        ),
    ),
)
```

### Physical cashbox reconciliation

For a machine with no opening change float:

```text
cashbox = revenue + current credit
```

With an opening float, add `OpeningFloatKopecks` to the right-hand side. A separate claim prevents negative money and stock values.

## Money movement

| Step | Operation | Credit | Accepted | Returned | Revenue | Cashbox |
|---:|---|---:|---:|---:|---:|---:|
| 0 | Initial state | 0 RUB | 0 RUB | 0 RUB | 0 RUB | 0 RUB |
| 1 | Insert 200 RUB | 200 RUB | 200 RUB | 0 RUB | 0 RUB | 200 RUB |
| 2 | Cappuccino 140 RUB, change 60 RUB | 0 RUB | 200 RUB | 60 RUB | 140 RUB | 140 RUB |
| 3 | Insert 100 RUB | 100 RUB | 300 RUB | 60 RUB | 140 RUB | 240 RUB |
| 4 | Espresso 90 RUB, change 10 RUB | 0 RUB | 300 RUB | 70 RUB | 230 RUB | 230 RUB |
| 5 | Insert 50 RUB | 50 RUB | 350 RUB | 70 RUB | 230 RUB | 280 RUB |
| 6 | Cancel and refund 50 RUB | 0 RUB | 350 RUB | 120 RUB | 230 RUB | 230 RUB |

Final checks:

```text
350 RUB = 120 RUB + 230 RUB + 0 RUB
230 RUB = 230 RUB + 0 RUB
```

Resource balances after two drinks:

| Resource | Initial | Used | Remaining |
|---|---:|---:|---:|
| Water | 2,000 ml | 100 ml | 1,900 ml |
| Beans | 500 g | 18 g | 482 g |
| Milk | 1,000 ml | 120 ml | 880 ml |
| Cups | 50 | 2 | 48 |

## Compile, persist and run

```go
plan, err := definition.Compile()
if err != nil {
    return err
}

store, err := axiom.OpenPebble("data/coffee-machine")
if err != nil {
    return err
}
defer store.Close()

engine, err := plan.New(
    axiom.WithStore(store),
    axiom.Act("DispenseEspresso", dispenseEspresso),
    axiom.Act("DispenseCappuccino", dispenseCappuccino),
    axiom.Act("ReturnMoney", returnMoney),
)
if err != nil {
    return err
}

run := engine.Execution("coffee-machine-01")
_ = run.Dispatch(ctx, MoneyInserted{AmountKopecks: 20000})
_ = run.Dispatch(ctx, CappuccinoRequested{PurchaseID: "sale-0001"})
```

Pebble provides transactional persistence but does not implicitly enable strict fast runtime. `WithStrictFastRuntime` and `WithProductionMode` are explicit. This example uses the regular compiled runtime because its accounting claims contain arithmetic expressions.

## History and replay

```go
var state Machine
if err := run.State(ctx, &state); err != nil {
    return err
}

history, err := run.History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(
    plan.Module(),
    history,
)
```

Replay verifies the compiled-plan hash, preventing accidental replay with another model version.

## Run the example

```bash
go run ./examples/coffee-machine
```

Expected result:

```text
accepted: 350.00 RUB
returned: 120.00 RUB
revenue:  230.00 RUB
cashbox:  230.00 RUB
credit:   0.00 RUB
drinks:   2
water:    1900 ml
beans:    482 g
milk:     880 ml
cups:     48
```

The program itself prints Russian labels because the Russian README is the primary documentation. CI executes the example and verifies the monetary totals rather than only compiling it.

## What the example demonstrates

| Capability | Result |
|---|---|
| Typed events | Callers cannot submit arbitrary fields |
| Model compilation | Field, type, policy and activity errors are found before runtime |
| Rule conditions | Insufficient money or stock blocks a sale |
| Activity boundary | Physical work is separated from accounting rules |
| Idempotency | Repeated work is linked to the same `PurchaseID` |
| Claims | A broken monetary balance aborts the transition |
| Pebble | State and history are persisted transactionally |
| `execution ID` | Events for one machine are processed sequentially |
| History | Money, sales, change, refunds and activity results are auditable |
| Replay | State can be rebuilt from the journal |

# API choices

| API | Use when | Files | Static analysis |
|---|---|---:|---:|
| Typed Go Flow | Small state machine with arbitrary Go logic | No | Opaque |
| Declarative Go model | Rules, activities and claims must be validated | No | Full |
| AXM | Versioned model outside the application | Yes | Full |
| TOML | Transition table stored as configuration | Yes | Full |
| Low-level runtime | Explicit `Start`, `Signal`, `Patch`, `RunUntilIdle` control | Optional | Depends on model |

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

`WithProductionMode` requires a transactional store and enables strict fast runtime. Models using expressions outside the strict subset are rejected when the engine is created.

## Concurrency

Within one process and one `Engine`:

- operations for one `execution ID` are serialized;
- different execution IDs can run concurrently;
- state and history are atomic with a transactional store;
- integer types survive Pebble reopen.

Multiple processes require an ownership router, distributed lock or a store with equivalent guarantees.

## Performance baseline

Measured on a shared GitHub-hosted `linux/amd64` runner, Go 1.26.5, 4 logical CPUs and concurrency 8. These figures are for coarse regression detection, not hardware-independent SLAs.

| Scenario | p95 | p99 | Throughput |
|---|---:|---:|---:|
| Go Flow, distinct executions | 3.841 ms | 4.788 ms | 9,028 ops/s |
| Go Flow, one contended execution | 20.777 ms | 24.880 ms | 772 ops/s |
| Compiled runtime, distinct executions | 0.505 ms | 3.011 ms | 55,011 ops/s |
| Compiled runtime, one contended execution | 1.085 ms | 1.437 ms | 50,938 ops/s |
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
go run ./examples/coffee-machine
```

## Packages

| Package | Purpose |
|---|---|
| `github.com/Homiakus/axiom` | `Plan`, runtime, execution API and Typed Go Flow |
| `github.com/Homiakus/axiom/model` | Declarative Go model |
| `github.com/Homiakus/axiom/axm` | AXM frontend |
| `github.com/Homiakus/axiom/table` | TOML frontend |
| `github.com/Homiakus/axiom/store/pebble` | Pebble storage |
| `github.com/Homiakus/axiom/cmd/axiomgen` | Typed-boundary generator |
| `github.com/Homiakus/axiom/cmd/axiombench` | Percentile benchmark harness |

## License

Apache-2.0. See [`LICENSE`](LICENSE).
