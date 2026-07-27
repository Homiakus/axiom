# Axiom

Axiom is a deterministic state-transition, workflow and decision engine for Go. Files are optional: start with typed Go reducers, use the declarative Go builder when static analysis matters, or load AXM/TOML into the same canonical `Plan`.

## Install

```bash
go get github.com/Homiakus/axiom
```

## Fastest start: typed Go flow

```go
type State struct { Count int }
type Increment struct { By int }

flow := axiom.NewFlow("counter", State{})
axiom.Handle(flow, func(ctx context.Context, state State, event Increment) (axiom.FlowResult[State], error) {
    state.Count += event.By
    return axiom.Next(state), nil
})

engine, _ := axiom.OpenFlow(flow)
run := engine.Execution("counter-1")
_ = run.Dispatch(ctx, Increment{By: 2})
state, _ := run.State(ctx)
```

This mode is file-free and fully typed. Because handlers are arbitrary Go code, its analysis level is `axiom.AnalysisOpaque`.

## Declarative Go model

`model` keeps the model in Go while preserving compiler validation, dependency indexes, claims and impact analysis:

```go
definition := model.New("Welcome")
user := model.State[User](definition, "User")
registered := model.Event[UserRegistered](definition, "UserRegistered")

definition.Rule("capture").
    On(registered.Trigger()).
    Set(user.Field("ID"), registered.Field("UserID"))

engine, err := axiom.Open(definition)
```

## Optional frontends

```go
plan, err := axm.Load("workflow.axm")
engine, err := plan.New()

plan, err = table.Load("workflow.toml")
engine, err = plan.New()
```

AXM and TOML are frontends, not runtime requirements. Both compile into `axiom.Plan`.

## Ergonomic execution API

```go
run := engine.Execution("order-42")
err := run.Dispatch(ctx, OrderCreated{OrderID: "42"})

var state OrderState
err = run.State(ctx, &state)

history, err := run.History(ctx)
pending, err := run.PendingActivities(ctx)
explanation, err := run.Explain(ctx)
```

`Dispatch` automatically creates a missing execution, sends the event and runs inline activities until idle. Existing `Start`, `Signal`, `Patch`, `Query` and `RunUntilIdle` methods remain available.

## Packages

- `github.com/Homiakus/axiom` — canonical Plan, runtime and Go-first flow API
- `github.com/Homiakus/axiom/model` — declarative file-free builder
- `github.com/Homiakus/axiom/axm` — AXM frontend
- `github.com/Homiakus/axiom/table` — TOML transition-table frontend
- `github.com/Homiakus/axiom/store/pebble` — durable Pebble store
- `github.com/Homiakus/axiom/cmd/axiomgen` — optional typed boundary generator

## Validation

```bash
go test ./...
go vet ./...
```

Licensed under Apache-2.0.
