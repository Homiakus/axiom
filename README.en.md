# Axiom

[Русский](README.md) · **English**

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**Axiom is a Go library for verifiable state transitions, business processes, and decision tables.**

Use it when changing a struct in memory is not enough: a transition needs explicit rules, invariant checks, durable history, safe external effects, and an explanation of how the current state was reached.

For a new Go application, start with the [`model`](model/) package.

## The mental model in 30 seconds

All declarative frontends converge on the same lifecycle:

```text
Go model / AXM / TOML
         ↓
      axiom.Plan
         ↓
       Engine
         ↓
Run = engine.Execution(id)
```

- **Definition** describes state, events, rules, claims, activities, and policies.
- **Plan** is the canonical compiled representation.
- **Engine** combines a Plan, store, activity implementations, and runtime options.
- **Run** is the preferred handle for one durable execution: `Dispatch`, `State`, `Status`, `History`, `Explain`, and `Cancel`.

`axiom.Flow` is a separate compact typed reducer API for cases that do not need a statically analyzable model graph.

## When Axiom fits

Good candidates include orders, approval requests, machines, production batches, payment operations, and other aggregates with a lifecycle of their own.

Axiom is especially useful when you need several of these at once:

- explicit allowed transitions;
- invariants (`claim`) that must never be violated;
- reproducible history/replay behavior;
- external activities with retry, timeout, and idempotency;
- runtime explanations of state and decisions;
- one runtime for Go model, AXM, and TOML definitions.

Axiom is **not** a message broker, distributed scheduler, distributed lock manager, or a replacement for plain CRUD.

## Choose a frontend

| Frontend | Package | Choose it when | Static analysis |
|---|---|---|---:|
| Declarative Go | `github.com/Homiakus/axiom/model` | **default for new Go code** | Yes |
| Typed Go Flow | `github.com/Homiakus/axiom` | small reducer; arbitrary Go matters more | No (`opaque`) |
| AXM | `github.com/Homiakus/axiom/axm` | the definition should live outside Go | Yes |
| TOML table | `github.com/Homiakus/axiom/table` | the problem naturally looks like a decision table | Yes |

See [`docs/api-guide.md`](docs/api-guide.md) for the detailed decision guide.

## Requirements and install

- Go 1.26+.
- The default in-memory mode needs no external service.
- Durable storage is available through the built-in CockroachDB Pebble integration.

```bash
go get github.com/Homiakus/axiom
```

Before the first stable `v1`, the public API follows the pre-v1 compatibility policy in [`docs/versioning.md`](docs/versioning.md).

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Homiakus/axiom"
    "github.com/Homiakus/axiom/model"
)

type Counter struct {
    Value int `json:"value"`
}

type SetValue struct {
    Value int `json:"value"`
}

func main() {
    definition := model.New("Counter")
    current := model.Bind[Counter](definition, "Current")
    setValue := model.EventOf[SetValue](definition)

    definition.Rule("set").
        On(setValue.Trigger()).
        Set(current.Int("Value"), setValue.Int("Value"))

    definition.Claim(
        "nonNegative",
        current.Int("Value").GreaterOrEqual(0),
    )

    engine, err := axiom.Open(definition)
    if err != nil {
        log.Fatal(err)
    }

    run := engine.Execution("counter-1")
    ctx := context.Background()

    if err := run.Dispatch(ctx, SetValue{Value: 7}); err != nil {
        log.Fatal(err)
    }

    var state Counter
    if err := run.State(ctx, &state); err != nil {
        log.Fatal(err)
    }

    fmt.Println(state.Value) // 7
}
```

`axiom.Open(definition)` compiles the `model.Definition` into a `Plan` and builds an `Engine`. `Dispatch` creates the execution on first use, applies the event, and drains available inline activities until idle or a durable retry boundary.

## Larger models: fewer field-name strings

Short helpers such as `order.Int("Total")` are convenient in small models. When the same field appears in many rules, claims, and activities, repeating its name becomes a refactoring and typo surface.

Use reusable typed field keys to centralize it:

```go
type Order struct {
    Status string `json:"status"`
    Total  int    `json:"total"`
}

var (
    orderStatus = model.Key[Order, string]("Status")
    orderTotal  = model.Key[Order, int]("Total")
)

definition := model.New("Orders")
order := model.Bind[Order](definition, "Order")

status := model.StateField(order, orderStatus)
total := model.StateField(order, orderTotal)

model.StateDefault(order, orderStatus, "new")
definition.Claim("totalNonNegative", total.GreaterOrEqual(0))
```

`FieldKey[Owner, Value]` keeps a field name in one place. The Owner type prevents applying a key to the wrong state/event type, while Value is validated against the real Go field when the key is used. Optional pointer fields may use their pointed-to logical value type.

Use `model.EventField` for event fields and `model.StateChanged` for `changed(...)` triggers.

This is **not code generation**: names are still resolved through reflection and `axiom`/`json` tags, but repeated strings and typo surface are substantially reduced.

## Typed expressions

`TypedField[T]` retains compatibility operators such as `EQ`, `GT`, and `Add`, but new code should prefer strict helpers:

```go
total.GreaterOrEqual(0)
status.Equal("paid")
left.EqualField(right)
subtotal.PlusField(tax)
```

Literal helpers constrain the value to the same `T`; field-to-field helpers require matching `TypedField[T]`. This lets the Go compiler catch more mistakes before Axiom compiles the model.

## Activities

Prefer `ActTyped` in application code:

```go
type ChargeInput struct {
    OrderID string `json:"orderId"`
    Amount  int    `json:"amount"`
}

type ChargeOutput struct {
    PaymentID string `json:"paymentId"`
}

engine, err := axiom.Open(
    definition,
    axiom.ActTyped("Charge", func(
        ctx context.Context,
        input ChargeInput,
    ) (ChargeOutput, error) {
        return ChargeOutput{PaymentID: "pay-1"}, nil
    }),
)
```

`ActTyped` input/output shapes must be structs, pointers to structs, or maps with string keys. Unsupported shapes and nil handlers fail during Engine construction with `AX507` instead of becoming late activity failures.

Use `axiom.Act` with `axiom.Input` / `axiom.Output` at dynamic integration boundaries where `map[string]any` is already the natural contract.

## Durable storage and production mode

The default is an in-memory store. For Pebble:

```go
store, err := axiom.OpenPebble("data/axiom")
if err != nil {
    return err
}
defer store.Close()

engine, err := axiom.Open(
    definition,
    axiom.WithStore(store),
    axiom.WithProductionMode(),
)
```

`WithProductionMode()` requires a `TransactionalStore` and enables the strict fast runtime.

Current activity guarantees:

- `retry` persists `Attempt`, `MaxAttempts`, and `NextAttemptAt`, and can resume in a new Engine after process restart when the store is durable;
- `timeout` applies independently to each attempt;
- `parallel` adds no serialization;
- `once` serializes an activity inside one Engine;
- `first` keeps the first active task in an `execution + activity` lane;
- `latest` supersedes older **pending** tasks but does not pretend to force-cancel arbitrary running Go code;
- external activities still need to be idempotent: durable retry provides at-least-once execution, not exactly-once external effects.

See [`docs/runtime-semantics.md`](docs/runtime-semantics.md) for the full contract.

## Runtime API

Use a `Run` handle for one execution:

```go
run := engine.Execution("order-42")

if err := run.Dispatch(ctx, Submitted{Total: 1500}); err != nil {
    return err
}

var state Order
if err := run.State(ctx, &state); err != nil {
    return err
}

status, err := run.Status(ctx)
history, err := run.History(ctx)
explanation, err := run.Explain(ctx)
```

`Run` also exposes `Signal`, `Patch`, `PendingActivities`, and `Cancel`. Lower-level Engine methods that require repeatedly passing an execution ID are primarily for integration/tooling layers.

## Examples

[`examples/`](examples/) is a runnable learning path:

| Example | Command | Purpose |
|---|---|---|
| `model` | `go run ./examples/model` | recommended declarative Go API |
| `go-first` | `go run ./examples/go-first` | typed reducer Flow |
| `order` | `go run ./examples/order` | Pebble + production activity semantics |
| `axiom-files` | `go run ./examples/axiom-files` | AXM file frontend |
| `table` | `go run ./examples/table` | TOML decision-table frontend |
| `triz` | `go run ./examples/triz` | normalization + diagnostics + source map |
| `coffee-machine` | `go run ./examples/coffee-machine` | large end-to-end reference |

See [`examples/README.md`](examples/README.md) for the learning path and guidance.

## Code generation

`axiomgen` generates typed activity boundaries from AXM/TOML:

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

See [`docs/axiomgen.md`](docs/axiomgen.md).

## Verify the project

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
go run ./examples/coffee-machine
```

CI additionally runs vulnerability scanning, fuzz smoke tests, an external consumer module, and a performance job. That downstream consumer test matters for a library: public packages are validated from outside the repository, not only by internal tests.

The benchmark runner and current baseline live in [`benchmarks/latest.md`](benchmarks/latest.md).

## Guarantee boundaries

1. Locking for one `execution ID` is local to one Engine; it is not a distributed ownership protocol.
2. `once` is also local to one Engine; `first/latest` are atomic only within the guarantees of the selected `TransactionalStore`.
3. `latest` means **latest pending wins**, not force-cancellation of arbitrary running Go code.
4. Durable retry does not make an external side effect exactly once; idempotency remains an integration-boundary responsibility.
5. `Flow` executes effects before `FlowStore.Save`; effect handlers must be idempotent.
6. The memory store is intended for development/tests and does not survive a process restart.

## Documentation

- [Documentation index](docs/README.md)
- [Public API guide](docs/api-guide.md)
- [Runtime semantics](docs/runtime-semantics.md)
- [Versioning and compatibility](docs/versioning.md)
- [AXM specification](docs/axiom-file-specification.md)
- [axiomgen](docs/axiomgen.md)
- [Architecture](ARCHITECTURE.md)
- [Development](DEVELOPMENT.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Examples](examples/README.md)

## License

Apache-2.0. See [`LICENSE`](LICENSE).
