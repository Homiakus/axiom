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

var (
    orderStatus    = model.Key[Order, string]("Status")
    orderTotal     = model.Key[Order, int]("Total")
    submittedTotal = model.Key[Submitted, int]("Total")
)

func OpenEngine() (*axiom.Engine, error) {
    definition := model.New("Orders")
    order := model.Bind[Order](definition, "Order")
    submitted := model.EventOf[Submitted](definition)

    model.StateDefault(order, orderStatus, "draft")
    status := model.StateField(order, orderStatus)
    total := model.StateField(order, orderTotal)
    incomingTotal := model.EventField(submitted, submittedTotal)

    definition.Rule("submit").
        On(submitted.Trigger()).
        Set(status, "submitted").
        Set(total, incomingTotal)

    definition.Claim("totalIsNotNegative", total.GreaterOrEqual(0))

    return axiom.Open(definition)
}

func Submit(ctx context.Context, engine *axiom.Engine, id string, total int) error {
    return engine.Execution(id).Dispatch(ctx, Submitted{Total: total})
}
```

### Small models vs reusable field keys

For a small definition, direct helpers remain intentionally concise:

```go
order.String("Status")
order.Int("Total")
submitted.Int("Total")
```

When a field is referenced in many rules, claims, activities or queries, prefer declaring its name once:

```go
var orderTotal = model.Key[Order, int]("Total")

total := model.StateField(order, orderTotal)
```

`FieldKey[Owner, Value]` provides two useful checks without introducing code generation:

- the `Owner` generic prevents using an `Order` key with a different state/event type;
- the `Value` generic is validated against the selected Go field when the key is resolved. Optional pointer fields may use the pointed-to logical value type.

A key may use either the Go field name or its serialized `axiom`/`json` name. Invalid names and type mismatches are returned as model diagnostics at `Compile`, not normal-path panics.

Related helpers:

- `model.StateField(state, key)` — resolve a reusable state field;
- `model.EventField(event, key)` — resolve a reusable event field;
- `model.StateChanged(state, key)` — create `changed(...)` from the same key;
- `model.StateDefault(state, key, value)` — set a type-checked default.

This localizes reflection-based names instead of pretending reflection can remove them entirely. If a project needs zero handwritten field names, use generated bindings as a separate tooling layer rather than making the core builder opaque.

### Prefer strict typed expression helpers

`TypedField[T]` keeps the compatibility operators (`EQ`, `GT`, `Add`, and others), but new code should prefer helpers that constrain literal values to `T`:

```go
total.GreaterOrEqual(0)
status.Equal("submitted")
```

That lets the Go compiler reject accidental literal type mismatches before Axiom compiles the model.

For field-to-field operations, prefer the typed `*Field` helpers. They require both operands to use the same Go type:

```go
total.GreaterOrEqualField(minimum)
subtotal.PlusField(tax)
status.EqualField(previousStatus)
```

Available field-to-field helpers cover equality, ordering and arithmetic: `EqualField`, `NotEqualField`, `GreaterThanField`, `GreaterOrEqualField`, `LessThanField`, `LessOrEqualField`, `PlusField`, `MinusField`, `TimesField`, `DividedByField` and `ModuloField`.

The legacy `any`-based operators remain useful for dynamic expressions and compatibility, but they should not be the default in new application code.

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

`ActTyped` validates its input/output shapes during Engine construction. Supported shapes are structs, pointers to structs and maps with string keys; unsupported scalar shapes fail fast instead of silently producing an empty output.

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

`WithProductionMode()` requires a transactional store and strict fast runtime. It is the recommended mode when activity policy guarantees are part of application correctness.

Current runtime guarantees include:

- **retry** — activity attempts are durable; `Attempt`, `MaxAttempts` and `NextAttemptAt` are persisted so a new `Engine` can continue retry after restart when the store is durable;
- **backoff** — fixed durations and `exponential(...)` are supported; an omitted backoff uses deterministic exponential delay;
- **timeout** — applied per activity attempt through the activity context;
- **concurrency: parallel** — no additional serialization;
- **concurrency: once** — serialized per activity inside one `Engine`;
- **concurrency: first** — the first active pending task wins its execution/activity lane and later pending tasks are superseded;
- **concurrency: latest** — the newest pending task supersedes older pending tasks in the same lane; an already-running Go handler is not forcibly cancelled;
- **idempotency** — explicit idempotency keys deduplicate the same external intent in the configured store and take precedence over first/latest supersession.

Important boundaries remain: execution locking and `once` are not distributed locks; exactly-once delivery to an external system is not guaranteed; and guarantees that depend on transactional task supersession require a `TransactionalStore` such as Pebble in production mode.

See `docs/runtime-semantics.md` for the detailed contract and failure boundaries.

## Error handling

Examples intentionally handle returned errors. Production code should do the same. `Must*` helpers are intended for tests, fixtures and initialization paths where a panic is explicitly acceptable.

The declarative model builder accumulates user-facing diagnostics for invalid state/event shapes and unknown fields and reports them from `Compile` instead of using panic as the normal validation path. Literal encoding failures and invalid reusable field keys are likewise reported as model diagnostics; use `TryLit` when immediate literal validation is preferable.

## Advanced APIs

The root package exposes lower-level compiler/runtime types for compatibility and tooling. Application code normally does not need to depend directly on IDs, raw `Value` representations or internal execution structures. Keep dependencies focused on `Plan`, `Engine`, `Run`, options, activities and the selected frontend.
