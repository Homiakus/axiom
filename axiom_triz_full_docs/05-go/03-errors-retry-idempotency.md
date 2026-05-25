# Errors, Retry, Idempotency

Profiles define how runtime treats function failures.

## Error model

Function errors should be classifiable:

```text
temporary
permanent
timeout
cancelled
domain error
```

Runtime maps them to retry, catch event, failure state or compensation.

## Retry

Retry belongs to runtime profile, not hidden loops inside the Go function.

```axiom
profile externalCall:
  timeout: 5s
  retry: 3
  idempotent
```

## Idempotency

External functions that can be retried need a stable idempotency key. It should
come from state/event values, not from current time.

Good:

```text
hash("measurement-db", zoneId, measuredAt)
```

Bad:

```text
hash(now())
```

## Timeout

Runtime owns timeout and cancellation through `context.Context`.

## Compensation

Compensation is a separate function/rule path. It should be explicit, auditable
and visible in Studio.
