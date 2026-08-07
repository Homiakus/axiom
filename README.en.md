# Axiom

[Русский](README.md) · **English**

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom is a Go library for state transitions, business processes, and decision tables.

It exposes several ways to define a process over one canonical executable representation, `axiom.Plan`:

- the declarative Go model (`model`) — **recommended for new Go applications**;
- typed Go reducers (`axiom.Flow`) — for compact reducers where static model analysis is not required;
- the AXM file DSL (`axm`) — when process definitions should live outside Go code;
- TOML decision tables (`table`) — for decision-table-shaped problems.

The compiled runtime stores state, history, and activity tasks, checks claims, supports replay, and can use transactional Pebble storage. See [`docs/api-guide.md`](docs/api-guide.md) for the frontend decision guide.

## When to use it

Axiom fits aggregates with their own lifecycle, such as orders, approval requests, machines, production batches, and payment operations, where transitions must be validated, persisted, and explainable.

Axiom is not a replacement for plain CRUD, a message broker, a distributed scheduler, or a cross-process lock manager. Operations for one execution ID are serialized only inside one `Engine` instance.

## Requirements

- Go 1.26 or newer.
- The default in-memory setup needs no external service or environment variable.
- Durable storage is available through CockroachDB Pebble.

## Install

```bash
go get github.com/Homiakus/axiom
```

Before the first stable `v1`, the public API follows the pre-v1 compatibility policy in [`docs/versioning.md`](docs/versioning.md).

To work on the repository:

```bash
git clone https://github.com/Homiakus/axiom.git
cd axiom
go test ./...
```

## Quick start: declarative Go model

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

    definition.Claim("nonNegative", current.Int("Value").GreaterOrEqual(0))

    engine, err := axiom.Open(definition)
    if err != nil {
        log.Fatal(err)
    }

    run := engine.Execution("counter-1")
    if err := run.Dispatch(context.Background(), SetValue{Value: 7}); err != nil {
        log.Fatal(err)
    }

    var state Counter
    if err := run.State(context.Background(), &state); err != nil {
        log.Fatal(err)
    }

    fmt.Println(state.Value) // 7
}
```

For typed field/literal expressions, prefer helpers such as `Equal`, `GreaterOrEqual`, and `LessThan`. Their literal argument is constrained to the field's `T`, so the Go compiler catches more mistakes before Axiom compiles the model.

## Durable storage

```go
store, err := axiom.OpenPebble("data/axiom")
if err != nil {
    return err
}
defer store.Close()

engine, err := axiom.Open(
    definition,
    axiom.WithStore(store),
)
```

`axiom.WithProductionMode()` additionally requires a `TransactionalStore` and enables the strict fast runtime. `retry` and `timeout` are enforced around activity handlers. `concurrency: once` serializes calls of one activity within one Engine, while `parallel` adds no serialization. `concurrency: latest/first` are still rejected with `AX508` because they require correct durable task-supersession semantics.

## Activities

Prefer typed handlers in application code:

```go
engine, err := axiom.Open(
    definition,
    axiom.ActTyped("SendEmail", func(
        ctx context.Context,
        input SendEmailInput,
    ) (SendEmailOutput, error) {
        return SendEmailOutput{Sent: true}, nil
    }),
)
```

`ActTyped` accepts structs, pointers to structs, or maps with string keys for input and output. Unsupported shapes and nil typed handlers fail during Engine construction with `AX507` instead of becoming late runtime failures. Use `axiom.Act` for dynamic `map[string]any` integration boundaries.

For `effect: external`, the compiler requires an idempotency policy and `idempotencyKey`. This deduplicates tasks in the configured store, but it is not an exactly-once guarantee for an external API, device, or payment system. A retry can invoke a handler more than once, so the external operation must remain idempotent.

## Frontends

| Definition style | Package | When to choose | Static analysis | Source |
|---|---|---|---:|---|
| Declarative Go model | `github.com/Homiakus/axiom/model` | **Default for new Go code** | Yes (`static`) | Go |
| Typed Go Flow | `github.com/Homiakus/axiom` | Small reducer / arbitrary Go logic | No (`opaque`) | Go |
| AXM | `github.com/Homiakus/axiom/axm` | External model / tooling | Yes (`static`) | `.axm` |
| TOML | `github.com/Homiakus/axiom/table` | Decision tables | Yes (`static`) | `.toml` |

All declarative frontends compile into `axiom.Plan`. Typed Go Flow uses a separate reducer runtime and remains analysis-opaque.

## Runtime API

Prefer a `Run` handle for one execution:

```go
run := engine.Execution("order-42")

err := run.Dispatch(ctx, Event{...})
err = run.State(ctx, &state)
status, err := run.Status(ctx)
history, err := run.History(ctx)
explanation, err := run.Explain(ctx)
```

`Run` also exposes `Signal`, `Patch`, `PendingActivities`, and `Cancel`.

## Verification commands

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
go run ./examples/coffee-machine
```

CI also builds an external consumer module so the public packages are tested from a downstream user's perspective.

## Code generation

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

The command is non-interactive and prints a JSON report. See [`docs/axiomgen.md`](docs/axiomgen.md).

## Current limitations

1. `retry` is currently an immediate in-process handler retry. It is not yet a durable task-level retry with backoff, `NextAttemptAt`, and a separate history record for every attempt.
2. `concurrency: once` is local to one Engine. `latest/first` do not yet have safe task-supersession semantics and are rejected in production mode.
3. Execution locking is process-local to one `Engine`.
4. Typed Go Flow executes effects before `FlowStore.Save`; effect handlers must be idempotent and custom stores must account for a save failure after an external effect.
5. The in-memory store is for development and tests and does not survive process restarts.

See [`ARCHITECTURE.md`](ARCHITECTURE.md) and [`docs/runtime-semantics.md`](docs/runtime-semantics.md).

## Documentation

- [Documentation index](docs/README.md)
- [Public API guide](docs/api-guide.md)
- [Versioning and compatibility](docs/versioning.md)
- [Examples](examples/README.md)
- [Architecture](ARCHITECTURE.md)
- [Development](DEVELOPMENT.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [AXM specification](docs/axiom-file-specification.md)
- [axiomgen](docs/axiomgen.md)
- [Runtime semantics](docs/runtime-semantics.md)

## License

Apache-2.0. See [`LICENSE`](LICENSE).
