# ADGO changelog

This file tracks major ADGO engine milestones. It is intentionally capability-oriented rather than a list of every commit.

## Unreleased — Production Engine

### Execution architecture

- Added `Engine` production coordinator/worker split while preserving embedded `Runtime` compatibility.
- Added durable task lifecycle: pending, claimed/running, completion/failure commit.
- Added worker leases, automatic/manual heartbeats and stale-worker fencing.
- Added repeated lease-loss quarantine and operator recovery decisions.
- Added `ExecutionCatalog` for fleet polling/query.
- Added multi-plan `Host` capable of serving multiple immutable Plan digests.
- Added batteries-included `OpenProduction` assembly.

### Durable storage

- Kept `MemoryStore` for tests/ephemeral execution.
- Hardened `FileStore` as a shared-filesystem durable backend.
- Added `PebbleStore` with atomic latest + immutable-version commits, durable inbox and execution catalog.
- Added optional store capabilities for versioning, deletion and pruning.

### Reliability

- Added resilient compensation recovery after coordinator crashes.
- Added bounded compensation retry/timeout policy wrapper.
- Hardened scheduler admission to reserve existing active work and cumulative batch cost.
- Added cross-execution/process admission control with concurrency permits and token-bucket rate limiting.
- Added graceful worker `BeginDrain` / `Drain` for rolling deployments.

### Adaptive execution

- Added adaptive provider routing with hard policy filtering, EWMA quality/latency/cost, reliability scoring, exploration and circuit breaking.
- Added durable `ProviderHealthStore` so provider health survives restart and can be shared by coordinators.
- Added explicit provider fallback ranking.
- Added shared content-addressed activity result cache with TTL and single-flight leases.
- Added opt-in hedged and ensemble execution for pure activities with conservative aggregate budget accounting.

### Human and external interaction

- Added durable execution pause/resume/cancel/budget/data patch operations.
- Added human approve/edit/reject/retry/confirm/abort protocol.
- Added durable high-risk approval and ambiguous-side-effect reconciliation.
- Added deterministic signal routing and optional payload-before-event commit.
- Added revision-bound callback/awaitable tokens.

### Long-lived workflows

- Added immutable historical inspection and execution forks.
- Added conservative compatible plan migration at quiescent points.
- Added continue-as-new for bounded history growth.
- Added durable fixed-interval schedules with deterministic firing execution IDs and bounded catch-up.
- Added production child workflow handles and deterministic child IDs across multiple plans.
- Added operator rewind of one affected dependency subgraph.

### Observability and operations

- Added resumable durable history watch stream.
- Added execution queries.
- Added execution diagnostics and invariant audit.
- Added fleet audit.
- Added explicit terminal retention, archive hook and immutable-version pruning.
- Added production operations runbook and security model.

### Remote workers

- Added authenticated HTTP worker protocol (`poll`, `heartbeat`, `complete`, `fail`).
- Added remote worker client that executes a local Registry while all durable execution state stays on the coordinator.
- Added protocol-level stale-task/conflict mapping and bearer/custom authorization.

### Testing

Added regression/failure tests for:

- worker fencing;
- heartbeat lease extension;
- provider fallback;
- independent repair anchors;
- plan migration;
- historical forks;
- callback awaitables;
- schedules;
- continue-as-new;
- cumulative scheduler budget admission;
- durable provider health across restart;
- admission permit recovery;
- multi-plan Host;
- Pebble reopen/catalog/inbox/version durability;
- compensation crash recovery;
- activity cache reuse;
- deterministic signal routing;
- operator rewind;
- speculative/ensemble selection;
- graceful worker drain;
- retention/version pruning;
- diagnostics;
- deterministic child workflow identity;
- authenticated remote worker transport.

### Documentation

- Rewrote `adgo/README.md` as a production implementation guide.
- Rewrote `adgo/ARCHITECTURE.md` as an invariant contract.
- Added `adgo/OPERATIONS.md` runbook.
- Added `adgo/SECURITY.md`.
- Updated root `ADGO.md` Russian production overview.
- Added runnable `adgo/examples/production`.

## Initial ADGO reference implementation

The initial ADGO package introduced:

- immutable Plan compilation and digest pinning;
- graph execution primitives;
- static validation;
- deterministic internal nodes;
- embedded activity execution;
- durable file/memory snapshots and inbox;
- bounded retry/throttle;
- quality gates and targeted repair;
- convergence/oscillation detection;
- compensation stack;
- content-addressed artifacts;
- adaptive child Plan validation;
- embedded fan-out/subflows;
- explainability, metrics and snapshot replay;
- runnable IRIS-like example.
