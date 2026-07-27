# Axiom

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**Axiom is a deterministic state-transition, workflow and decision engine for Go.**

Use ordinary typed Go reducers for the shortest path, the declarative Go model when static validation matters, or AXM/TOML definitions when workflows must live outside application code. Every statically analyzable frontend compiles into the same canonical `axiom.Plan` and runs on the same deterministic runtime.

Axiom is designed for systems where state changes must be explainable, replayable and safe under concurrency: business workflows, control logic, approvals, orchestration, decision tables and durable background activities.

## Why Axiom

- **Go-first:** start without a DSL or generated files.
- **Deterministic:** the same plan, state and event produce the same transition result.
- **Typed boundaries:** dispatch named Go structs instead of hand-built maps.
- **Static validation:** declarative models, AXM and TOML are validated before execution.
- **Durable execution:** use the embedded Pebble store for transactional persistence.
- **Replay and audit:** reconstruct state from history and inspect why rules fired.
- **Concurrency safe:** updates to one execution are serialized; independent executions remain parallel.
- **Activities:** isolate external side effects behind registered Go handlers.
- **Claims:** enforce invariants as part of the transition model.
- **Impact analysis:** compare compiled bundles and identify affected rules and fields.

## Install

Axiom currently requires Go 1.26 or newer.

```bash
go get github.com/Homiakus/axiom
```

## Choose an API

| API | Best for | Files required | Static analysis |
|---|---|---:|---:|
| Typed Go Flow | Application-local reducers, commands and state machines | No | Opaque |
| Declarative Go Model | Validated workflows expressed entirely in Go | No | Full |
| AXM frontend | Rich versioned workflow definitions | Yes | Full |
| TOML table frontend | Transition tables maintained as configuration | Yes | Full |
| Low-level runtime | Existing integrations and explicit lifecycle control | Optional | Depends on source |

## Fastest start: typed Go Flow

A Flow is a typed reducer with optional effects and claims. It is the smallest API surface and requires no schema file.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Homiakus/axiom"
)

type Counter struct {
    Count int `json:"count"`
}

type Increment struct {
    By int `json:"by"`
}

type LogCount struct {
    Count int
}

func main() {
    ctx := context.Background()

    flow := axiom.NewFlow("counter", Counter{})

    axiom.Handle(flow, func(
        _ context.Context,
        state Counter,
        event Increment,
    ) (axiom.FlowResult[Counter], error) {
        state.Count += event.By
        return axiom.Next(
            state,
            axiom.Call(LogCount{Count: state.Count}),
        ), nil
    })

    axiom.EffectHandler(flow, func(_ context.Context, command LogCount) error {
        fmt.Printf("count=%d\n", command.Count)
        return nil
    })

    axiom.AddClaim(flow, func(state Counter) error {
        if state.Count < 0 {
            return fmt.Errorf("count must not be negative")
        }
        return nil
    })

    engine, err := axiom.OpenFlow(flow)
    if err != nil {
        log.Fatal(err)
    }

    run := engine.Execution("counter-1")
    if err := run.Dispatch(ctx, Increment{By: 2}); err != nil {
        log.Fatal(err)
    }

    state, err := run.State(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(state.Count) // 2
}
```

Flow handlers are arbitrary Go code, so their analysis level is `axiom.AnalysisOpaque`. A failed claim, handler or effect does not commit the new state or history.

Run the complete example:

```bash
go run ./examples/go-first
```

## Declarative Go Model

The `model` package keeps the workflow in Go while preserving compiler validation, dependency indexes, activities, claims, replay and impact analysis.

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/Homiakus/axiom"
    "github.com/Homiakus/axiom/model"
)

type User struct {
    ID          *string `json:"id"`
    Email       *string `json:"email"`
    WelcomeSent bool    `json:"welcomeSent"`
}

type UserRegistered struct {
    UserID string `json:"userId"`
    Email  string `json:"email"`
}

func (UserRegistered) AxiomEventName() string { return "UserRegistered" }

func main() {
    definition := model.New("Welcome")

    user := model.State[User](definition, "User").
        Default("WelcomeSent", false)
    registered := model.Event[UserRegistered](definition, "UserRegistered")

    definition.Policy("emailPolicy").
        Retry(2).
        Timeout(5 * time.Second).
        Concurrency("once").
        Idempotency("required")

    definition.Activity("SendWelcomeEmail").
        Input("userId", user.Field("ID")).
        Input("email", user.Field("Email")).
        Output("sent", "Bool").
        Effect("external").
        IdempotencyKey(user.Field("ID")).
        Policy("emailPolicy")

    definition.Rule("captureRegistration").
        On(registered.Trigger()).
        Set(user.Field("ID"), registered.Field("UserID")).
        Set(user.Field("Email"), registered.Field("Email"))

    definition.Rule("sendWelcomeEmail").
        On(user.Changed("Email")).
        When(model.Eq(user.Field("WelcomeSent"), model.Lit(false))).
        Run("SendWelcomeEmail").
        Set(user.Field("WelcomeSent"), model.Ref("output.sent"))

    definition.Claim(
        "welcomeSentRequiresEmail",
        model.Implies(
            model.Eq(user.Field("WelcomeSent"), model.Lit(true)),
            model.Exists(user.Field("Email")),
        ),
    )

    engine, err := axiom.Open(
        definition,
        axiom.Act("SendWelcomeEmail", func(
            context.Context,
            axiom.Input,
        ) (axiom.Output, error) {
            return axiom.Output{"sent": true}, nil
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    err = engine.Execution("user-1").Dispatch(
        context.Background(),
        UserRegistered{
            UserID: "user-1",
            Email:  "user@example.com",
        },
    )
    if err != nil {
        log.Fatal(err)
    }
}
```

Run the complete example:

```bash
go run ./examples/model
```

## Optional AXM and TOML frontends

Definitions outside application code compile into the same `axiom.Plan` used by the declarative Go model.

```go
import (
    "github.com/Homiakus/axiom/axm"
    "github.com/Homiakus/axiom/table"
)

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

_, _ = axmEngine, tableEngine
```

AXM and TOML are frontends, not runtime requirements. Applications can choose one representation per subsystem and still share execution, storage and observability infrastructure.

## Execution API

A compiled engine exposes an ergonomic handle for a single durable execution:

```go
run := engine.Execution("order-42")

// Creates the execution when absent, dispatches the typed event and drains
// registered inline activities until the execution becomes idle.
err := run.Dispatch(ctx, OrderCreated{OrderID: "42"})

var state OrderState
err = run.State(ctx, &state)

status, err := run.Status(ctx)
history, err := run.History(ctx)
pending, err := run.PendingActivities(ctx)
explanation, err := run.Explain(ctx)
err = run.Cancel(ctx)
```

The low-level lifecycle remains available when explicit orchestration is needed:

```go
err := engine.Start(ctx, "order-42", initialContext)
err = engine.Signal(ctx, "order-42", "OrderCreated", payload)
err = engine.Patch(ctx, "order-42", patch)
result, err := engine.Query(ctx, "order-42", "state")
err = engine.RunUntilIdle(ctx, "order-42")
```

## Durable storage with Pebble

The default compiled runtime store is in-memory. For durable execution, open a Pebble-backed transactional store and close it during shutdown.

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
    axiom.Act("ChargeCard", chargeCard),
)
```

Durability modes:

```go
// Synchronous commits: strongest durability, highest write latency.
store, err := axiom.OpenPebble("data/axiom")

// No fsync on every commit: faster, but recent writes may be lost after a
// process or machine failure.
store, err = axiom.OpenPebble("data/axiom", axiom.PebbleNoSync())

// Group commits by flushing periodically.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleSyncEvery(10*time.Millisecond),
)
```

`WithProductionMode` enables the strict fast runtime and requires a transactional store. It is intended to fail during startup instead of silently falling back to unsupported execution paths.

## Activities and side effects

Rules describe when an activity is scheduled; Go implements the actual external call.

```go
func chargeCard(ctx context.Context, input axiom.Input) (axiom.Output, error) {
    // Make the external operation idempotent using the key supplied by the plan.
    return axiom.Output{
        "transactionId": "txn-123",
        "approved":      true,
    }, nil
}

engine, err := axiom.Open(
    definition,
    axiom.Act("ChargeCard", chargeCard),
)
```

For external effects, model an idempotency key and a retry policy. Axiom records scheduled, completed and failed activities in execution history.

## Concurrency model

Axiom provides process-local linearizability for operations submitted through one engine instance:

- updates to the **same execution ID** are serialized;
- operations for **different execution IDs** may proceed concurrently;
- Pebble transactions never replace or expose the engine's shared store object;
- state and history are committed atomically when the store supports transactions;
- typed integer values remain integers across dispatch and Pebble reopen.

For coordination across multiple processes, place one ownership or routing layer in front of each execution ID, or implement a distributed store with equivalent transactional and concurrency guarantees.

## Replay, history and explanation

```go
history, err := engine.Execution("order-42").History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(engine.Module(), history)
if err != nil {
    return err
}

explanation, err := engine.Execution("order-42").Explain(ctx)
```

Replay validates module identity and reconstructs deterministic runtime state from recorded history. Use the same compiled plan version that produced the history.

## Performance baseline

The current CI baseline was measured on a shared GitHub-hosted `linux/amd64` runner with Go 1.26.5, 4 logical CPUs and concurrency 8. These numbers are useful for coarse regression detection, not as hardware-independent SLAs.

| Scenario | p95 | p99 | Throughput |
|---|---:|---:|---:|
| Go-first Flow, distinct executions | 3.841 ms | 4.788 ms | 9,028 ops/s |
| Go-first Flow, one contended execution | 20.777 ms | 24.880 ms | 772 ops/s |
| Compiled runtime, distinct executions | 0.505 ms | 3.011 ms | 55,011 ops/s |
| Compiled runtime, one contended execution | 1.085 ms | 1.437 ms | 50,938 ops/s |
| Compiled runtime, cold memory execution | 0.800 ms | 4.058 ms | 40,239 ops/s |
| Pebble NoSync, cold durable execution | 3.904 ms | 5.061 ms | 8,773 ops/s |
| Pebble Sync, cold durable execution | 8.688 ms | 10.225 ms | 1,437 ops/s |
| Replay of a 1,000-event history | 1.977 ms | 2.541 ms | 761 runs/s |

The compiled runtime currently has the strongest tail latency. A long-lived, contended Go-first Flow is slower because its current memory store copies and serializes the complete history on each save.

See [`benchmarks/latest.md`](benchmarks/latest.md) for p50, maximum latency, methodology, resilience coverage and reproduction commands.

Run the percentile harness locally:

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

## Resilience validation

The test suite covers:

- concurrent updates to one Go-first execution;
- concurrent updates to one compiled execution;
- independent parallel Pebble executions;
- transaction rollback after a failed Flow effect;
- typed event integer preservation;
- integer preservation through Pebble close and reopen;
- exact replay reconstruction;
- a 16-worker, 8,000-operation Flow soak test;
- Go's race detector for the runtime and stores;
- an external consumer module importing public packages only.

## Packages

| Package | Purpose |
|---|---|
| `github.com/Homiakus/axiom` | Canonical Plan, compiled runtime, typed execution API and Go-first Flow |
| `github.com/Homiakus/axiom/model` | Declarative, file-free, statically validated Go builder |
| `github.com/Homiakus/axiom/axm` | AXM parser and Plan frontend |
| `github.com/Homiakus/axiom/table` | TOML transition-table frontend |
| `github.com/Homiakus/axiom/store/pebble` | Public durable Pebble store package |
| `github.com/Homiakus/axiom/cmd/axiomgen` | Optional typed-boundary code generator |
| `github.com/Homiakus/axiom/cmd/axiombench` | Percentile and resilience benchmark harness |

## Development

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
```

CI also builds a separate consumer module to verify that applications can use the public API without importing internal packages.

## Design guidance

- Use **Flow** when the transition logic is ordinary application code and static inspection is not required.
- Use **model**, **AXM** or **TOML** when validation, impact analysis, explicit claims and explainability are first-class requirements.
- Keep effects idempotent and model their keys explicitly.
- Use stable execution IDs based on the business aggregate being protected.
- Use Pebble Sync when committed state must survive power loss; use NoSync only when the recovery-point tradeoff is acceptable.
- Pin plan versions for durable histories and replay them with the same compiled module.
- Measure strict latency objectives on dedicated hardware with fixed CPU, storage and Go versions.

## License

Apache-2.0. See [`LICENSE`](LICENSE).