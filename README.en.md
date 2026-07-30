# Axiom

[Русский](README.md) · **English**

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom is a Go library for state transitions, business processes, and decision tables.

It exposes several ways to define a process over one canonical executable representation, `axiom.Plan`:

- typed Go reducers (`axiom.Flow`);
- a declarative Go model (`model`);
- the AXM file DSL (`axm`);
- TOML decision tables (`table`).

The compiled runtime stores state, history, and activity tasks, checks claims, supports replay, and can use transactional Pebble storage.

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
    current := model.State[Counter](definition, "Current")
    setValue := model.Event[SetValue](definition, "SetValue")

    definition.Rule("set").
        On(setValue.Trigger()).
        Set(current.Field("Value"), setValue.Field("Value"))

    plan, err := definition.Compile()
    if err != nil {
        log.Fatal(err)
    }

    engine, err := plan.New()
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

## Durable storage

```go
store, err := axiom.OpenPebble("data/axiom")
if err != nil {
    return err
}
defer store.Close()

engine, err := plan.New(axiom.WithStore(store))
```

`axiom.WithProductionMode()` additionally requires a `TransactionalStore` and enables the strict fast runtime.

## Frontends

| Definition style | Package | Static analysis | Source |
|---|---|---:|---|
| Typed Go Flow | `github.com/Homiakus/axiom` | No (`opaque`) | Go |
| Declarative Go model | `github.com/Homiakus/axiom/model` | Yes (`static`) | Go |
| AXM | `github.com/Homiakus/axiom/axm` | Yes (`static`) | `.axm` |
| TOML | `github.com/Homiakus/axiom/table` | Yes (`static`) | `.toml` |

## Verification commands

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
go run ./examples/coffee-machine
```

## Code generation

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

The command is non-interactive and prints a JSON report. See [`docs/axiomgen.md`](docs/axiomgen.md).

## Current limitations

1. `policy.retry`, `policy.timeout`, and `policy.concurrency` are model fields, but they must not yet be treated as fully enforced runtime guarantees. An inline activity error currently marks the task and execution as failed; automatic retry after handler failure and an activity-call timeout are not performed.
2. Execution locking is process-local to one `Engine`.
3. Typed Go Flow executes effects before `FlowStore.Save`; effect handlers must be idempotent and custom stores must account for a save failure after an external effect.
4. The in-memory store is for development and tests and does not survive process restarts.

See [`ARCHITECTURE.md`](ARCHITECTURE.md) and [`docs/runtime-semantics.md`](docs/runtime-semantics.md).

## Documentation

- [Documentation index](docs/README.md)
- [Architecture](ARCHITECTURE.md)
- [Development](DEVELOPMENT.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [AXM specification](docs/axiom-file-specification.md)
- [axiomgen](docs/axiomgen.md)
- [Runtime semantics](docs/runtime-semantics.md)

## License

Apache-2.0. See [`LICENSE`](LICENSE).
