# Changelog

All notable public API, runtime, compatibility, and developer-experience changes are recorded here.

Axiom follows the pre-v1 Semantic Versioning policy described in [`docs/versioning.md`](docs/versioning.md).

## [Unreleased]

### Added

- durable task-level retry checkpoints backed by `ActivityTask.Attempt`, `MaxAttempts`, and `NextAttemptAt`;
- `ActivityRetryScheduled` and `ActivityRetryExhausted` history events;
- public `axiom.ErrRetryScheduled` / `axiom.RetryScheduledError` for low-level schedulers;
- retry `backoff` policy with fixed duration, `fixed(...)`, and `exponential(...)` forms;
- `model.PolicyBuilder.Backoff` and `ExponentialBackoff` helpers;
- memory-store Engine replacement and Pebble close/reopen regression tests for retry recovery;
- terminal `TaskSuperseded` status and `ActivitySuperseded` history entries;
- transactional pending-task supersession for `concurrency: first` and `concurrency: latest`;
- stable runtime query namespace validation and `model.Runtime.*` helpers;
- typed activity domain failures via `axiom.FailActivity(code, err)` / `ActivityErrorCoder`;
- runtime policy `catch:` dispatch with exact error-code matching and `*` fallback;
- `model.PolicyBuilder.Catch` and `CatchAll` helpers;
- `ActivityCaught` history and standard catch-signal metadata payload.

### Changed

- `retry: N` now means up to `N + 1` persisted task leases/handler attempts instead of an in-process handler loop;
- `timeout` remains scoped to one handler attempt;
- `Run.Dispatch`, `Run.Signal`, and `Run.Patch` keep synchronous behavior by waiting for due durable retries within the caller context;
- low-level `Engine.RunUntilIdle` returns `ErrRetryScheduled` after a retry checkpoint instead of sleeping until `NextAttemptAt`;
- delayed in-memory tasks are prefiltered before polling, preventing pending-queue spin when all work is scheduled for the future;
- retry-aware stores preserve indexed `TaskDedupStore` behavior instead of falling back to linear task scans;
- `concurrency: first` keeps the earliest active task in one execution/activity lane and records later scheduled tasks as superseded;
- `concurrency: latest` replaces older pending work with the newest pending task, but never pretends to forcibly cancel arbitrary running Go code;
- explicit non-empty idempotency keys continue to deduplicate the same external intent before supersession logic is applied;
- production mode accepts `parallel`, `once`, `first`, and `latest` when used with a transactional store;
- unknown `runtime.*` projections are rejected on canonical Plan/Open/New paths instead of silently becoming `nil`;
- policy catch routing now happens only after retry exhaustion/terminal handler failure; intermediate retry attempts never dispatch catch signals;
- exact coded catches take precedence over wildcard fallback; output-contract failures `AX503`/`AX504` remain terminal runtime validation errors rather than catchable domain failures;
- a failing catch target rolls back the catch transaction and returns `AX511` instead of committing partial catch state.

### Planned

- distributed execution ownership/coordination semantics for multiple processes;
- multi-file AXM import resolution and linking;
- explicit wall-clock timer scheduler contract;
- operational recovery tooling for failed catch/lease scenarios;
- public API classification and cleanup before `v1.0.0`.

## [v0.1.0] - 2026-08-07

First versioned public baseline of Axiom.

### Developer experience

- established `model` as the recommended frontend for new Go applications;
- documented the canonical mental model `Definition -> Plan -> Engine -> Run`;
- added typed expression helpers while retaining compatibility operators;
- hardened `ActTyped` with fail-fast input/output shape validation (`AX507`), nil-handler checks, and named string-key map outputs;
- replaced model-builder panics for unknown fields and invalid state/event types with `AX509` compile diagnostics;
- added `model.TryLit` and retained invalid literal encoding failures as precise `AX510` diagnostics;
- modernized runnable examples and removed ignored errors from the Go-first example;
- added public API selection and pre-v1 versioning guides.

### Runtime policies

- implemented `retry: N` as the initial activity handler call plus up to `N` immediate in-process retries;
- implemented per-attempt activity `timeout` using `context.WithTimeout`;
- implemented process-local `concurrency: once` serialization per activity and retained unrestricted `parallel` behavior;
- production mode accepts implemented retry/timeout/once/parallel policies;
- production mode rejects `concurrency: latest/first` with `AX508` in the frozen v0.1.0 baseline.

### Expressions and numeric behavior

- completed AXM arithmetic support for `*`, `/`, `%`, unary `-`, precedence, associativity, zero-division handling, and slow/fast runtime parity;
- preserved exact signed integer arithmetic and comparison through the full `int64` range instead of routing large values through `float64`;
- added explicit signed integer overflow errors;
- defined AXM `Int` as a signed 64-bit runtime value;
- accepted unsigned Go integer inputs only when losslessly representable within `math.MaxInt64` and rejected larger values with `AX406`.

### Queries

- implemented stable `runtime.*` query projections for execution metadata:
  - `runtime.id`;
  - `runtime.domain`;
  - `runtime.status`;
  - `runtime.version`;
  - `runtime.createdAt`;
  - `runtime.updatedAt`;
  - `runtime.moduleHash`;
  - `runtime.compilerVersion`;
  - `runtime.planVersion`.

### Reliability and hardening

- fixed a fuzz-discovered parser panic for truncated map expressions;
- added regression coverage for parser crashes, runtime policy semantics, arithmetic parity, large integer precision, overflow, integer range boundaries, runtime query projections, and model literal diagnostics;
- retained full CI gates for tests, race detection, vet, vulnerability scanning, parser/TRIZ fuzz smoke tests, external-consumer compilation, runnable examples, and performance benchmarks.

### Known limitations in v0.1.0

- retry is immediate and in-process; durable task-level retry/backoff is not yet implemented in the frozen v0.1.0 baseline;
- `concurrency: once` is local to one `Engine`, not a distributed lock;
- `latest/first` concurrency modes are not production-supported in the frozen v0.1.0 baseline;
- policy `catch:` targets are parsed/validated but not dispatched by the verified runtime path;
- timer triggers do not yet define a complete wall-clock scheduler contract;
- AXM imports are parsed but the public compiler has no multi-file resolver/linker;
- Typed Go Flow effects execute before `FlowStore.Save`;
- automatic execution transition to `Completed` is not a documented runtime guarantee.
