# Axiom

Axiom is an embeddable deterministic workflow, rules and state-transition library for Go. The application imports one package, while Axiom handles parsing and compilation of `.axm` models, execution to a fixpoint, activities, persistence, replay, diagnostics, claims and impact analysis.

## Install

```bash
go get github.com/Homiakus/axiom
```

## Minimal use

```go
package main

import (
    "context"
    "os"

    "github.com/Homiakus/axiom"
)

func main() {
    source, err := os.ReadFile("workflow.axm")
    if err != nil {
        panic(err)
    }

    engine, err := axiom.CompileAndNew(source,
        axiom.Act("SendWelcomeEmail", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
            return axiom.Output{"sent": true}, nil
        }),
    )
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    if err := engine.Start(ctx, "user-1", nil); err != nil {
        panic(err)
    }
    if err := engine.Signal(ctx, "user-1", "UserRegistered", axiom.Input{
        "userId": "user-1",
        "email":  "user@example.com",
    }); err != nil {
        panic(err)
    }
    if err := engine.RunUntilIdle(ctx, "user-1"); err != nil {
        panic(err)
    }
}
```

A complete executable model is available at `axiom-main/examples/axiom-files/welcome.axm`.

## What remains available

- stable and TRIZ-facing `.axm` syntax;
- parser, compiler and structured `AXnnn` diagnostics;
- deterministic rule evaluation and FastVM;
- signals, state, computed values, facts, policies and activities;
- retries, timeouts, idempotency and worker execution;
- claims, queries, traces, history and replay;
- in-memory and Pebble-backed stores;
- module hashes, compatibility checks and impact analysis;
- typed Go boundary generation through `axiom-main/tools/axiomgen`.

## Public API

Common entry points are `Load`, `Compile`, `CompileAny`, `CompileBundle`, `New`, `CompileAndNew`, `Act`, `Acts`, `OpenPebble` and `ReplayFromHistory`.

The default store is in-memory. For durable execution:

```go
store, err := axiom.OpenPebble("data/axiom")
if err != nil {
    return err
}
defer store.Close()

engine, err := axiom.New(module,
    axiom.WithStore(store),
    axiom.WithProductionMode(),
)
```

## Repository layout

```text
.
├── axiom.go                  # compact public facade
├── axiom-main/               # isolated implementation module
│   ├── internal/lang/        # parser and AST
│   ├── internal/compiler/    # validation and compiled module
│   ├── internal/runtime/     # engine, FastVM, workers and replay
│   ├── internal/store/       # memory and Pebble persistence
│   ├── internal/triz/        # TRIZ normalization
│   ├── pkg/axiom/            # implementation-facing API
│   └── tools/axiomgen/       # optional typed-code generator
├── LICENSE
└── README.md
```

## Verification

```bash
go test ./...

cd axiom-main
go test ./...

cd tools/axiomgen
go test ./...
```

Licensed under Apache-2.0.
