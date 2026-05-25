# Function Contract

Every TRIZ `function` becomes a managed Go activity.

## Signature

Generated wrappers should expose typed signatures:

```go
type SendNotificationInput struct {
    Message string
}

type SendNotificationOutput struct {
    Sent bool
}

func (impl Activities) SendNotification(
    ctx context.Context,
    input SendNotificationInput,
) (SendNotificationOutput, error)
```

## Allowed

- call external APIs;
- read injected dependencies;
- return structured output;
- return typed/domain errors;
- be idempotent when profile requires it.

## Forbidden

- mutate Axiom state directly;
- schedule rules;
- write history;
- hide retries inside the function when runtime owns retry;
- return fields not declared in DSL.

## Dependency injection

Use normal Go constructors:

```go
type Activities struct {
    UART UARTClient
    DB   MeasurementStore
}
```

## Testing

Test functions alone with Go unit tests. Test rules with fake function outputs
through Axiom runtime/simulator.
