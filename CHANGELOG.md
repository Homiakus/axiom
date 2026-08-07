# Changelog

All notable public API, runtime, compatibility, and developer-experience changes are recorded here.

Axiom follows the pre-v1 Semantic Versioning policy described in [`docs/versioning.md`](docs/versioning.md).

## [Unreleased]

### Planned

- durable task-level retry with backoff and `NextAttemptAt` scheduling;
- production-safe `concurrency: latest/first` task supersession;
- runtime dispatch for policy `catch:` mappings;
- stricter validation of unknown `runtime.*` query projection names;
- multi-file AXM import resolution and linking;
- explicit wall-clock timer scheduler contract.

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
- production mode now accepts implemented retry/timeout/once/parallel policies;
- production mode rejects `concurrency: latest/first` with `AX508` until durable supersession semantics exist.

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

- retry is immediate and in-process; durable task-level retry/backoff is not yet implemented;
- `concurrency: once` is local to one `Engine`, not a distributed lock;
- `latest/first` concurrency modes are not production-supported;
- policy `catch:` targets are parsed/validated but not dispatched by the verified runtime path;
- timer triggers do not yet define a complete wall-clock scheduler contract;
- AXM imports are parsed but the public compiler has no multi-file resolver/linker;
- Typed Go Flow effects execute before `FlowStore.Save`;
- automatic execution transition to `Completed` is not a documented runtime guarantee.
