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
- `runtime.*` is accepted only in query scope, but current runtime resolution returns `nil` for runtime projections;
- writes may target only declared context fields.

## Types

The runtime explicitly checks these type names:

| AXM type | Runtime value |
|---|---|
| `String` | Go `string` |
| `Int` | Go integer types |
| `Float` | Go integer or floating-point values |
| `Bool` | Go `bool` |
| `Time` | Go `string` |
| `Duration` | duration literal or string |
| `Object` | `map[string]any` |
| `List<T>` | slice or array |
| `Map<K,V>` | `map[string]any` |
| `T?` | nullable form of a type |

Unknown type identifiers are not rejected uniformly by the current runtime and should not be used as a stable contract.

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

## Expressions

Implemented operators, from lower to higher practical precedence:

- `implies`;
- `or`;
- `and`;
- `==`, `!=`, `>`, `>=`, `<`, `<=`, `in`;
- `+`, `-`;
- unary `not`, unary `-`, and postfix `exists`;
- parentheses.

Examples:

```axiom
User.email exists
not User.disabled
Cart.total >= 100
Risk.status in ["unknown", "expired"]
Payment.status == "paid" implies Payment.id exists
Counter.value + signal.by
```

`+` supports numbers and string concatenation. `-` requires numeric operands. Multiplication, division, and modulo tokens are not accepted by the current AXM lexer.

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
  timeout: 5s
  concurrency: once
  idempotency: required
```

Policy entries are parsed as expressions. Current enforcement is partial:

- `retry` sets task `MaxAttempts`, but failed handlers are not automatically requeued;
- `timeout` is parsed but does not wrap the handler in a timeout context;
- `concurrency` is parsed but does not control worker scheduling;
- `idempotency: required` is enforced for `effect: external` activities together with an `idempotencyKey`.

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
    paid = Payment.status == "paid"
```

Queries are read-only projections evaluated against an existing execution.

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
- Policy retry, timeout, concurrency, and catch semantics are incomplete.
- `runtime.*` query projections currently resolve to `nil` in the verified evaluator.
- Multiplication, division, and modulo are not accepted by the AXM lexer.
- Unknown type identifiers are not rejected consistently.
