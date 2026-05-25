# Go Codegen

Codegen keeps the boundary between DSL and Go type-safe.

Current project already has `axiomgen` for Axiom v0. For TRIZ DSL the flow should
be:

```text
TRIZ DSL -> normalized Axiom v0/IR -> axiomgen -> typed Go wrapper
```

## Generated files

| File | Regeneration |
|---|---|
| `*_axiom.gen.go` | always overwritten |
| `*_activities.go` | created once, then smart-merged |
| `*_test.go` | optional generated contract/safety tests |

## Generated content

- embedded source and model hash;
- constants for functions/activities;
- typed input/output structs;
- `Activities` interface;
- adapters to `axiom.ActivityRegistry`;
- compile-time checks for missing implementations.

## Example

```go
func (impl HydroPilotActivities) SendUartRequest(
    ctx context.Context,
    input SendUartRequestInput,
) (SendUartRequestOutput, error) {
    return SendUartRequestOutput{Status: "ok"}, nil
}
```

## Rule

Go code implements functions. DSL decides when they run and what state changes
after they return.
