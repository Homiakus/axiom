# Activity failures, retry, and policy catch

This guide covers the application-facing failure path for compiled Axiom activities.

## Return a stable domain error code

Use `axiom.FailActivity` when business/runtime policy should distinguish one terminal activity failure from another:

```go
result, err := paymentGateway.Charge(ctx, request)
if err != nil {
    if errors.Is(err, ErrCardDeclined) {
        return nil, axiom.FailActivity("PaymentDeclined", err)
    }
    return nil, err
}
return axiom.Output{"ok": result.OK}, nil
```

`FailActivity` keeps the original error as `Unwrap()` cause and adds a stable code through `ActivityErrorCoder`.

Custom application error types may implement:

```go
type ActivityErrorCoder interface {
    ActivityErrorCode() string
}
```

Do not use free-form human error messages as policy keys. Axiom intentionally does not parse arbitrary error text to select a catch branch.

## Configure retry and catch

AXM:

```axiom
policy payment:
  retry: 2
  backoff: exponential(100ms)
  timeout: 3s
  concurrency: parallel
  idempotency: required
  catch:
    PaymentDeclined -> PaymentDeclinedSignal
    * -> PaymentFailureSignal
```

Go model:

```go
policy := definition.Policy("payment")
policy.
    Retry(2).
    ExponentialBackoff(100 * time.Millisecond).
    Catch("PaymentDeclined", "PaymentDeclinedSignal").
    CatchAll("PaymentFailureSignal")
```

## When catch runs

Catch is evaluated only for a **terminal handler failure**.

For `retry: 2` the order is:

```text
attempt 1 fails -> durable retry checkpoint
attempt 2 fails -> durable retry checkpoint
attempt 3 fails -> retry exhausted -> catch lookup
```

Intermediate failures do not emit catch signals.

Exact code has priority over `*`. If no mapping matches, the normal terminal `AX505` activity error is returned and execution becomes failed.

## Catch signal payload

The catch signal environment contains:

```text
activity
rule
taskId
errorCode
error
attempt
maxAttempts
```

The AXM signal declaration only needs fields referenced by its rules. Example:

```axiom
signal PaymentDeclinedSignal:
  errorCode: String
  error: String
  attempt: Int

rule recordDecline:
  on PaymentDeclinedSignal
  write:
    Payment.lastError = signal.error
```

## Transaction semantics

When the store is transactional, terminal task failure and catch processing happen in the same completion transaction.

Successful catch:

- keeps the activity task terminal `failed` for audit;
- records `ActivityFailed` with `caught: true`;
- records `ActivityCaught`;
- evaluates the catch target signal rules;
- commits resulting writes;
- returns the execution to `Waiting`.

If the catch target itself fails—for example a claim rejects its write—Axiom returns `AX511` and rolls back the catch transaction so partial catch state/history is not committed.

The task lease was acquired before that transaction. After rollback the task may remain `running` until lease recovery. This is deliberate at-least-once orchestration behavior.

## What is not catchable

Catch is for handler/domain failures. Deterministic activity contract errors such as:

- missing output field (`AX503`);
- wrong output field type (`AX504`);

are not routed to policy catch mappings. Fix the handler/model contract instead of catching those errors.

## Idempotency remains mandatory

Retry, lease recovery, and failed catch rollback can all lead to another handler attempt after an external system may already have observed a previous attempt.

For external effects:

- keep `idempotency: required`;
- provide a stable `idempotencyKey`;
- pass the same key to the external API/device when possible;
- or persist a business-level deduplication record.

Policy catch improves domain routing; it does not create exactly-once side effects.
