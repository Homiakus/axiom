# Testing

Use different test levels for different risks.

## Levels

| Level | What it proves |
|---|---|
| parser/normalizer | TRIZ DSL maps to expected IR |
| function unit | Go implementation handles inputs/errors |
| rule test | event + state -> expected writes/actions |
| safety test | `always` cannot be violated by known paths |
| replay test | history rebuilds state without external calls |
| golden trace | important scenario keeps the same timeline |

## Rule test shape

```text
given state
when event
mock function output
expect actions
expect writes
expect always pass/fail
```

## Generated tests

Codegen can generate baseline tests:

- all declared functions have Go implementations;
- output fields match DSL;
- critical functions have profiles;
- actuator writes are protected by at least one `always`;
- example scenarios reach fixpoint.
