# Security

## Local-first default

Rule Studio should default to local-only:

```text
127.0.0.1
no external network
no telemetry by default
```

## Remote mode

If remote mode exists:

```text
authentication
authorization
TLS
CSRF protection
rate limits
audit log
read-only mode
```

## Source safety

Uploaded/opened `.axm` files are text. Do not execute them.

## Function safety

Generated stubs are code, but Studio should not compile or run arbitrary code unless explicitly configured.

## Secrets

Do not store secrets in DSL.

Bad:

```axiom
state Api:
  key: Text = "secret"
```

Use external secret manager / environment.

## Device safety

For physical systems:

```text
manual confirmation for dangerous commands
estop cannot be bypassed by UI
actuator actions require profile
unsafe paths shown in diagnostics
```
