# ADGO remote workers

ADGO can run workers on hosts that have **no direct access to the durable execution store**. The coordinator exposes the worker protocol over HTTP and validates all durable task transitions locally.

The protocol is intentionally small:

```text
POST /v1/poll
POST /v1/heartbeat
POST /v1/complete
POST /v1/fail
```

Every response carries:

```text
X-ADGO-Worker-Protocol: adgo-worker-v1
```

## Architecture

```text
                 trusted state network

      +----------------------------------------+
      | coordinator host                       |
      |                                        |
API ->| Engine -> Store / Pebble / FileStore   |
      |   ^                                    |
      |   | HTTPWorkerServer                   |
      +---|------------------------------------+
          |
          | TLS + workload auth
          |
  +-------+------------------------------+
  |                                      |
  v                                      v
worker host A                         worker host B
HTTPWorkerClient                     HTTPWorkerClient
local Registry                       local Registry
LLM/search/tools                     effect integrations
```

Workers receive only the durable activity request they are authorized to execute. They do not need database credentials.

## Coordinator

```go
server, err := adgo.NewHTTPWorkerServer(engine, adgo.HTTPWorkerServerOptions{
    BearerToken: os.Getenv("ADGO_WORKER_TOKEN"),
})
if err != nil {
    return err
}

httpServer := &http.Server{
    Addr:              ":8443",
    Handler:           server,
    ReadHeaderTimeout: 5 * time.Second,
}

// Prefer TLS termination in http.Server, reverse proxy or service mesh.
return httpServer.ListenAndServeTLS(certFile, keyFile)
```

For production identity systems use `Authorize` instead of a static bearer token:

```go
server, err := adgo.NewHTTPWorkerServer(engine, adgo.HTTPWorkerServerOptions{
    Authorize: func(r *http.Request) bool {
        return verifyWorkloadIdentity(r)
    },
})
```

## Worker

```go
registry := adgo.NewRegistry()
registry.Activity("Search", search)
registry.Activity("Generate", generate)

client := &adgo.HTTPWorkerClient{
    BaseURL:     "https://coordinator.internal:8443",
    BearerToken: os.Getenv("ADGO_WORKER_TOKEN"),
}

err := client.Run(ctx, adgo.WorkerSpec{
    ID:          "worker-eu-1",
    Activities:  []string{"Search", "Generate"},
    Concurrency: 16,
    LeaseTTL:    30 * time.Second,
    MaxRisk:     adgo.RiskMedium,
}, registry)
```

`WorkerSpec` is part of the admission boundary:

- activity allowlist;
- concurrency;
- lease duration;
- maximum risk.

Do not give a worker permissions for activities it does not need.

## Durable protocol

### Poll

Worker sends `WorkerSpec`.

Coordinator:

1. scans/ranks durable pending work;
2. checks worker compatibility;
3. CAS-claims exactly one task;
4. records WorkerID, attempt and LeaseUntil;
5. returns a `WorkItem`.

`204 No Content` means no compatible work is currently available.

### Heartbeat

The remote client automatically heartbeats every `LeaseTTL/3` while a handler is running.

A handler can publish explicit progress through the same context helper used by local workers:

```go
adgo.ActivityHeartbeat(ctx, map[string]any{
    "documents": 37,
})
```

The client forwards that heartbeat to the coordinator.

### Complete

Completion contains:

- `WorkToken`;
- `ActivityResult`;
- observed duration.

The coordinator accepts it only if the fencing token is still current.

### Fail

Remote error transport preserves:

- `FailureClass`;
- message;
- RetryAfter.

The coordinator applies its normal retry/repair/reconciliation policy.

## Fencing across the network

A remote worker cannot commit merely because it still holds a serialized work item.

Every mutation validates:

```text
ExecutionID
TaskID
WorkerID
Attempt
LeaseUntil > now
```

If another worker recovered the task after lease expiry, the old remote worker receives a `409 stale_task` mapped to `ErrStaleTask`.

The old result is not committed.

## Network failure cases

### Poll response lost

The coordinator may already have committed the claim.

The worker does not know the task. Its lease expires and the coordinator recovers it.

### Heartbeat lost temporarily

Later heartbeat can extend the lease as long as it has not expired.

If the lease expires first, the worker is stale and its eventual result is rejected.

Choose LeaseTTL large enough for ordinary network jitter.

### Complete request lost before coordinator receives it

Worker may retry the HTTP request with the same `WorkToken` while its lease remains valid.

### Complete committed but HTTP response lost

A repeated Complete receives stale/conflict semantics because the active task is already gone. The workflow result remains durably committed.

For external side effects, provider idempotency is still mandatory. HTTP transport does not turn at-least-once I/O into exactly-once.

## Security

Minimum production rules:

- TLS;
- workload authentication;
- private network / ingress restriction;
- worker activity allowlists;
- short-lived credentials where possible;
- no durable-store credentials on worker hosts unless they are separately required;
- no raw provider secrets in activity payloads;
- reverse proxy/service mesh request-size and rate limits.

The built-in static bearer token is useful for compact deployments and tests. Stronger deployments should use `Authorize` with mTLS identity, signed service tokens or an authenticating proxy.

See [`SECURITY.md`](SECURITY.md).

## Protocol compatibility

The current protocol version is:

```text
adgo-worker-v1
```

Workers and coordinators should check `X-ADGO-Worker-Protocol` during controlled upgrades when compatibility matters.

Plan compatibility is independent: an execution remains pinned to its exact PlanDigest on the coordinator. Remote workers receive the already-selected activity request and do not reinterpret the full workflow graph.

## What the protocol deliberately does not expose

Remote workers do not receive APIs to:

- patch execution state;
- migrate plans;
- resolve human decisions;
- delete executions;
- modify budgets;
- access unrelated execution history.

Those are privileged control-plane operations and belong behind the application's authenticated operator API.
