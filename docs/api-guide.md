# Public API guide

Axiom has several frontends, but they are not intended to be equally likely starting points.

## Which API should I start with?

For a new Go application, use this order of preference:

1. **`model`** — default choice for business processes that need rules, claims, activities, static analysis and a Go-native definition.
2. **`Flow`** — use for a small typed reducer when arbitrary Go logic is more valuable than static analysis.
3. **AXM (`axm`)** — use when the process definition should live outside Go code or be consumed by tooling.
4. **TOML (`table`)** — use when the problem is naturally a decision table.

The common declarative lifecycle is:

```text
Definition / AXM / TOML
        ↓
       Plan
        ↓
      Engine
        ↓
       Run
```

`Plan` is the canonical compiled representation. `Engine` owns runtime services and a store. `Run` (`engine.Execution(id)`) is the preferred handle for one durable execution.

## Recommended Go model style

```go
package example

import (
    "context"

    "github.com/Homiakus/axiom"
    "github.com/Homiakus/axiom/model"
)

type Order struct {
    Status string `json:"status"`
    Total  int    `json:"total"`
}

type Submitted struct {
    Total int `json:"total"`
}

func OpenEngine() (*axiom.Engine, error) {
    definition := model.New("Orders")
    order := model.Bind[Order](definition, "Order").Default("Status", "draft")
    submitted := model.EventOf[Submitted](definition)

    definition.Rule("submit").
        On(submitted.Trigger()).
        Set(order.String("Status"), "submitted").
        Set(order.Int("Total"), submitted.Int("Total"))

    definition.Claim("totalIsNotNegative", order.Int("Total").GreaterOrEqual(0))

    return axiom.Open(definition)
}

func Submit(ctx context.Context, engine *axiom.Engine, id string, total int) error {
    return engine.Execution(id).Dispatch(ctx, Submitted{Total: total})
}
```

### Prefer strict typed expression helpers

`TypedField[T]` keeps the compatibility operators (`EQ`, `GT`, `Add`, and others), but new code should prefer helpers that constrain literal values to `T`:

```go
order.Int("Total").GreaterOrEqual(0)
order.String("Status").Equal("submitted")
```

That lets the Go compiler reject accidental literal type mismatches before Axiom compiles the model.

For field-to-field comparisons of the same type, use `EqualField` / `NotEqualField`.

## Activities

Prefer `ActTyped` for application code:

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
    axiom.ActTyped("Charge", func(ctx context.Context, in ChargeInput) (ChargeOutput, error) {
        // external call
        return ChargeOutput{PaymentID: "pay-1"}, nil
    }),
)
```

Use `axiom.Act` and `axiom.Input`/`axiom.Output` mainly at dynamic integration boundaries where maps are already the natural representation.

## Runtime API

Prefer a `Run` handle over repeatedly passing the execution ID to lower-level engine methods:

```go
run := engine.Execution("order-42")

if err := run.Dispatch(ctx, Submitted{Total: 1500}); err != nil {
    return err
}

var state Order
if err := run.State(ctx, &state); err != nil {
    return err
}

explanation, err := run.Explain(ctx)
```

Useful methods include `Dispatch`, `Signal`, `Patch`, `State`, `Status`, `History`, `PendingActivities`, `Explain` and `Cancel`.

## Production semantics

`WithProductionMode()` requires a transactional store and strict fast runtime. It does **not** turn every declared policy field into an implemented runtime guarantee.

At the current runtime level:

- external activities should be idempotent;
- idempotency metadata is meaningful and required for external effects where specified by the compiler;
- `policy.retry`, `policy.timeout` and `policy.concurrency` must not be treated as complete automatic runtime guarantees yet;
- execution serialization is local to one `Engine`; cross-process ownership must be provided by the application.

Do not build correctness assumptions around retry/timeout/concurrency declarations until the runtime semantics documentation marks them as enforced.

## Error handling

Examples intentionally handle every returned error. Production code should do the same. `Must*` helpers are intended for tests, fixtures and initialization paths where a panic is explicitly acceptable.

## Advanced APIs

The root package exposes lower-level compiler/runtime types for compatibility and tooling. Application code normally does not need to depend directly on IDs, raw `Value` representations or internal execution structures. Keep dependencies focused on `Plan`, `Engine`, `Run`, options, activities and the selected frontend.
