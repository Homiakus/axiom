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

`axiom.WithProductionMode()` additionally requires a `TransactionalStore` and enables the strict fast runtime. `retry` is enforced at the durable task level: each handler attempt receives a lease, `Attempt`/`MaxAttempts` and `NextAttemptAt` are persisted, and another Engine can continue the task after a process restart. `backoff` accepts a fixed duration, `fixed(...)`, or `exponential(...)`; when omitted, retry uses deterministic exponential delay starting at 100 ms and capped at 30 s. `timeout` applies independently to each attempt.

Concurrency policies have distinct guarantees: `parallel` adds no serialization, `once` serializes one activity within one Engine, `first` keeps the earliest active task in the `execution + activity` lane and records later tasks as `TaskSuperseded`, and `latest` replaces older **pending** tasks with the newest pending task. `latest` never pretends to forcibly cancel arbitrary running Go code; a new task waits behind the current lease. Production mode supports all four modes with a transactional store, and Pebble performs the pending-supersession decision inside the store transaction.

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

For `effect: external`, the compiler requires an idempotency policy and `idempotencyKey`. This deduplicates tasks in the configured store, but it is not an exactly-once guarantee for an external API, device, or payment system. Durable retry can invoke a handler more than once, so the external operation must remain idempotent. An explicit non-empty idempotency key takes precedence over first/latest supersession: the same external intent is deduplicated before supersession.

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

`Run` also exposes `Signal`, `Patch`, `PendingActivities`, and `Cancel`. `Dispatch`, `Signal`, and `Patch` wait for due durable retries within the caller context. Low-level `Engine.RunUntilIdle` does not sleep until a future retry: after persisting a checkpoint it returns `axiom.ErrRetryScheduled`, allowing an external worker to release the goroutine until `NextAttemptAt`.

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

1. `once` is local to one Engine and is not a distributed lock. `first/latest` atomically manage pending tasks only within the guarantees of the selected `TransactionalStore`.
2. `latest` does not cancel an already running handler: the current guarantee is **latest pending wins**, not unsafe force-cancellation of arbitrary Go code.
3. Execution locking is process-local to one `Engine`.
4. Durable retry persists checkpoints between attempts but does not make an external effect exactly once; activity handlers still need idempotency.
5. Typed Go Flow executes effects before `FlowStore.Save`; effect handlers must be idempotent and custom stores must account for a save failure after an external effect.
6. The in-memory store preserves retry checkpoints across replacement Engine instances, but it does not survive a process restart. Use Pebble or another durable store for process-level recovery.

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
