# Axiom Production Stabilization & Architecture Hardening Plan

Status: proposed executable roadmap  
Scope: `axiom` core + `adgo` + storage + CI/CD + performance + security + documentation  
Target branch: `main`  
Baseline audit: current architecture review of Axiom/ADGO, including current nightly/race behavior, durable timer semantics, storage contracts, FileStore locking, Pebble contention, Flow effect durability, typed activity encoding, benchmark gates, compatibility policy, and release governance.

---

## 0. Purpose and execution rules

This plan converts the current audit findings into an implementation sequence that can be executed atomically. Every task below is intentionally small enough to be implemented, reviewed, tested, and reverted independently.

### 0.1 Mandatory rules for every implementation task

For every task marked `A-*`, `B-*`, `C-*`, etc.:

1. Make one logically isolated code change.
2. Add or update tests in the same commit.
3. Run the narrow package tests first.
4. Run repository-wide tests before merge.
5. For concurrency/runtime/storage changes, run the race detector.
6. For hot-path changes, run allocation/benchmark checks.
7. Update docs when behavior or public API changes.
8. Do not weaken an existing invariant to make a test pass.
9. Prefer deterministic tests over sleeps/retries.
10. Every change must preserve backward compatibility unless the task explicitly declares a breaking change.

### 0.2 Global Definition of Done

A stabilization milestone is complete only when all of the following are true:

- `go test ./...` passes on Linux, Windows, and macOS.
- `go test -race ./...` passes on Linux and macOS.
- no time-dependent orchestration test relies on millisecond `time.Sleep` for correctness;
- all store implementations pass one shared contract suite;
- FileStore lock ownership cannot be lost by stale-lock cleanup;
- `main` requires CI and race gates;
- performance checks detect material regressions rather than only catastrophic regressions;
- public durability/transactionality guarantees are explicitly encoded in capabilities;
- serialized-state and plan compatibility rules are documented;
- release/versioning policy exists and is executable;
- current architecture docs match runtime behavior.

---

# Phase A — Deterministic time and timer semantics

Priority: **P0**  
Goal: make durable timers, retries, leases, schedules, retention, routing cooldowns, and tests deterministic.

## A-001 — Inventory all direct wall-clock usage

### Files/packages

Search at minimum:

- `adgo/runtime.go`
- `adgo/engine.go`
- `adgo/schedule.go`
- `adgo/scheduler.go`
- `adgo/policy.go`
- `adgo/admission.go`
- `adgo/router.go`
- `adgo/router_store.go`
- `adgo/repair.go`
- `adgo/retention.go`
- `adgo/store.go`
- `adgo/pebble_store.go`
- `internal/runtime/types.go`
- `internal/runtime/retry_store.go`
- `internal/store/memory/store.go`
- `internal/store/pebble/store.go`
- `internal/store/pebble/transaction.go`

### Change

Create an inventory table in a temporary implementation note or commit description containing every use of:

- `time.Now`
- `time.Sleep`
- `time.NewTimer`
- `time.NewTicker`
- `time.After`
- `time.Since`

Classify each usage as:

- semantic workflow time;
- lease/coordination time;
- observability timestamp only;
- test-only timing;
- storage metadata timestamp.

### Tests

No behavioral change yet.

### Acceptance

No direct wall-clock use in orchestration-critical code remains unclassified.

---

## A-002 — Introduce common clock primitives

### New package

Create:

`internal/durabletime/`

Suggested files:

- `clock.go`
- `system.go`
- `manual.go`
- `timer.go`
- `clock_test.go`

### API

Define a minimal clock contract similar to:

```go
type Clock interface {
    Now() time.Time
    NewTimer(time.Duration) Timer
}

type Timer interface {
    C() <-chan time.Time
    Stop() bool
}
```

Optional only if needed by scheduler code:

```go
type Ticker interface {
    C() <-chan time.Time
    Stop()
}
```

Do not over-generalize the API before real callers require it.

### ManualClock invariants

- starts at explicitly supplied time;
- `Advance` is synchronous;
- timers fire deterministically when the logical deadline is crossed;
- equal-deadline timers fire in stable creation order;
- `Stop` has deterministic semantics;
- advancing time never sleeps;
- safe under concurrent use.

### Tests

Add table/property tests for:

- timer before deadline does not fire;
- timer at exact deadline fires;
- multiple timers ordered deterministically;
- canceled timer never fires;
- concurrent readers of `Now`;
- monotonic `Advance`;
- reject/handle negative advance explicitly.

### Acceptance

`internal/durabletime` is independently race-clean.

---

## A-003 — Inject Clock into ADGO Runtime

### Files

- `adgo/runtime.go`
- `adgo/types.go` if option/config type belongs there
- new `adgo/clock.go` only if a public option is required

### Change

Add clock to Runtime construction/configuration. Preserve current public behavior by defaulting to system clock.

Preferred API:

```go
func WithClock(clock Clock) RuntimeOption
```

If exposing internal clock type publicly is undesirable, define the smallest public interface in `adgo` and adapt internally.

Replace orchestration-semantic `time.Now()` calls with `rt.clock.Now()`.

### Tests

Add tests proving:

- Start timestamps are derived from injected clock;
- wait deadline is deterministic;
- advancing manual clock changes eligibility without sleeping;
- zero/negative duration wait has explicit documented behavior.

### Acceptance

Core runtime wait semantics need no wall-clock sleeps in tests.

---

## A-004 — Rewrite `TestDurableTimerResumes`

### File

- `adgo/runtime_test.go`

### Current problem

The test uses approximately 5 ms wait + 7 ms sleep and flakes under race instrumentation.

### Change

Replace real time with `ManualClock`:

1. start execution;
2. run once;
3. assert `StatusWaiting`;
4. advance logical clock to deadline minus epsilon;
5. run and assert still waiting;
6. advance to exact deadline;
7. run and assert completed.

### Acceptance

Run:

```bash
go test -race ./adgo -run TestDurableTimerResumes -count=100
```

Expected: 100/100 pass.

---

## A-005 — Inject Clock into ADGO Engine

### Files

- `adgo/engine.go`
- `adgo/production.go`
- `adgo/host.go` if engine construction propagates there

### Change

Ensure coordinator, worker leases, heartbeat deadlines, fencing, retries, human timeout state, and durable timer decisions use the configured clock consistently.

Do not use injected semantic clock for raw benchmark elapsed-time measurements unless intended.

### Tests

- worker lease expiration exactly at deadline;
- heartbeat extends lease deterministically;
- zombie worker rejected after logical expiry;
- no early expiry before deadline;
- restart/reload semantics remain equivalent.

### Acceptance

No worker-fencing test depends on `time.Sleep` for correctness.

---

## A-006 — Convert retry/backoff to injected clock

### Files

- `internal/runtime/retry_store.go`
- Axiom runtime constructor/config path

### Change

The existing core `Engine` already has a clock field, but retryStore currently uses direct wall clock. Pass the same clock into retryStore.

Make retry scheduling use exactly one time source.

### Tests

- fixed backoff exact deadline;
- exponential backoff sequence;
- max cap;
- retry not visible before deadline;
- retry visible exactly on deadline;
- engine replacement preserves durable retry deadline.

### Acceptance

No mixed clock domains inside a single Engine.

---

## A-007 — Convert schedules, admission, routing, retention, repair deadlines

### Files

- `adgo/schedule.go`
- `adgo/scheduler.go`
- `adgo/admission.go`
- `adgo/router.go`
- `adgo/router_store.go`
- `adgo/retention.go`
- `adgo/repair.go`

### Change

Replace semantic wall-clock access with shared clock dependency.

### Tests

Add deterministic tests for:

- schedule firing at exact boundary;
- no duplicate firing across restart;
- token refill;
- permit expiry;
- circuit breaker open/half-open timing;
- router cooldown;
- retention cutoff;
- repair max-duration budget.

### Acceptance

A grep for direct wall-clock calls in ADGO runtime-critical code returns only reviewed exceptions.

---

## A-008 — Add time abstraction policy test

### New test

Create an architecture/static guard test or script that flags new direct `time.Now`, `time.Sleep`, `time.After`, `time.NewTimer` calls in restricted packages, with an allowlist for legitimate system-boundary code.

Possible location:

- `internal/archtest/time_test.go`
- or `scripts/check_time_usage.go`

### Acceptance

A future contributor cannot silently reintroduce non-deterministic time into orchestration code.

---

# Phase B — Make full race safety a merge gate

Priority: **P0**

## B-001 — Expand main race job to ADGO

### File

- `.github/workflows/ci.yml`

### Change

Replace the limited critical-package race command with:

```bash
go test -race ./...
```

on Linux.

Keep Windows normal tests but do not require race there if unsupported/too costly.

### Acceptance

Main CI must fail on the current flaky timer implementation and pass after Phase A.

---

## B-002 — Add repeated concurrency suite

### File

- `.github/workflows/nightly.yml`

### Change

Add:

```bash
go test -race ./... -count=3
```

or a targeted `-count=10` for concurrency-heavy packages if runtime cost is too high.

### Packages

At minimum:

- `./adgo`
- `./internal/runtime/...`
- `./internal/store/...`
- `./internal/syncx/...`

### Acceptance

Nightly race is a stress gate, not the first place basic ADGO race compatibility is checked.

---

## B-003 — Add race-specific test classification

### Change

Tag/name concurrency tests consistently:

- `TestRace*`
- `TestLease*`
- `TestConcurrent*`
- `TestFencing*`

Add CI step that can run them independently for fast diagnosis.

### Acceptance

When a race gate fails, logs clearly identify whether failure is data race, timing semantic failure, or assertion failure.

---

# Phase C — FileStore lock correctness

Priority: **P0/P1**

## C-001 — Formalize FileStore lock invariants

### File

Create:

- `adgo/FILESTORE_LOCKING.md`

### Required invariants

1. only lock owner may release the lock;
2. stale recovery cannot delete a lock acquired by a newer owner;
3. a crashed owner can eventually be recovered;
4. two live owners cannot both enter the same execution critical section;
5. context cancellation aborts wait safely;
6. lock identity is unique per acquisition, not only per process;
7. filesystem restart/reopen preserves safe behavior.

### Acceptance

Implementation tasks below are tested against these invariants.

---

## C-002 — Replace anonymous lock file with ownership record

### File

- `adgo/store.go`

### Change

Write structured lock ownership data containing at least:

```text
owner_token
created_at
heartbeat_at
```

Optionally:

```text
pid
hostname
process_start_id
```

Ownership token must be cryptographically/randomly unique enough to avoid accidental reuse.

### Acceptance

Unlock requires matching token.

---

## C-003 — Implement compare-before-release

### File

- `adgo/store.go`

### Change

Before removing the lock file:

1. read current ownership record;
2. verify token equals caller token;
3. remove only if still owned;
4. sync lock directory.

If ownership changed, do not delete.

### Tests

Simulate:

- owner A acquires;
- lock replaced/recovered by B;
- A executes deferred release;
- verify B lock still exists.

### Acceptance

The stale-owner-deletes-new-owner bug is impossible.

---

## C-004 — Add lock heartbeat or renewable lease

### Change

For long critical sections, update heartbeat before stale threshold.

Keep implementation simple. If all critical sections are intentionally short, alternatively redesign stale reclamation to use an ownership generation protocol rather than heartbeat.

### Tests

- long-running live owner is not stolen;
- truly dead/stale owner is recoverable;
- heartbeat failure surfaces explicitly.

---

## C-005 — Add FileStore multi-process adversarial tests

### New tests

Prefer subprocess-based tests rather than goroutines only.

Scenarios:

1. two processes commit same execution;
2. one process dies while holding lock;
3. stale recovery happens;
4. old process cleanup cannot remove new lock;
5. 10+ concurrent committers preserve monotonic versions;
6. inbox event dedup survives contention;
7. context cancellation does not leak lock files.

### Acceptance

Run repeatedly under CI on Linux.

---

## C-006 — Narrow FileStore production claim

### Files

- `ADGO.md`
- `adgo/README.md`
- `adgo/OPERATIONS.md`

### Change

Until multi-process protocol is fully hardened, document FileStore as single-host/shared-filesystem constrained backend rather than generic distributed storage.

Explicitly recommend SQL/KV implementation for multi-host production without shared POSIX-like semantics.

---

# Phase D — Unified Store contract suites

Priority: **P1**

## D-001 — Define Core Store behavioral contract

### New file

- `internal/runtime/store_contract.go` or test-only package contract helper

Document:

- ownership/copy semantics;
- version increment behavior;
- history ordering;
- task ordering;
- idempotency behavior;
- lease behavior;
- error behavior;
- transaction visibility;
- rollback behavior;
- context semantics.

Do not change interface yet.

---

## D-002 — Create Core Store contract test harness

### New test package

Suggested:

- `internal/store/storetest/`

Factory API roughly:

```go
type Factory interface {
    New(t *testing.T) runtime.Store
}
```

If transactional capability exists, run additional tests automatically.

### Required tests

- Create/Get isolation;
- Save isolation;
- returned object mutation does not mutate store;
- input object mutation after save does not mutate store;
- Version monotonicity;
- AppendHistory sequence monotonicity;
- ListHistory deterministic ordering;
- task insert/update isolation;
- lease claim behavior;
- heartbeat ownership;
- expired lease recovery;
- completion/failure terminal state;
- idempotency lookup;
- missing IDs;
- transaction rollback invisibility;
- transaction commit visibility.

---

## D-003 — Run contract suite against Memory Store

### File

- `internal/store/memory/store_test.go`

Add factory hookup to shared contract suite.

### Acceptance

MemoryStore becomes reference semantics.

---

## D-004 — Run contract suite against Pebble Store

### File

- `internal/store/pebble/store_test.go`

Use `t.TempDir()`.

### Acceptance

Every generic Store invariant matches MemoryStore.

---

## D-005 — Add transaction-specific contract tests

### File

- `internal/store/pebble/transaction_test.go`

Test:

- uncommitted writes invisible externally;
- visible inside same tx;
- commit atomically exposes all writes;
- rollback exposes none;
- caller input objects are not mutated;
- repeated `Commit` / `Rollback` behavior is documented;
- sequence allocation remains deterministic.

---

## D-006 — Fix `txStore.SaveExecution` pointer mutation

### File

- `internal/store/pebble/transaction.go`

### Change

Replace direct aliasing:

```go
next := execution
```

with a proper deep clone using same semantics as regular Pebble/Memory stores.

Do the same for any transaction-local method that stores caller-owned pointers.

### Tests

1. Save caller object.
2. Verify caller Version/UpdatedAt unchanged unless contract explicitly says otherwise.
3. Mutate caller object after Save.
4. Verify transaction-local stored state unaffected.

---

## D-007 — Create ADGO Store contract harness

### Existing/new files

- extend `adgo/store_contract_test.go`
- add helper file if needed: `adgo/storetest_test.go`

Run the same logical contract against:

- `MemoryStore`
- `FileStore`
- `PebbleStore`

### Invariants

- Create uniqueness;
- Load copy isolation;
- Commit CAS;
- version monotonicity;
- mutation callback atomicity;
- mutation callback error leaves no state change;
- inbox dedup;
- inbox ordering;
- AckInbox idempotency;
- restart durability where applicable;
- version history capabilities where applicable.

---

# Phase E — Deterministic task and event ordering

Priority: **P1**

## E-001 — Define canonical task ordering

### Design

Choose one canonical tuple, preferably:

```text
TaskSequence → TaskID
```

Avoid wall-clock timestamp as primary ordering key when a deterministic sequence exists.

Document invariant.

---

## E-002 — Remove map-order dependence in Pebble transaction task lists

### File

- `internal/store/pebble/transaction.go`

### Change

Sort `ListTasks` output canonically before return.

### Tests

Insert identical task set in randomized order 100+ times and assert byte/logical equality of resulting task order.

---

## E-003 — Make `nextPendingTask` deterministic

### File

- `internal/store/pebble/transaction.go`

### Change

Do not select first eligible item from a Go map. Select minimum by canonical ordering.

### Tests

Randomized insertion order must produce the same selected task.

---

## E-004 — Audit ADGO map iteration that can affect execution decisions

### Packages

Search all ADGO code for `range` over maps where iteration affects:

- winner selection;
- scheduling;
- repair roots;
- provider routing tie-break;
- child creation;
- diagnostics order;
- serialization/digest input.

### Change

Sort keys or use explicit deterministic tie-breaks.

### Tests

Add randomized construction-order property tests.

---

# Phase F — Durable effect semantics for `Flow`

Priority: **P1**

## F-001 — Document current Flow effect guarantee precisely

### Files

- `README.md`
- package docs around `Flow`

State explicitly:

- reducer state is saved after effects;
- external effect may succeed even if state save fails afterward;
- therefore Flow effects require idempotent handlers or an external dedup strategy;
- use ADGO for stronger durable external-effect orchestration.

---

## F-002 — Add crash-window regression test

### File

- `flow_test.go`

Create a test store that intentionally fails after the effect handler succeeds but before state persistence.

Prove current behavior explicitly.

This test is initially documentation-as-test, not yet a fix.

---

## F-003 — Design `DurableFlow` / outbox capability

### Design document

Create:

- `docs/DURABLE_FLOW_DESIGN.md`

Evaluate two designs:

### Option 1

`DurableFlowStore` atomically stores:

- new state;
- event history;
- pending effect commands.

A worker executes pending effects later.

### Option 2

Reuse a low-level durable task primitive shared with Axiom runtime/ADGO.

Prefer Option 2 only after durable-kernel extraction makes reuse clean.

### Required properties

- at-least-once effect delivery;
- deterministic idempotency key;
- no external effect before durable intent commit;
- replay does not re-run completed effects;
- recovery resumes pending effects.

---

## F-004 — Implement durable outbox capability without breaking existing Flow

### Rule

Existing `Flow` remains backward compatible.

Add a new opt-in API, e.g.:

```go
OpenDurableFlow(...)
```

or an explicit store capability that switches semantics only when configured.

### Tests

Crash injection at each boundary:

1. before state commit;
2. after state commit, before effect;
3. during effect;
4. after effect, before acknowledgement;
5. after acknowledgement.

Verify no lost effect and idempotent redelivery behavior.

---

# Phase G — Extract a shared Durable Kernel

Priority: **P1/P2**  
Goal: remove duplicated critical semantics while preserving distinct Axiom and ADGO execution models.

## G-001 — Inventory duplicated primitives

Compare Axiom Core and ADGO implementations of:

- clock/time;
- version/CAS;
- lease;
- worker identity;
- fencing;
- retry/backoff;
- deterministic IDs;
- transaction capability;
- durable event/task state;
- codec/copy semantics.

Produce a duplication map before moving code.

---

## G-002 — Create `internal/durable` package

Suggested initial files:

- `version.go`
- `lease.go`
- `retry.go`
- `identity.go`
- `capability.go`

Do not move high-level Execution structs into this package.

### Principle

`internal/durable` contains primitives, not workflow semantics.

---

## G-003 — Move common retry/backoff math

Extract pure deterministic retry calculation from:

- Axiom runtime;
- ADGO policy/retry logic where compatible.

### Tests

Property tests:

- delay monotonic until cap;
- never negative;
- respects max;
- deterministic for same inputs;
- overflow safe.

---

## G-004 — Move lease expiry/fencing predicates

Centralize pure functions such as:

```go
Expired(now, leaseUntil)
CanCommit(worker, attempt, lease, now)
```

Keep store mutation in each runtime.

### Tests

Boundary table at `<`, `==`, `>` deadline.

---

## G-005 — Consolidate deterministic identity framing

Use one canonical framing/hash strategy for durable IDs where semantics are shared.

Do not change persisted ID format silently. If format must change, introduce versioning/migration first.

---

## G-006 — Add architecture dependency test

Prevent `internal/durable` from importing high-level runtime/ADGO packages.

Desired dependency direction:

```text
internal/durable <- Axiom runtime
internal/durable <- ADGO
```

never reverse.

---

# Phase H — Pebble contention and scalability

Priority: **P2**

## H-001 — Establish contention baseline first

### Benchmark

Add benchmark scenarios:

- 1 execution, N goroutines;
- N executions, N goroutines;
- read-heavy;
- commit-heavy;
- inbox-heavy;
- transaction-heavy.

Collect:

- ops/s;
- p50/p95/p99;
- alloc/op;
- mutex profile if possible.

Do not optimize without baseline.

---

## H-002 — Add mutex/block profiling benchmark mode

### Tool

Extend `cmd/axiombench` or add benchmark flags enabling Go mutex/block profiles.

### Acceptance

Demonstrate percentage of time waiting on store-wide mutex.

---

## H-003 — Introduce per-execution striped locks in ADGO PebbleStore

### File

- `adgo/pebble_store.go`

### Change

Replace global mutation serialization for independent execution IDs with fixed striped locks or keyed locks.

Keep DB-wide operations protected separately only where necessary.

### Tests

- same execution remains serialized;
- different executions proceed concurrently;
- CAS conflict remains correct;
- no races.

---

## H-004 — Separate catalog/inbox lock domains

Avoid blocking unrelated execution commits on catalog scans or inbox work where Pebble itself provides safe concurrent access.

Benchmark before/after.

---

## H-005 — Revisit Core Pebble transaction lock scope

### Files

- `internal/store/pebble/store.go`
- `internal/store/pebble/transaction.go`

### Goal

Reduce duration of the store-wide lock without weakening atomicity.

Possible path:

- execution-scoped lock;
- batch-local reads/snapshots;
- optimistic version validation at commit;
- deterministic conflict handling.

### Requirement

Do not implement this until D-phase contract tests are complete.

---

# Phase I — Typed activity path optimization

Priority: **P2**

## I-001 — Benchmark `Act` vs `ActTyped`

### New benchmark

Measure identical payload using:

- dynamic `Act`;
- `ActTyped` struct input/output;
- named map input/output.

Capture latency and allocations.

---

## I-002 — Extract typed codec registration plan

### Files

- `axiom.go` initially;
- preferably new `typed_activity.go` after behavior is frozen.

### Change

Move shape validation and output field plan computation to registration time.

Cache:

- exported fields;
- resolved output names;
- skip rules;
- field indexes.

Do not rediscover schema on every invocation.

---

## I-003 — Align typed output semantics with documented JSON semantics

Explicitly define behavior for:

- `json:"name"`;
- `json:"-"`;
- `omitempty`;
- embedded structs;
- pointers;
- custom marshalers;
- named map types.

Either fully support a subset and reject unsupported constructs at registration, or use generated codecs.

Never silently produce semantics different from documentation.

---

## I-004 — Generate zero/low-allocation adapters in `axiomgen`

### Package

- `cmd/axiomgen/internal/codegen`

### Goal

Generate typed wrappers for known module activity shapes.

Generated adapters should avoid repeated generic JSON round-trips where practical.

### Tests

- generated code compiles;
- generated adapter behavior equals dynamic adapter;
- tags preserved;
- error handling equivalent;
- benchmark shows measurable benefit.

---

# Phase J — Performance regression engineering

Priority: **P2**

## J-001 — Replace broad allocation ceilings with baselines + budget

### Files

- `allocation_test.go`
- benchmark tooling/CI

Current tests should remain as hard safety ceilings, but add tighter baseline regression detection.

Example policy:

```text
hard absolute ceiling
AND
relative regression <= 15%
```

Use stable operations only; avoid flaky microbench thresholds in normal unit tests.

---

## J-002 — Persist benchmark artifact schema

### `cmd/axiombench`

Ensure JSON includes:

- commit SHA;
- Go version;
- OS/arch;
- operation name;
- sample count;
- ops/s;
- p50/p90/p95/p99;
- alloc/op where available;
- bytes/op;
- configuration parameters.

Version the artifact schema.

---

## J-003 — Add benchmark comparison CI

Store a reviewed baseline file, for example:

- `benchmarks/baseline.json`

CI should fail only on material regressions, with explicit tolerances per benchmark category.

### Rule

Do not automatically rewrite baseline on every run.

Baseline updates require an intentional commit explaining why regression/improvement is accepted.

---

## J-004 — Add ADGO benchmarks

Currently performance focus is stronger in Axiom Core than ADGO.

Add scenarios for:

- simple graph completion;
- parallel super-step;
- durable timer handling;
- router selection;
- repair planning;
- MemoryStore commit;
- PebbleStore commit;
- inbox delivery;
- worker claim/complete;
- 1k independent executions.

---

# Phase K — Store capability and durability model

Priority: **P2**

## K-001 — Separate transactionality from durability conceptually

### Problem

`TransactionalStore` only proves transaction API presence, not persistence level.

### Design

Introduce explicit durability metadata/capability.

Possible API:

```go
type DurabilityLevel uint8
const (
    DurabilityEphemeral DurabilityLevel = iota
    DurabilityProcess
    DurabilityHost
    DurabilityDistributed
)
```

Do not require exact names if a cleaner model emerges.

---

## K-002 — Add capability inspection without breaking Store

Prefer optional interfaces:

```go
type DurabilityProvider interface {
    Durability() DurabilityLevel
}
```

rather than bloating base `Store`.

### Defaults

- Memory: ephemeral;
- Pebble: host durable;
- File: host/shared-filesystem durable with documented constraints;
- custom stores: explicit or conservative default.

---

## K-003 — Strengthen `WithProductionMode`

### File

- `axiom.go`

### Change

Production mode must validate both:

- required transaction semantics;
- required durability capability.

If backward compatibility forbids immediate enforcement, introduce warning/strict production option first, then enforce in next breaking version.

---

## K-004 — Add production configuration diagnostics

Return structured diagnostics identifying:

- missing transaction support;
- ephemeral durability;
- disabled sync/WAL risk;
- unsupported concurrency mode;
- non-idempotent external activity where detectable.

---

# Phase L — Public API and compatibility cleanup

Priority: **P2**

## L-001 — Audit deprecated API fallbacks

### File

- `axiom.go`

Focus on `NewEngine` fallback path that bypasses modern validation.

Document all deprecated functions and whether they preserve modern invariants.

---

## L-002 — Stop silent validation bypass

Choose staged path:

### Current minor release

- keep deprecated API;
- emit/return diagnostics where possible;
- document unsafe compatibility fallback.

### Next major

- remove fallback;
- deprecated constructor delegates strictly to validated `New`.

Add migration docs.

---

## L-003 — Freeze public API inventory

Generate/check a public API manifest using Go tooling (`go doc`, `go list`, or an API checker).

CI detects accidental exported API changes.

Intentional changes require manifest update.

---

# Phase M — Serialization and compatibility specification

Priority: **P1/P2**

## M-001 — Inventory persisted schemas

Document all durable representations:

- Axiom `Execution`;
- `ExecutionState`;
- `ActivityTask`;
- history entries;
- ADGO `Execution`;
- inbox events;
- plan digest/version;
- schedule state;
- router health state;
- cache metadata;
- artifact metadata.

---

## M-002 — Add explicit schema/version fields where missing

Do not depend exclusively on Go struct layout evolving compatibly.

Add schema version metadata at durable boundaries where necessary.

---

## M-003 — Add golden compatibility fixtures

### New directory

- `testdata/compat/`

Store representative serialized states from released schema versions.

Tests must verify new code can:

- load supported old state;
- reject unsupported state with clear diagnostic;
- preserve integer precision;
- preserve plan identity;
- preserve task/history semantics.

---

## M-004 — Define PlanDigest compatibility rules

Document exactly what changes affect digest and migration compatibility:

- node addition/removal;
- activity rename;
- dependency changes;
- policy changes;
- risk flags;
- idempotency expressions;
- output schema changes.

Add tests for stable digest on semantically identical definitions.

---

# Phase N — Security and supply-chain hardening

Priority: **P2**

## N-001 — Pin security tool versions

### File

- `.github/workflows/security.yml`

Replace `@latest` executions for:

- govulncheck;
- gosec;

with reviewed pinned versions.

Use Dependabot/Renovate or periodic explicit updates.

---

## N-002 — Reduce global gosec exclusions

Audit each excluded rule:

`G101,G104,G115,G301,G302,G304,G306,G404,G703`

For each:

1. find actual findings;
2. fix legitimate issues;
3. use local `#nosec` with explanation only where justified;
4. remove rule from global exclude list when feasible.

### Acceptance

Every remaining global exclusion has a documented reason.

---

## N-003 — Pin GitHub Actions by commit SHA for release/security-critical workflows

At minimum:

- checkout;
- setup-go;
- artifact upload;
- gitleaks action;
- release actions.

Optionally keep comments indicating upstream tag for readability.

---

## N-004 — Add SBOM generation

Generate CycloneDX or SPDX for releases.

Attach to release artifacts.

---

## N-005 — Add provenance/checksum release artifacts

Produce:

- checksums;
- SBOM;
- build metadata;
- signed provenance if practical.

---

# Phase O — Branch protection and repository governance

Priority: **P0 process**

## O-001 — Protect `main`

Configure GitHub ruleset / branch protection:

- require pull request;
- require `ci-gate`;
- require security workflow or selected security checks;
- require full race gate;
- block force push;
- block deletion;
- require resolved review conversations;
- require branch to be up to date when appropriate.

If direct pushes are intentionally retained temporarily, at minimum require status checks for automated merges.

---

## O-002 — Add CODEOWNERS for critical runtime areas

Suggested critical paths:

- `/internal/runtime/`
- `/internal/store/`
- `/adgo/`
- `/.github/workflows/`

Use actual maintainers available to repository.

---

## O-003 — Add PR checklist for invariant-sensitive changes

Require author to state:

- public API change?;
- durable schema change?;
- concurrency change?;
- timing change?;
- storage semantics change?;
- benchmark impact?;
- migration impact?;
- docs updated?;
- race test run?;
- compatibility fixtures updated?

---

# Phase P — Documentation correctness and executable docs

Priority: **P2**

## P-001 — Fix known documentation drift

Audit at minimum:

- production concurrency modes (`parallel/once/latest/first`);
- Pebble default codec behavior;
- production store requirements;
- FileStore deployment constraints;
- race/CI guarantees;
- ADGO timer semantics.

---

## P-002 — Make examples compile/run in CI as documentation tests

Existing examples suite is a good foundation. Extend it so every code block used as a canonical quick-start has a corresponding runnable source file or test.

Avoid untested copy-paste snippets that can drift.

---

## P-003 — Generate current audit/performance report from CI artifacts

Do not maintain performance claims manually.

Add generated report structure:

```text
reports/current/
  test-summary.md
  benchmark-summary.md
  allocations.json
  race-summary.md
  fuzz-summary.md
  security-summary.md
```

Prefer generated files in release artifacts if committing them causes noise.

---

# Phase Q — Release engineering and compatibility policy

Priority: **P1/P2**

## Q-001 — Define SemVer policy while project is pre-1.0

Create:

- `docs/VERSIONING.md`

Define what constitutes breaking change for:

- Go API;
- AXM syntax;
- TRIZ frontend;
- model/table API;
- ADGO Plan definition;
- serialized state;
- worker protocol;
- callbacks;
- store capability contracts.

---

## Q-002 — Define supported persistence compatibility window

Example policy:

- current schema N reads N and N-1;
- older schemas require migration tool;
- major format incompatibility fails fast with explicit code.

Choose actual policy deliberately; do not leave implicit.

---

## Q-003 — Create first reproducible release workflow

Release should run only after:

- full CI;
- full race;
- security;
- compatibility fixtures;
- codegen verification;
- benchmark gate.

Artifacts:

- source tag;
- checksums;
- SBOM;
- generated documentation/report;
- changelog.

---

# Phase R — Advanced correctness testing

Priority: **P1**

## R-001 — Replay equivalence property tests

Generate legal event sequences and verify:

```text
live execution final state == replayed final state
```

for deterministic components.

Include:

- patches;
- signals;
- retries;
- completions;
- human decisions;
- repair revisions where supported.

---

## R-002 — Crash-point injection framework

Create a test-only failpoint abstraction capable of simulating crash/error at durable boundaries.

Important crash points:

- before commit;
- after batch construction;
- after state commit;
- before/after inbox ack;
- before task claim;
- after claim before activity;
- after activity before completion commit;
- compensation step boundaries;
- schedule firing before cursor commit.

### Rule

Failpoints must not ship enabled in normal builds unless zero-cost and explicitly controlled.

---

## R-003 — Crash equivalence tests

For each injected crash point:

1. run until injected failure;
2. reopen durable store/runtime;
3. resume;
4. compare final state to uninterrupted execution;
5. assert allowed duplicate external calls only where contract says at-least-once;
6. assert stale workers cannot commit.

---

## R-004 — Store backend equivalence tests

Run same deterministic workflow corpus against:

- MemoryStore;
- FileStore;
- PebbleStore.

Compare logical final execution state and history semantics modulo backend-specific metadata.

---

## R-005 — Retry monotonicity properties

Property tests:

- attempt never decreases;
- next retry deadline never moves backward unless explicitly rescheduled;
- attempts never exceed MaxAttempts;
- terminal failure cannot become pending without explicit repair/reset semantics;
- completed task cannot be retried.

---

## R-006 — Lease/fencing safety properties

Generate worker IDs, attempts, lease renewals, expirations, and delayed completions.

Invariant:

> no stale worker can commit after ownership has transferred.

Run under race detector.

---

## R-007 — Budget monotonicity across all execution paths

Expand existing ADGO budget tests to include:

- retry;
- hedging;
- ensemble;
- repair;
- child workflow;
- compensation;
- failure;
- cache hit/miss.

No path may reduce already-accounted usage.

---

## R-008 — Repair locality property tests

Generate DAGs with independent branches.

After targeted repair:

- descendants of repair root may be invalidated;
- unrelated completed branches must remain intact;
- revision increment is deterministic;
- no unrelated external effect is repeated.

---

## R-009 — Migration safety property tests

Generate compatible/incompatible plan mutations.

Verify migration accepts only changes covered by compatibility rules and only at permitted quiescent points.

---

## R-010 — Terminal-state non-resurrection tests

For every terminal state:

- completed;
- failed;
- canceled;
- superseded where applicable;

feed delayed/duplicate events and verify state cannot silently become active again unless an explicit API (repair/fork/retry) authorizes it.

---

# Phase S — Observability and operations hardening

Priority: **P2**

## S-001 — Define stable metrics contract

Metrics should include:

- active executions;
- waiting executions;
- human-blocked executions;
- pending/running tasks;
- lease expirations;
- stale worker commits rejected;
- retry counts;
- retry exhaustion;
- repair attempts;
- compensation attempts/failures;
- provider circuit state;
- admission rejection;
- store latency;
- coordinator loop latency.

Avoid unbounded labels such as raw execution ID.

---

## S-002 — Add structured event/log schema

Define stable fields:

```text
execution_id
plan_digest
node_id
task_id
worker_id
attempt
revision
event_type
error_code
```

Ensure sensitive payloads are not logged by default.

---

## S-003 — Add health/readiness distinction

Production service helpers should distinguish:

- process alive;
- store reachable;
- coordinator operational;
- worker drain state;
- schema compatible;
- plan registry loaded.

---

## S-004 — Expand operations runbook

### File

- `adgo/OPERATIONS.md`

Add procedures for:

- corrupted execution diagnostics;
- stuck lease;
- worker quarantine;
- Pebble recovery/open failure;
- FileStore stale lock recovery;
- failed migration;
- repair exhaustion;
- human escalation backlog;
- retention failure;
- rollback to previous plan version.

---

# Phase T — Codebase structure and maintainability

Priority: **P2/P3**

## T-001 — Split oversized public files by responsibility

Candidate: root `axiom.go`.

Target files may become:

- `app.go`
- `compile.go`
- `engine_options.go`
- `activity.go`
- `store_options.go`
- `compat.go`

Preserve package API exactly.

Do this only after stabilization tests exist to make refactor low-risk.

---

## T-002 — Separate ADGO runtime, persistence, coordination, and policy layers physically

Without changing package name immediately, group implementation by clear files/subpackages where circularity allows.

Desired conceptual modules:

```text
plan/compiler
runtime state machine
coordinator
worker
storage
policy
routing
repair
scheduling
operations
```

Avoid creating many tiny packages solely for aesthetics.

---

## T-003 — Add architecture tests for dependency boundaries

Prevent:

- compiler depending on runtime;
- durable primitives depending on high-level orchestration;
- storage importing application-facing helpers;
- examples becoming dependencies of library code.

---

## T-004 — Standardize error taxonomy

Audit Axiom `AXxxx` diagnostics and ADGO errors.

Define categories:

- compile/config;
- runtime deterministic;
- transient/retryable;
- storage;
- concurrency/conflict;
- stale/fencing;
- policy;
- human required;
- compatibility/migration.

Expose machine-readable classification rather than string matching where possible.

---

## T-005 — Remove retry classification by string inspection

### Current area

- `internal/runtime/retry_store.go`

Replace logic based on checking whether error text contains `AX505` with typed/structured errors or `errors.Is`/`errors.As` classification.

### Tests

- wrapped retryable error remains retryable;
- unrelated text containing `AX505` is not accidentally retryable;
- terminal errors never retry;
- context cancellation never retry loops.

---

# Phase U — Concrete CI target topology

After stabilization, target CI DAG should be approximately:

```text
lint
module-hygiene
api-compat
unit-linux
unit-windows
unit-macos
race-full-linux
fuzz-smoke
store-contracts
compat-fixtures
codegen-verify
examples
security-fast
benchmark-regression
        ↓
      ci-gate
```

Nightly:

```text
race-full-linux-count3
race-full-macos
fuzz-60s-or-longer
crash-injection-suite
multi-process-filestore
stress-many-executions
benchmark-full
security-full
```

Release:

```text
ci-gate
security-full
compatibility
benchmark-gate
SBOM
checksums
provenance
release
```

---

# Phase V — Recommended commit sequence

The safest implementation order is:

1. `A-001` time inventory.
2. `A-002` durabletime package.
3. `A-003` ADGO Runtime clock injection.
4. `A-004` deterministic durable timer test.
5. `A-005..A-008` complete time migration + guard.
6. `B-001` enable full race merge gate.
7. `B-002..B-003` nightly repeated race diagnostics.
8. `D-001..D-005` store contract suite before storage refactors.
9. `D-006` tx pointer isolation fix.
10. `E-001..E-004` deterministic ordering.
11. `C-001..C-006` FileStore lock protocol + adversarial tests.
12. `R-002..R-006` crash/lease property framework.
13. `O-001..O-003` repository governance.
14. `M-001..M-004` persistence compatibility.
15. `Q-001..Q-003` release/versioning policy.
16. `F-001..F-004` durable Flow effects.
17. `G-001..G-006` shared durable kernel extraction.
18. `H-001..H-005` Pebble contention optimization.
19. `I-001..I-004` typed activity optimization.
20. `J-001..J-004` stricter performance regression gates.
21. `K-001..K-004` durability capability model.
22. `L-001..L-003` public API cleanup.
23. `N-001..N-005` supply-chain hardening.
24. `P-001..P-003` executable docs/current reports.
25. `S-001..S-004` operations/observability hardening.
26. `T-001..T-005` structural cleanup after invariants are protected.
27. `R-007..R-010` final broad property/migration invariants.

Do not start Pebble concurrency refactors or Durable Kernel movement before shared store/time contracts are green.

---

# Phase W — Milestones and exit criteria

## Milestone M1 — Race-clean deterministic runtime

Contains:

- Phase A complete;
- Phase B complete.

Exit:

- `go test -race ./...` green on every main change;
- no flaky durable timer tests across repeated runs.

## Milestone M2 — Storage semantics locked down

Contains:

- Phase C;
- Phase D;
- Phase E.

Exit:

- all stores pass contract tests;
- FileStore stale-lock ownership bug fixed;
- deterministic task ordering proven.

## Milestone M3 — Durable correctness

Contains:

- core of Phase R;
- Phase F;
- Phase M.

Exit:

- crash/replay/backend equivalence tests green;
- Flow durable effect path available;
- persisted schema compatibility specified.

## Milestone M4 — Production governance

Contains:

- Phase O;
- Phase N;
- Phase Q.

Exit:

- protected main;
- reproducible release pipeline;
- security tools pinned;
- first versioned release can be created safely.

## Milestone M5 — Performance and architecture consolidation

Contains:

- Phase G;
- Phase H;
- Phase I;
- Phase J;
- Phase K;
- Phase T.

Exit:

- shared durable primitives extracted;
- independent execution throughput scales better than global mutex baseline;
- typed activity overhead materially reduced;
- benchmark regression gates enforce reviewed budgets.

---

# Phase X — Test matrix required before declaring production-ready

## Functional

- parser/compiler success/error corpus;
- AXM/TRIZ/model/table frontends;
- Core Engine lifecycle;
- Flow lifecycle;
- ADGO runtime/engine/host/production topology.

## Concurrency

- same execution serialization;
- independent execution parallelism;
- CAS conflicts;
- worker fencing;
- lease expiry/heartbeat;
- duplicate events;
- duplicate callbacks;
- concurrent schedule tick.

## Durability

- store reopen;
- process crash simulation;
- inbox redelivery;
- retry persistence;
- compensation persistence;
- timer persistence;
- migration/fork/continue-as-new.

## Determinism

- replay equivalence;
- randomized insertion order;
- stable digest;
- stable task ordering;
- manual-clock exact-boundary behavior.

## Performance

- allocations;
- latency percentiles;
- throughput;
- mutex contention;
- large execution count;
- long history;
- large DAG;
- parallel super-step.

## Security

- secret scan;
- govulncheck;
- gosec;
- unsafe path/file identity tests;
- callback authenticity/authz where applicable;
- worker protocol auth/fencing;
- dependency/SBOM checks.

---

# Phase Y — Production readiness scorecard

Maintain a generated or reviewed scorecard with hard evidence, not subjective claims:

| Area | Required evidence |
|---|---|
| Correctness | full unit + property suite green |
| Concurrency | full race + repeated concurrency suite green |
| Durability | crash/reopen equivalence suite green |
| Determinism | replay/order/time properties green |
| Storage | contract suite on every backend |
| Performance | benchmark regression gate green |
| Security | pinned scanners green, SBOM generated |
| Compatibility | persisted fixture suite green |
| Operations | runbook + diagnostics + health/readiness |
| Governance | protected main + release gate |

Axiom/ADGO should only be described as production-ready when all rows have current CI evidence.

---

# Phase Z — Final architectural target

Long-term structure should converge toward:

```text
Declarative frontends
  model / AXM / TRIZ / table / codegen
                |
         immutable plans/IR
                |
      +---------+---------+
      |                   |
Axiom object runtime   ADGO graph runtime
      |                   |
      +---------+---------+
                |
        shared durable kernel
  clock / version / CAS / lease / retry /
  fencing / deterministic identity / capabilities
                |
            Storage SPI
     Memory / Pebble / File / SQL-KV
```

The purpose is **not** to merge Axiom Runtime and ADGO into one giant engine. They model different problems. The purpose is to ensure both use the same rigorously tested low-level durability semantics.

---

# Immediate first implementation batch

After this plan lands, implementation should begin with exactly this batch:

1. `A-002` create deterministic clock package.
2. `A-003` inject it into ADGO Runtime.
3. `A-004` eliminate `TestDurableTimerResumes` wall-clock flake.
4. run `go test ./...`.
5. run `go test -race ./...`.
6. only when both are green, merge `B-001` making full race a mandatory main gate.
7. then build Store contract tests before touching FileStore/Pebble semantics.

This order minimizes simultaneous behavioral change and ensures each later refactor is protected by stronger invariants than the code has today.
