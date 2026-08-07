# AXM file specification

This document describes the AXM syntax and behavior implemented by the current parser, compiler, and runtime.

The canonical file extension used by repository examples is `.axm`. Public loaders read the supplied path and do not enforce an extension, so alternative extensions are not a compatibility guarantee.

Runtime guarantees and current policy limitations are documented separately in [`runtime-semantics.md`](runtime-semantics.md).

## File rules

- UTF-8 text.
- Tabs are rejected.
- Top-level declarations have zero indentation.
- Nested blocks use two additional spaces per level.
- `#` starts a line comment outside a quoted string.
- Blank lines are ignored.
- Exactly one `domain` declaration is required.
- Top-level declarations may appear in any order accepted by the parser.

Supported top-level declarations:

```text
domain
import
signal
context
computed
fact
policy
activity
rule
claim
query
```

`import` declarations are parsed into the AST, but the public `Compile`/`CompileAny` path does not currently provide a multi-file resolver or linker. Treat imports as syntax-only until a resolver contract is implemented and tested.

## Minimal module

```axiom
domain Counter

signal Increment:
  by: Int

context State:
  value: Int = 0

rule increment:
  on Increment
  write:
    State.value = State.value + signal.by

query Current:
  return:
    value = State.value
```

## Identifiers and references

Identifiers may contain letters, digits, and `_`; the first character must be a letter or `_`. Dotted references are supported, for example:

```text
State.value
signal.by
output.sent
RegisteredUser.email
runtime.status
```

Compiler scope rules:

- `signal.*` is valid only in a rule triggered by a signal and in activity input/idempotency expressions compiled in signal scope;
- `output.*` is valid only when the rule runs an activity exposing the referenced output field;
- `runtime.*` is valid only in query scope;
- writes may target only declared context fields.

Stable runtime query projections are:

```text
runtime.id
runtime.domain
runtime.status
runtime.version
runtime.createdAt
runtime.updatedAt
runtime.moduleHash
runtime.compilerVersion
runtime.planVersion
```

Canonical `CompilePlan`, `NewPlan`, `Plan.New`, `Open`, and `New` paths reject unsupported runtime projection names with `AX001`. The lower-level raw `Compile` API still returns a compiled `Module` before this canonical Plan/runtime namespace validation step.

## Types

The runtime explicitly checks these type names:

| AXM type | Runtime value |
|---|---|
| `String` | Go `string` |
| `Int` | signed 64-bit integer range |
| `Float` | signed integer or floating-point value |
| `Bool` | Go `bool` |
| `Time` | Go `string` |
| `Duration` | duration literal or string |
| `Object` | `map[string]any` |
| `List<T>` | slice or array |
| `Map<K,V>` | `map[string]any` |
| `T?` | nullable form of a type |

`Int` is a signed 64-bit runtime contract. Signed Go integer widths are normalized losslessly. Unsigned Go integer values are accepted only when the numeric value is less than or equal to `math.MaxInt64`; larger values are rejected with `AX406` instead of being truncated or wrapped.

Unknown AXM type identifiers are not rejected uniformly by the current runtime and should not be used as a stable contract.

## Literals

Supported literals include:

```axiom
"text"
42
3.14
true
false
null
250ms
5s
[1, 2, 3]
{kind: "card", trusted: true}
```

Duration literals start with a number followed by alphabetic unit text. Repository examples use `ms`, `s`, `m`, and `h`.

The Go `model` frontend serializes general literal values through JSON. `model.TryLit(value)` returns serialization errors immediately; `model.Lit(value)` retains them and `Definition.Compile()` reports them as `AX510` with the declaration path.

## Expressions

Implemented operators, from lower to higher practical precedence:

- `implies`;
- `or`;
- `and`;
- `==`, `!=`, `>`, `>=`, `<`, `<=`, `in`;
- `+`, `-`;
- `*`, `/`, `%`;
- unary `not`, unary `-`, and postfix `exists`;
- parentheses.

Examples:

```axiom
User.email exists
not User.disabled
Cart.total >= 100
Risk.status in ["unknown", "expired"]
Payment.status == "paid" implies Payment.id exists
Counter.value + signal.by * 2
(State.total + signal.delta) / 4
State.sequence % 2
-State.offset
```

Arithmetic semantics:

- `+` supports numbers and string concatenation;
- `-`, `*`, `/`, `%` require numeric operands;
- multiplication, division, and modulo bind more tightly than addition and subtraction;
- operators of the same precedence are left-associative;
- signed integer operations remain exact through the `int64` range and report overflow explicitly;
- integer division uses Go-style truncating integer division;
- when a floating operand participates, division and modulo return floating-point results (`math.Mod` semantics for `%`);
- division or modulo by zero returns a runtime error;
- unary `-` accepts numeric values and is supported by both the regular evaluator and fast expression VM.

Implemented calls:

| Call | Meaning |
|---|---|
| `missing(value)` | `true` when the evaluated value is `nil` |
| `changed(Context.field)` | whether a context field is dirty in the current turn |
| `hash(...)` | SHA-256 of JSON-encoded argument values |
| `fixed(...)` | returns a normalized policy string |
| `exponential(...)` | returns a normalized policy string |
| `timer(...)` | preserves the raw timer expression |

The expression parser accepts a generic call shape, but runtime evaluation rejects unknown call names.

## Declarations

### Domain

```axiom
domain Checkout
```

Only one domain is allowed per module.

### Import

```axiom
import Common.Types
import Payments.Core as Payments
```

Parsing is supported. Resolution, alias binding, cyclic-import detection, and multi-file compilation are not implemented by the verified public compile path.

### Signal

```axiom
signal CheckoutRequested

signal PaymentAuthorized:
  paymentId: String
  amount: Float
```

Signal payload fields are read through `signal.<field>`.

### Context

```axiom
context Payment:
  status: String = "idle"
  id: String?
  paidCount: Int = 0
```

Context fields are the only direct write targets. Defaults are expressions evaluated when an execution starts. A missing non-nullable field without a default is initialized as `nil` and may later fail type validation when written or patched; callers should provide valid initial data or defaults.

### Computed

```axiom
computed payable: Bool =
  Cart.total > 0 and Cart.items.length > 0
```

Computed values are pure expressions. Computed dependency cycles are rejected.

### Fact

```axiom
fact RegisteredUser when:
  User.id exists
  User.email exists
expose:
  id = User.id
  email = User.email
```

Every line in `when` must be truthy. Exposed fields are referenced as `RegisteredUser.id` and `RegisteredUser.email`. Fact dependency cycles are rejected.

### Policy

```axiom
policy externalCall:
  retry: 2
  backoff: exponential(100ms)
  timeout: 5s
  concurrency: latest
  idempotency: required
```

Implemented semantics:

- `retry: N` allows at most `N + 1` persisted task attempts. Each attempt receives its own task lease and increments `ActivityTask.Attempt`;
- after a retryable handler failure, runtime clears the lease, returns the task to `pending`, preserves the error, and stores `NextAttemptAt` before another attempt may be leased;
- `backoff: 250ms` and `backoff: fixed(250ms)` configure a fixed delay;
- `backoff: exponential(100ms)` configures deterministic exponential delay from the supplied base duration;
- when retry is configured without `backoff`, runtime uses deterministic exponential delay starting at `100ms`; retry delay is capped at `30s`;
- retry checkpoints produce `ActivityRetryScheduled` history entries, and exhausted budgets produce `ActivityRetryExhausted` before terminal `ActivityFailed`;
- a persisted retry checkpoint can be continued by another `Engine` using the same store; with Pebble it survives closing and reopening the store;
- `timeout` creates a fresh `context.WithTimeout` for each handler attempt. Handlers must observe `ctx` cancellation;
- `concurrency: parallel` adds no activity-level serialization;
- `concurrency: once` serializes calls of that activity inside one `Engine`;
- `concurrency: first` keeps the earliest `pending` or `running` task in the same `execution ID + activity` lane and records later tasks as `TaskSuperseded`;
- `concurrency: latest` supersedes older **pending** tasks in the same lane and keeps only the newest pending task;
- `latest` never forcibly cancels an already `running` Go handler. A new latest task waits behind the current running lease, while later pending arrivals may replace that waiting pending task;
- supersession emits `ActivitySuperseded` history records;
- explicit non-empty `idempotencyKey` deduplication occurs before first/latest supersession, so the same external intent remains deduplicated;
- `idempotency: required` is enforced for `effect: external` activities together with an `idempotencyKey`.

`WithProductionMode()` accepts `parallel`, `once`, `first`, and `latest`, but requires a `TransactionalStore`. Pending-task supersession is executed inside the store transaction. The built-in Pebble store serializes its transactions on the store instance; custom transactional stores must provide sufficient transaction isolation for correct scheduling decisions.

Low-level `Engine.RunUntilIdle` returns `axiom.ErrRetryScheduled` after a retry checkpoint instead of sleeping until a future `NextAttemptAt`. The high-level `Run.Dispatch`, `Run.Signal`, and `Run.Patch` APIs wait for due retries within the caller context and continue draining automatically.

A `catch:` block is parsed and target signal names are validated, but verified runtime dispatch of catch mappings is not implemented.

### Activity

```axiom
activity SendWelcomeEmail:
  require:
    RegisteredUser
  input:
    userId = RegisteredUser.id
    email = RegisteredUser.email
  output:
    sent: Bool
  effect: external
  idempotencyKey: RegisteredUser.id
  policy: externalCall
```

Fields:

- `require:` — conditions that must be truthy;
- `input:` — named input bindings;
- `output:` — declared output fields;
- `effect:` — normally `none`, `local`, or `external`;
- `idempotencyKey:` — expression used for task deduplication;
- `policy:` — referenced policy name.

Activities with `effect != none` require a registered Go handler when the engine is built. `local` and `external` activities require a policy. `external` also requires `idempotency: required` and an idempotency key.

### Rule

```axiom
rule captureRegistration:
  on UserRegistered
  when:
    signal.email exists
  require:
    not User.disabled
  write:
    User.id = signal.userId
    User.email = signal.email
```

A rule requires at least one trigger. Supported trigger forms:

```axiom
on UserRegistered
on changed(User.email)
on timer(24h after Order.createdAt)
```

Multiple triggers can be written as:

```axiom
on:
  SignalA
  changed(State.value)
```

A rule may contain `when`, `require`, `run`, and `write`. When `run` is present, writes are applied after a successful activity result and may reference `output.*`.

### Claim

```axiom
claim paymentHasId:
  always:
    Payment.status == "paid" implies Payment.id exists
```

Claims are checked during execution and before/after writes. A violating write is rejected.

### Query

```axiom
query CheckoutStatus:
  return:
    status = Payment.status
    executionId = runtime.id
    runtimeStatus = runtime.status
    updatedAt = runtime.updatedAt
```

Queries are read-only projections evaluated against an existing execution. Runtime projections expose stable execution metadata but do not expose task locks, worker state, or other internal scheduling details.

For Go-model definitions, prefer `model.Runtime.ID()`, `model.Runtime.Status()`, and the other `model.Runtime.*` helpers over manually spelling `model.Ref("runtime.*")`.

## Runtime entry points

Compile AXM bytes:

```go
module, err := axiom.Compile(source, axiom.WithSourceName("workflow.axm"))
```

Compile AXM or TRIZ-like source:

```go
module, err := axiom.CompileAny(source, axiom.WithSourceName("workflow.axm"))
```

Load an AXM file into a plan:

```go
plan, err := axm.Load("workflow.axm")
```

Create an engine:

```go
engine, err := plan.New(
    axiom.Act("SendWelcomeEmail", handler),
)
```

## Version identifiers

The current compiler records these identifiers in compiled modules:

```text
DSL:      axm/v1
Compiler: axiom-compiler/v2
Plan:     fast-plan/v2
```

These are compiled-artifact identifiers, not semantic-version release tags.

## Known limitations

- Imports have parser/AST support but no verified public resolver/linker.
- Timer triggers are indexed, but a complete wall-clock scheduler contract is not documented as implemented.
- Policy `catch:` targets are parsed/validated but runtime catch dispatch remains incomplete.
- `concurrency: latest` is a **latest pending wins** guarantee; it does not forcibly stop a running handler.
- Durable retry and supersession do not make external effects exactly once; handlers for external systems must remain idempotent.
- The raw low-level `Compile` API does not itself reject unknown `runtime.*` projection names; canonical Plan/Open/New paths do.
- Unknown AXM type identifiers are not rejected consistently.
