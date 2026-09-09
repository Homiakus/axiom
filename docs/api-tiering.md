# Public API Tiering & Classification Guide

Status: **Canonical Architectural Specification (T-085 / API-004)**  
Scope: All public packages (`axiom`, `adgo`, `model`, `profile`, `table`, `diagram`, `store/pebble`, `store/postgres`).  
Machine-readable inventory: [`docs/api-tiering.json`](api-tiering.json)  
Anti-drift CI test: [`api_tiering_test.go`](../api_tiering_test.go) (`TestAPITieringIntegrity`)

---

## 1. Context and Problem Statement (F-031)

Axiom provides a rich collection of declarative, functional, and distributed workflow primitives. Previously, all exported symbols were presented with equal visibility, leading to high cognitive load for new adopters and making accidental low-level coupling difficult to prevent.

To resolve **F-031**, Axiom establishes a strict 5-tier classification across all 849 exported symbols in the public API manifest. Every symbol belongs to exactly one tier:

| Tier | Category | Target Audience | Compatibility Promise | Count |
|---|---|---|---|---|
| **`stable_facade`** | Stable Facade | Application developers | Guaranteed stable; non-breaking backwards compatibility | 593 |
| **`extension_spi`** | Extension SPI | Infrastructure & platform integrators | Pluggable interfaces with strict semantic conformance | 134 |
| **`advanced`** | Advanced Control | Specialist developers & distributed coordinators | Fine-grained options; backward compatibility preserved | 101 |
| **`internalization_candidate`** | Internalization Candidate | Axiom internal compiler & VM | Deprecation notice before transition to `internal/` | 18 |
| **`deprecated`** | Deprecated | Legacy callers | Scheduled for sunset with immediate drop-in replacement | 3 |

---

## 2. Recommended Entry Profiles (`stable_facade`)

Application developers should almost never assemble low-level stores, engines, and registries manually. Instead, prefer the high-level profiles from `package profile`:

1. **In-process & tests**:
   ```go
   p := profile.NewEmbedded()
   engine, err := p.CompileAndNew(axmSource)
   ```
2. **Single-host durable daemon**:
   ```go
   p, err := profile.OpenDurableSingleNode(profile.DurableSingleNodeConfig{Dir: "./data"})
   engine, err := p.CompileAndNew(axmSource)
   ```
3. **Multi-host distributed production**:
   ```go
   cluster, err := profile.OpenDistributedProduction(plan, registry, profile.DefaultDistributedProductionConfig(nodeID, rootDir))
   ```

For business logic modeling, prefer:
- **`model.New`** + `model.Bind` + `model.Key` for Go-native declarative processes.
- **`axiom.Compile`** + `axiom.Open` for file-based `.axm` declarative pipelines.
- **`axiom.NewFlow`** for small typed reducers and deterministic state machines.

---

## 3. Mapping Low-Level Types to High-Level Features

As required by **T-085** Action 5, the following table maps low-level runtime compiler types and advanced coordinator primitives to the authoritative high-level feature in [`docs/algorithm-integration-matrix.json`](algorithm-integration-matrix.json) that requires them:

| Low-Level Symbol | Tier | High-Level Feature | Justification & Architectural Boundary |
|---|---|---|---|
| `axiom.AtomID` | `internalization_candidate` | `fast_plan_compilation` | Fast bytecode VM slot reference; applications use rule/signal names. |
| `axiom.FieldID` | `internalization_candidate` | `fast_plan_compilation` | Interned state field token; applications use `model.Key`. |
| `axiom.RuleID` | `internalization_candidate` | `fast_plan_compilation` | Internal rule index in the execution dependency graph. |
| `axiom.SignalID` | `internalization_candidate` | `fast_plan_compilation` | Internal signal event dispatch slot. |
| `axiom.ActivityID` | `internalization_candidate` | `fast_plan_compilation` | Fast activity registry pointer. |
| `axiom.Value` / `ValueKind` | `internalization_candidate` | `fast_plan_compilation` | Low-level tagged union runtime value; applications use typed Go values. |
| `axiom.ExecutionState` | `internalization_candidate` | `terminal_drain_checkpoint` | Raw snapshot map representation; applications use `run.State(ctx, &struct)`. |
| `axiom.TRIZNormalization` | `internalization_candidate` | `compiler_graph_validation` | TRIZ contradiction matrix AST normalization source map. |
| `axiom.SourceMapEntry` | `internalization_candidate` | `compiler_graph_validation` | Diagnostic mapping between high-level TRIZ rules and compiled AXM. |
| `axiom.NewMemoryFlowStore` | `internalization_candidate` | `profile_embedded` | Direct constructor; applications should use `profile.NewEmbedded()`. |
| `(Engine).CommitFenced` | `advanced` | `worker_fencing` | Fenced state commit guarded by fencing token and worker lease epoch. |
| `(Engine).Advance` | `advanced` | `worker_fencing` | Low-level single-step coordinator advancement; applications use `Run`. |
| `axiom.RetryScheduledError` | `advanced` | `retry_backoff` | Sentinel failure when an activity attempt is scheduled for backoff sleep. |
| `axiom.ErrRetryScheduled` | `advanced` | `retry_backoff` | Target for `errors.Is` when filtering deferred tasks. |
| `axiom.ExecutionSnapshot` | `advanced` | `terminal_drain_checkpoint` | Full audit representation for external telemetry exporters. |
| `axiom.ExternalActivityClaim` | `advanced` | `worker_fencing` | Lease token passed to decoupled external worker daemons. |
| `axiom.FlowEffect*` | `advanced` | `outbox` | Low-level synchronous outbox intent dispatch and recovery error wrappers. |
| `adgo.AdaptiveRouter` | `advanced` | `adaptive_routing` | EWMA latency and quality-weighted provider dispatch algorithm. |
| `adgo.DependencyRepairPlanner` | `advanced` | `quality_gate_repair` | Automated dependency repair and missing prerequisite synthesizer. |
| `adgo.HedgeExecutor` | `advanced` | `hedged_requests` | Impure-safe speculative execution against backup providers. |
| `adgo.RetentionManager` | `advanced` | `retention_purge` | Purges terminal execution records based on TTL and disk high-watermark. |
| `adgo.ScheduleRunner` | `advanced` | `durable_schedules` | Multi-host cron and interval schedule dispatcher. |

---

## 4. Extension SPI Contracts

Infrastructure integrators implementing custom persistence or coordinator components interact with the `extension_spi` tier:

1. **Storage backends**:
   - `runtime.Store` (`axiom.Store`): Core declarative engine persistence.
   - `adgo.Store`: ADGO workflow engine persistence with CAS and inbox.
   - First-party implementations:
     - `store/pebble`: Embedded zero-network LSM engine for single-host durability.
     - `store/postgres`: Authoritative multi-host PostgreSQL backend with CAS, row leasing, and advisory-locked migrations.
2. **Auxiliary Control-Plane SPIs**:
   - `adgo.ProviderHealthStore`: Shared provider EWMA metrics across hosts.
   - `adgo.ScheduleStore`: Distributed schedule definitions and next fire times.
   - `adgo.AdmissionController`: Cluster-wide concurrency rate limiting.
   - `adgo.ActivityCache`: Content-addressed memoization of deterministic activities.

---

## 5. Deprecation Schedule and Replacements

All pre-v1 deprecated functions carry explicit compiler `// Deprecated:` annotations and mechanical replacements:

| Deprecated Function | Replacement | Target Sunset | Migration Guidance |
|---|---|---|---|
| `axiom.Register(name, fn)` | `axiom.Act` or `axiom.ActTyped` | `v0.2.0` | Replace untyped registration with compile-time type-checked `ActTyped`. |
| `axiom.LoadModule(source)` | `axiom.Compile(source)` | `v0.2.0` | Use canonical top-level `Compile` returning `(*Module, error)`. |
| `axiom.NewEngine(mod, store, acts)` | `axiom.New(mod, WithStore(...), WithActivities(...))` | `v0.2.0` | Use canonical functional options constructor `New`. |

---

## 6. Mechanical Enforcement

Tiering and classification integrity is mechanically enforced on every build:
- **`api_compatibility_test.go`**: Detects any unapproved removals or signature modifications against `testdata/compat/public_api_manifest.txt`.
- **`api_tiering_test.go`**: Validates that 100% of exported symbols are categorized in `docs/api-tiering.json`, have descriptive rationale, and comply with feature mapping requirements.
