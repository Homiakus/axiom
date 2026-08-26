# Axiom Production Stabilization & Architecture Hardening Plan

Status: **active executable roadmap — re-audited 2026-08-26**  
Scope: `axiom` core + `adgo` + Flow + storage + CI/CD + performance + security + compatibility + operations  
Target branch: `main`  
Audit baseline: `ee0b2910db2d89cfbe3c9b2aee7f4c626082189b`  
Supersedes: the original production stabilization roadmap created at `97d490f90679bf277451c579f8a6ae12658c5529`.

This document is the single execution plan for post-audit stabilization. It deliberately separates already-completed work from remaining work so future agents do not repeat migrations, weaken new invariants, or optimize code before correctness gates are green.

---

# 0. Status legend and mandatory execution protocol

## Status legend

- **DONE** — implemented and backed by tests/CI evidence.
- **PARTIAL** — useful implementation exists, but the production invariant is not fully closed.
- **TODO** — not implemented or not verified.
- **P0 BLOCKER** — must be resolved before feature/performance work continues.
- **EXTERNAL** — repository setting or external operation that cannot be completed by a normal source-code commit alone.

## Mandatory protocol for every task

1. Fetch the latest `main` HEAD before making any change.
2. If `main` CI is red, work only on the root cause of the red gate or on documentation describing that blocker. Do not start unrelated feature work.
3. One atomic task = one logical commit. Tests belong in the same commit whenever practical.
4. Run the narrowest relevant tests first.
5. For runtime/storage/concurrency changes, run the package under `-race`, then `go test -race ./...`.
6. For cross-platform behavior, verify Linux, Windows, and macOS. Never hide a portability bug with an unconditional OS skip merely to make CI green.
7. For hot-path changes, capture before/after benchmark and allocation measurements.
8. For persisted-state changes, add compatibility fixtures and reopen/migration tests before changing write format.
9. For public API changes, add compatibility tests and migration notes before changing behavior.
10. Never weaken an existing invariant, timeout, race gate, security rule, or test to make a task pass.
11. Prefer deterministic clocks/failpoints over millisecond sleeps.
12. Inspect the GitHub Actions result after each push. If a job fails, read the exact failing job log before the next commit.
13. Update this plan when a task changes status. Record the implementing commit SHA beside the task or in the progress log.
14. Direct pushes to `main` are currently possible because branch protection is disabled. After governance hardening requires pull requests, follow the protected-branch workflow instead of bypassing it.

## Global Definition of Done

Axiom is considered production-hardened for the scope of this plan only when all of the following are true:

- `go test ./...` passes on Linux, Windows, and macOS;
- `go test -race ./...` passes on Linux and repeated nightly race runs remain green;
- `main` is protected by required checks and cannot silently accept a red commit;
- semantic orchestration time comes from explicit clock dependencies rather than mixed wall/logical time;
- all production stores have explicit behavioral, durability, ordering, context, and persisted-format contracts;
- FileStore/admission locking has portable ownership/fencing tests, including subprocess recovery where applicable;
- durable Flow effects have crash-boundary/failpoint coverage and an operationally inspectable outbox;
- persisted formats have explicit schema/version handling and golden backward-compatibility fixtures;
- public API compatibility is mechanically checked;
- material performance regressions are detected relative to a reviewed baseline;
- global security suppressions are eliminated or reduced to documented local exceptions;
- release tooling and release documentation describe the same frozen-SHA process;
- at least one reproducible GitHub Release is published through the hardened release pipeline;
- operations documentation covers corruption, stale leases, lock recovery, outbox backlog, migration, rollback, and degraded readiness.

---

# 1. Re-audit result at baseline `ee0b291`

## 1.1 Current gate state

| Area | State | Re-audit finding |
|---|---|---|
| Linux unit tests | DONE | Current baseline passes. |
| macOS unit tests | DONE | Current baseline passes. |
| Windows unit tests | **P0 BLOCKER** | `TestFileAdmissionLockCleanupDoesNotDeleteReplacementOwner` fails because Windows refuses to unlink a lock file while its handle is open. |
| Full race gate | DONE | `go test -race -v ./...` is in main CI and passed on the audited baseline. |
| Nightly repeated race | DONE | Nightly runs `go test -race ./... -count=3` on Linux/macOS. |
| Module hygiene | DONE | `go mod tidy` + diff gate exists. |
| Codegen runtime verification | DONE | Generated wrapper is compiled/run inside the repository module without nested dependency resolution. |
| Security workflow | PARTIAL | Pinned tools/actions and scans are present, but gosec still excludes a broad global rule set. |
| Branch protection | **EXTERNAL / P0 process risk** | `main` is not protected and has no required status checks. |
| Core store contracts | DONE | Shared Memory/Pebble direct/transaction contract suite exists and passes. |
| ADGO Store contract | PARTIAL | Rich backend tests exist, but no single reusable behavioral conformance harness covers Memory/File/Pebble consistently. |
| Core Pebble persisted identity | DONE | Schema+codec marker, legacy adoption and mismatch/future/partial-marker tests exist. |
| ADGO Pebble persisted identity | TODO | JSON records are durable, but no equivalent explicit schema/version marker is opened/validated. |
| FileStore lock ownership | DONE/PARTIAL | Owner token, heartbeat and ownership-aware release exist; admission regression test exposed a Windows portability gap in the test/takeover model. |
| Durable Flow outbox | DONE/PARTIAL | Transactional state+outbox and synchronous Pebble Flow store exist; broader failpoint/operational/backpressure work remains. |
| Durability capability | DONE | Production mode distinguishes synchronous durability and rejects NoSync/buffered modes. |
| Deterministic time | PARTIAL | Durable timers/retry deadlines use injected clocks, but multiple ADGO decisions and retry waiting still mix wall and semantic time. |
| Deterministic task order | DONE | Canonical transactional task ordering tests exist. |
| `ActTyped` performance | TODO | Input still uses `json.Marshal -> json.Unmarshal` per call and output reflection runs per invocation. |
| Pebble transaction scalability | TODO | A global Store mutex is held from `BeginTransaction` through `Commit/Rollback`. |
| Error taxonomy | PARTIAL | Typed retry marker exists, but retryability still recognizes `AX505` via string inspection. |
| Release workflow | PARTIAL | Cross-build, SBOM and checksums exist; frozen-release-branch policy and workflow behavior disagree. |
| Published releases | TODO | No GitHub Release exists at the audit baseline. |
| Public API compatibility automation | TODO | No API manifest/apidiff-style merge gate was found. |
| CODEOWNERS | TODO | No active CODEOWNERS file was found. |

## 1.2 Completed stabilization work that must not be reimplemented

The following work from the original roadmap is already present and should be treated as an invariant:

- deterministic durable clock primitives: `e09fe698909108df3784755e23ecd58342caa3b8`;
- ADGO runtime clock option and durable timer migration: `da26e8a...`, `139331b...`, `0ca7d03...`;
- core retry clock unification: `4c7674d875a1adf65d01bba00c57a4b169071eca`;
- full repository race gate: `58fadee49072f008e405b8ea316b594c658d1e90`;
- repeated nightly race: `e2cc370d2371fb7ee2de9a9b0832dcc932378174`;
- Pebble transactional ownership/copy fixes and deterministic task view: `1ba9b2c...`, `d2bebec...`;
- shared core Store contract suite: `3677213466d1020e1d08a6245bafe2807ed4c1c8`;
- ownership-aware FileStore lock and heartbeat: `7b6df33...`, `f7aece2...`, `a9290a6...`;
- explicit production durability capability: `2696c718dbbf869296965993950665ea8c00bc7a`;
- pinned security scanners/actions and pin guard: `d202075...`, `4f5874f...`;
- durable Flow outbox boundary: `3b381cfc8ab2118ff1c48b84c8031fcf8d51a4ce`;
- race-safe/offline codegen tests and generated runtime verification: `0d41727...`, `1c54c1b...`;
- synchronous Pebble durable Flow store: `f8a8295148d96b0b710eac6302d66dd6386862f4`;
- tighter allocation ceilings: `c30e0bd88fecfe5188f479450caf7115ced98b15`;
- canonical durable Flow documentation: `fe9a44e...`, `034ec5f...`;
- ownership-aware admission locks: `aebc695...`, `fba33aa...`;
- core Pebble persisted-format identity and compatibility tests: `05374d2...`, `2013211...`, `ee0b291...`.

Do not replace these mechanisms with weaker alternatives while implementing remaining tasks.

---

# 2. P0 — Restore a trustworthy green `main`

Priority: **P0; no unrelated feature or performance work until complete.**

## P0-001 — Fix Windows portability of admission lock replacement regression

Status: **DONE**
Implementation commit: `9f51ae6ecbb0328c86214cd2f20ba473b67b8b0a`

### Evidence

Current Windows CI fails in:

`adgo/admission_lock_test.go` → `TestFileAdmissionLockCleanupDoesNotDeleteReplacementOwner`

The test removes/replaces a lock path while the first owner still has an open file handle. POSIX permits unlinking an open inode; Windows normally refuses with a sharing violation.

### Required change

Do not solve this by skipping the test on Windows.

Separate the invariants being tested:

1. **ownership-aware release** — an owner token A must not delete a path containing owner token B;
2. **stale takeover** — a stale/dead owner can eventually be reclaimed;
3. **live-owner safety** — a still-open/live owner must not be stolen;
4. **portable filesystem semantics** — tests must not require POSIX unlink-of-open-file behavior when Axiom claims Windows support.

Preferred test structure:

- test compare-before-release directly with owner A token against a replacement B record without retaining A's open file handle;
- test stale recovery after simulating process death by closing the old handle first;
- retain a POSIX-specific test only if it verifies an explicitly POSIX-only takeover window and is documented as such;
- preserve the existing generic `releaseFileLock` replacement-owner invariant;
- add an admission-specific integration test proving `withFileLock` uses the shared ownership primitive.

### Tests

```bash
go test ./adgo -run 'TestFileAdmissionLock|TestReleaseFileLock|TestRemoveStaleFileLock' -count=100
go test -race ./adgo -run 'TestFileAdmissionLock|TestReleaseFileLock|TestRemoveStaleFileLock' -count=20
go test ./...
go test -race ./...
```

Required GitHub matrix: Linux + Windows + macOS all green.

### Acceptance

- Windows unit job passes;
- no OS skip hides the ownership invariant;
- CI Completion Gate is green;
- security and module-checksum remain green.

---

## P0-002 — Make red-main recovery an explicit merge invariant

Status: **DONE**

### Change

Add a concise repository rule/documentation entry stating:

- a red `main` blocks unrelated implementation work;
- a failing cross-platform job is treated as a production regression, even when Linux/race are green;
- the next commit after a red baseline should either fix the failure or revert the introducing change;
- no test may be disabled merely to restore the badge.

This is documented in `CONTRIBUTING.md` and enforced as a mandatory execution rule.

### Acceptance

The rule is easy to find and consistent with CI behavior.

---

## P0-003 — Correct public Pebble codec documentation

Status: **DONE**

### Evidence

`internal/store/pebble.Open` defaults to JSON, while public comments in `axiom.go` still state:

- `PebbleJSONCodec uses JSON instead of Gob`;
- `PebbleGobCodec uses Gob encoding (default)`.

The new persisted-format marker makes this documentation error operationally important.

### Change

Update public documentation so it states:

- JSON is the default codec;
- Gob is opt-in;
- the selected codec is persisted into store metadata;
- reopening with a different codec fails fast;
- legacy unmarked stores are adopted only after format detection;
- mixed/ambiguous/future formats fail closed.

Update `store/pebble` package docs and runtime/storage documentation as needed. Keep one canonical detailed compatibility section and link to it from shorter API comments.

### Tests

- documentation/examples compile;
- any doctest/example commands remain valid;
- code search finds no remaining claim that Gob is the default.

---

## P0-004 — Protect `main` with required checks

Status: **EXTERNAL**

### Current state

`main` is unprotected and GitHub reports no required checks.

### Required repository settings

At minimum require:

- CI Completion Gate;
- security workflow or equivalent required security checks;
- module consistency/checksum gate;
- pull request before merge once the team is ready to stop direct pushes;
- no force pushes;
- no branch deletion;
- resolved review conversations;
- administrators should not silently bypass required checks for ordinary changes.

### Source-controlled companion changes

Add `.github/CODEOWNERS` for at least:

- runtime/core API;
- `internal/store/**`;
- `adgo/**`;
- `.github/workflows/**`;
- persisted-format/compatibility fixtures.

### Acceptance

GitHub branch metadata reports protection/rules enabled and required check names exactly match active workflow jobs.

---

# 3. TIME — Finish deterministic semantic-time separation

Priority: **P1 after P0 green.**

The initial time work fixed the most visible flakes, but the codebase still contains many direct `time.Now()` calls. Not every direct wall-clock call is wrong. The goal is to classify every call and remove only those that influence durable decisions.

## TIME-001 — Build a checked clock-usage inventory

Status: **TODO**

### Scope

Audit all direct uses of:

- `time.Now`;
- `time.NewTimer`;
- `time.NewTicker`;
- `time.After`;
- `time.Since`;
- `time.Sleep`.

At minimum classify callers in:

- `adgo/admission.go`;
- `adgo/policy.go`;
- `adgo/schedule.go` / scheduler code;
- `adgo/router*.go`;
- `adgo/retention.go`;
- `adgo/repair.go`;
- `adgo/cache.go`;
- `adgo/speculation.go`;
- `adgo/awaitable.go`;
- `adgo/engine.go`;
- `adgo/runtime.go`;
- `adgo/store.go` / `pebble_store.go`;
- `internal/runtime/retry_store.go`;
- core Memory/Pebble stores;
- `flow.go` / outbox code.

### Classification

Each use must be one of:

1. durable decision time;
2. lease/fencing time;
3. retry/schedule deadline time;
4. persisted event timestamp only;
5. observability/benchmark elapsed time;
6. OS/filesystem freshness boundary;
7. test-only wait.

Persist the allowlist/classification in a small architecture-check source file or generated/static check, not only in a one-time audit note.

---

## TIME-002 — Inject time into Admission controllers

Status: **TODO**

### Evidence

Memory/File admission controllers still evaluate acquire/heartbeat/refill/purge deadlines using direct wall clock.

### Change

Introduce a backward-compatible clock option/constructor path. Do not break existing `NewMemoryAdmissionController()` / `NewFileAdmissionController(...)` call sites.

All of these decisions must use one clock source:

- permit expiration;
- token refill;
- heartbeat extension;
- purge;
- retry-after calculation;
- snapshot cleanup.

### Tests

- exact deadline boundary;
- no early permit expiration;
- token refill at exact logical interval;
- heartbeat extension;
- deterministic rate-limit retry-after;
- restart/file persistence equivalence where applicable;
- no sleeps.

---

## TIME-003 — Unify retry deadline and retry waiting clocks

Status: **PARTIAL**

### Evidence

Retry due calculation uses the injected Engine clock, but `drainUntilIdle` still waits via `time.NewTimer(wait)`.

### Change

Use a timer-capable clock consistently. Reuse/adapt `internal/durabletime.Clock`, which already provides `Now()` and `NewTimer()`.

Avoid creating incompatible duplicate clock abstractions between:

- `internal/durabletime`;
- core runtime;
- ADGO public clock options.

Do not expose an internal package type through public API. Define/adapt the smallest stable interfaces at package boundaries.

### Tests

A ManualClock must be able to advance a durable retry from scheduled to runnable without wall-clock waiting.

---

## TIME-004 — Convert remaining durable decision paths

Status: **TODO**

After TIME-001 classification, migrate only semantic calls, including where applicable:

- schedule firing and dedup boundaries;
- policy delay/circuit windows;
- router cooldown/health decay;
- retention cutoff decisions;
- repair duration budgets;
- cache TTL when cache affects orchestration decisions;
- speculative execution deadlines;
- awaitable/human timeout decisions.

Keep observability timestamps/system filesystem checks on real time when that is the correct boundary.

---

## TIME-005 — Add architecture guard against semantic wall-clock regression

Status: **TODO**

Add a static/AST test that rejects new direct wall-clock calls in restricted orchestration files unless explicitly allowlisted with a reason.

### Acceptance

A future contributor cannot silently reintroduce `time.Now()` into a durable decision path.

---

# 4. STORE — Complete persistence contracts and schema safety

Priority: **P1.**

## STORE-001 — Add a reusable ADGO Store conformance suite

Status: **TODO/PARTIAL**

Core Store already has a reusable contract harness. ADGO has many good targeted tests but no equivalent single backend contract.

### Backends

Run one shared suite against:

- `MemoryStore`;
- `FileStore`;
- `PebbleStore`.

### Required invariants

- create/load copy isolation;
- CAS version conflict behavior;
- mutation callback atomicity;
- failed clone/encode causes no partial commit;
- inbox deduplication;
- inbox deterministic order;
- ack idempotency;
- catalog consistency;
- immutable historical versions where supported;
- context cancellation behavior;
- durability declaration where supported;
- restart/reopen equivalence;
- invalid/corrupt state error behavior.

Do not paper over backend differences; represent optional capabilities explicitly.

---

## STORE-002 — Define context-cancellation semantics for store operations

Status: **TODO**

### Evidence

Several store methods currently accept `context.Context` but intentionally ignore it, including `BeginTransaction` and many Pebble operations.

### Change

Document which operations are:

- cancellable before starting;
- non-interruptible once a local atomic commit begins;
- expected to return `ctx.Err()` before any write;
- allowed to finish a commit after cancellation for durability safety.

Then enforce the contract consistently across Memory/File/Pebble and Flow stores.

Do not add mid-commit cancellation that can violate atomicity.

---

## STORE-003 — Add ADGO Pebble persisted-format identity

Status: **TODO**

### Evidence

Core Pebble now pins schema+codec. `adgo/PebbleStore` persists JSON execution/inbox/catalog/version records but does not have an equivalent explicit format marker.

### Change

Introduce an ADGO-specific store-format envelope/marker with:

- schema version;
- format identity;
- migration policy;
- fail-fast future-schema behavior.

Do not reuse the core marker key if the stores have different schemas.

### Tests

- new empty store marker;
- reopen same version;
- reject future version;
- partial marker fail closed;
- legacy unmarked adoption only after validation;
- corruption does not silently become a new empty store.

---

## STORE-004 — Inventory every durable serialized surface

Status: **TODO**

Create a machine-reviewable inventory covering at least:

- core Pebble execution/task/history records;
- ADGO FileStore execution commits/inbox;
- ADGO Pebble execution/version/inbox/catalog records;
- Flow Pebble state/history/outbox records;
- schedules;
- router health state;
- admission state;
- retention/repair metadata;
- artifacts/manifests where durable compatibility matters.

For each surface record:

- owner package;
- encoding;
- current schema/version field;
- compatibility promise;
- migration path;
- golden fixture location.

---

## STORE-005 — Add golden compatibility fixtures

Status: **TODO**

Create `testdata/compat/` (or package-specific equivalents) containing previous supported serialized records.

Tests must prove:

- current code reads the supported previous form;
- unsupported future versions fail with explicit errors;
- migration preserves identity/order/version;
- reserialization does not silently discard unknown critical data;
- PlanDigest/module identity constraints remain enforced.

---

## STORE-006 — Expand FileStore/admission subprocess tests

Status: **PARTIAL**

Add true multi-process tests for:

1. competing committers;
2. process death while holding a lock;
3. stale recovery after process death;
4. owner A cleanup never removes B lock;
5. 10+ concurrent committers preserve monotonic versions;
6. inbox dedup under contention;
7. cancellation leaves no lock leak;
8. admission lock recovery follows the same ownership rules;
9. Windows and POSIX behavior are both represented without relying on unsupported unlink semantics.

Nightly is the preferred place for the heavier variants.

---

# 5. SCALE — Remove serialization bottlenecks only after store contracts are green

Priority: **P1/P2.**

## SCALE-001 — Benchmark current core Pebble transaction contention

Status: **TODO**

### Evidence

`internal/store/pebble.Store.BeginTransaction` takes `s.mu` and keeps it until transaction `Commit/Rollback`. This serializes unrelated executions.

### Before changing code

Add benchmarks for:

- 1, 2, 4, 8, 16, 32 concurrent independent executions;
- read-heavy vs write-heavy transactions;
- same execution vs different execution IDs;
- commit latency percentiles;
- allocations;
- mutex/block profiles.

Capture baseline artifacts.

---

## SCALE-002 — Measure double-serialization between Engine and Store

Status: **TODO**

Core Engine also owns `storeMu` around transactional execution paths while it has per-execution locking.

Determine with profiling whether:

- `executionLocks` already provide sufficient same-execution serialization;
- `storeMu` serializes unrelated executions unnecessarily;
- Store transaction isolation depends on that outer lock.

Write correctness tests before removing any lock.

---

## SCALE-003 — Design conflict/isolation model before replacing global mutex

Status: **TODO**

Write a short design note containing:

- transaction read snapshot semantics;
- sequence allocation strategy;
- same-execution conflict handling;
- cross-execution independence;
- task dedup/index consistency;
- commit atomicity;
- retry/CAS policy;
- rollback behavior.

Possible implementations may include execution-scoped/striped locking, Pebble indexed batches/snapshots, or optimistic CAS. Choose only after benchmarks and contracts exist.

---

## SCALE-004 — Refactor core Pebble transaction locking

Status: **TODO**

Acceptance requirements:

- same-execution semantics unchanged;
- all shared Store contract tests green;
- race detector green;
- no task/history sequence regression;
- throughput for independent executions improves materially at concurrency >1;
- no significant single-thread regression.

---

## SCALE-005 — Benchmark and reduce ADGO Pebble global mutex contention

Status: **TODO**

ADGO `PebbleStore` also guards its full API with one mutex. After STORE-001 and schema work are green:

- establish contention profiles;
- split execution/catalog/inbox lock domains only where correctness permits;
- preserve version-CAS semantics;
- add high-contention backend-equivalence tests.

---

# 6. TYPED — Optimize `ActTyped` without changing data semantics

Priority: **P1/P2.**

## TYPED-001 — Freeze typed activity conversion semantics with tests

Status: **TODO**

Before optimizing, define the exact supported contract for:

- struct input/output;
- pointer structs;
- named `map[string]T` types;
- `axiom:"name"` tag precedence;
- `json:"name"` tags;
- `json:"-"`;
- `omitempty` behavior if supported;
- embedded fields;
- nil pointers;
- integers vs floating JSON numbers;
- slices/maps/nested structs;
- custom `json.Marshaler` / `json.Unmarshaler` behavior;
- unknown fields;
- missing required fields if Axiom has such a concept.

Do not assume Go `encoding/json` behavior and current reflection behavior are identical — test and deliberately choose the public contract.

---

## TYPED-002 — Establish `Act` vs `ActTyped` benchmark baseline

Status: **TODO**

Measure:

- ns/op;
- B/op;
- allocs/op;
- small/medium/nested structs;
- map input;
- pointer-heavy input;
- generated wrapper path.

Save baseline in the benchmark artifact schema.

---

## TYPED-003 — Compile conversion plans at registration time

Status: **TODO**

Move reflection/tag discovery out of each activity invocation.

At registration time build immutable plans for:

- input field lookup and assignment;
- output field names/access;
- pointer handling;
- conversion validation.

Cache by concrete type where safe.

---

## TYPED-004 — Remove unnecessary JSON round trip from typed input path

Status: **TODO**

Current path performs `json.Marshal(input map)` then `json.Unmarshal` into `In` for every call.

Replace it with a lower-allocation converter only after TYPED-001 semantics are frozen.

Acceptance:

- all semantic fixtures match;
- error quality does not regress;
- large integer correctness does not regress;
- no unsafe reflection panic;
- materially fewer allocations than baseline.

---

## TYPED-005 — Remove per-call output reflection

Status: **TODO**

Use the registration-time output plan. Preserve tag behavior and named map support.

---

## TYPED-006 — Optional generator specialization

Status: **TODO / later**

After generic typed mapping is correct, allow `axiomgen` to emit specialized adapters for zero/minimal reflection on known generated types.

Do not make generated wrappers semantically different from the normal typed API.

---

# 7. ERR — Replace string-based control flow with typed errors

Priority: **P1.**

## ERR-001 — Eliminate `AX505` substring retry classification

Status: **TODO**

### Evidence

`isRetryableActivityFailure(message string)` currently treats any string containing `AX505` as retryable.

This can misclassify unrelated error text and makes control flow depend on formatting.

### Change

Introduce/propagate a typed activity failure classification through the Store/retry boundary.

Use:

- `errors.Is` / `errors.As`;
- explicit typed/sentinel retryability;
- diagnostic `Code` only as a stable presentation identifier, not arbitrary substring parsing.

### Backward compatibility

If low-level/custom Store APIs only provide error strings, define a narrow compatibility adapter rather than preserving broad `strings.Contains` forever.

### Tests

- exact typed AX505 retryable failure;
- wrapped typed failure;
- text containing `AX505` but not typed failure is not retryable;
- terminal diagnostic does not retry;
- retry exhaustion unchanged;
- persisted history still exposes the stable diagnostic code expected by users.

---

## ERR-002 — Unify transaction commit-on-error classification

Status: **PARTIAL**

`shouldCommitTransactionError` recognizes typed retry scheduling and selected diagnostic codes. Define an explicit interface/type for errors whose state mutations are intentionally durable despite a returned control-flow error.

Avoid growing a switch over presentation error codes.

---

## ERR-003 — Publish an error taxonomy

Status: **TODO**

Define categories for:

- configuration/compile errors;
- retryable activity errors;
- terminal activity errors;
- conflicts;
- stale lease/fencing;
- corruption/compatibility;
- durability capability failure;
- cancellation/deadline;
- transient storage/system failure.

Document which are safe for callers to branch on and provide `errors.Is/As` behavior.

---

# 8. FLOW — Finish durable-effect crash and operations engineering

Priority: **P1/P2.**

The durable Flow outbox is already implemented. Do not redesign it from scratch.

## FLOW-001 — Add deterministic failpoints around every durable effect boundary

Status: **TODO**

Test crashes/failures:

1. before state+intent commit;
2. after commit, before effect call;
3. during effect call;
4. after effect success, before acknowledgement;
5. during acknowledgement commit;
6. after acknowledgement;
7. during reopen/recovery.

Prove:

- no external effect before durable intent;
- no lost pending effect;
- same EffectID is reused for redelivery;
- acknowledged effect is not redelivered;
- state/history/outbox remain mutually consistent.

---

## FLOW-002 — Add outbox backlog and recovery observability

Status: **TODO**

Expose bounded-cardinality metrics/diagnostics for:

- pending effects;
- oldest pending age;
- delivery attempts/failures;
- acknowledgement failures;
- recovery deliveries.

Do not use execution ID as an unbounded metric label.

---

## FLOW-003 — Define batching/backpressure behavior

Status: **TODO**

For large backlogs define:

- max batch size;
- retry pacing;
- cancellation behavior;
- fairness between executions;
- memory bound;
- shutdown/drain behavior.

Add scale benchmarks before optimizing.

---

## FLOW-004 — Version Flow durable records

Status: **TODO**

Include Flow state/history/outbox in STORE-004/STORE-005 compatibility work. Do not assume application state JSON itself is globally migratable; distinguish Axiom envelope schema from user state schema.

---

## FLOW-005 — Keep non-durable Flow guarantee explicit

Status: **DONE/POLICY**

The compatibility Flow path may execute an effect before persisting state. Keep documentation explicit that users requiring crash-safe external effects must use durable effects with a synchronous `DurableFlowStore` and downstream idempotency by EffectID.

Do not silently change compatibility semantics in a patch release.

---

# 9. PERF — Turn performance checks into relative regression engineering

Priority: **P2 after correctness work.**

## PERF-001 — Keep tightened allocation ceilings and add relative baselines

Status: **PARTIAL**

Existing hard ceilings are useful catastrophe guards. Add reviewed relative thresholds against checked-in baseline data.

Suggested policy:

- fail or require review for >10–15% alloc/op regression on stable hot paths;
- compare latency only with statistically meaningful benchmark sampling;
- never auto-rewrite the baseline in the same change that regresses it.

---

## PERF-002 — Version benchmark artifact schema

Status: **PARTIAL/TODO**

Ensure benchmark JSON contains:

- schema version;
- commit SHA;
- Go version;
- OS/arch;
- CPU metadata when available;
- sample count;
- throughput;
- percentile latency;
- bytes/op;
- allocs/op;
- benchmark configuration.

---

## PERF-003 — Add ADGO production-path benchmarks

Status: **TODO**

Cover:

- runtime supersteps;
- scheduler selection;
- router decisions;
- admission acquire/heartbeat;
- Memory/File/Pebble commits;
- inbox processing;
- lease recovery;
- Flow outbox delivery bookkeeping;
- retention/repair scans.

---

## PERF-004 — Add long-history and high-cardinality storage benchmarks

Status: **TODO**

Measure:

- 1K/10K/100K history entries;
- large task queues;
- large inboxes;
- many executions;
- outbox backlog;
- catalog scans;
- reopen/recovery cost.

Use results to choose indexes/retention strategy rather than guessing.

---

# 10. SEC — Reduce security blind spots

Priority: **P1/P2.**

## SEC-001 — Remove broad global gosec exclusions one rule at a time

Status: **TODO**

Current global exclusions include:

`G101,G104,G115,G301,G302,G304,G306,G404,G703`

### Procedure for each rule

1. enable one rule;
2. inspect every finding;
3. fix real issues;
4. for intentional behavior, place the narrowest local `#nosec <rule> -- justification` or equivalent documented suppression;
5. keep tests/security green;
6. commit the rule removal atomically.

Do not replace one broad global exclusion with a broad directory exclusion unless the directory is explicitly non-production and justified.

---

## SEC-002 — Minimize workflow permissions per job

Status: **TODO**

Review workflows so write permissions exist only in jobs that need them.

In particular:

- security scans generally need read access unless uploading SARIF/security events;
- release publication needs contents/packages/id-token writes, but verification/build jobs should not inherit unnecessary write capability if job-level permissions can narrow them.

---

## SEC-003 — Preserve immutable dependency pins

Status: **DONE/POLICY**

The action pin guard and pinned scanner versions are established invariants. Update pins intentionally and keep the guard.

---

## SEC-004 — Harden release provenance

Status: **PARTIAL**

SBOM and SHA256SUMS already exist. Add/verify:

- artifact provenance/attestation;
- reproducible build metadata;
- immutable target SHA recorded in release assets/notes;
- container provenance/signing strategy if containers remain a supported artifact.

---

# 11. API — Stabilize public surface before v1

Priority: **P2.**

## API-001 — Add mechanical public API compatibility gate

Status: **TODO**

Create a checked baseline/API manifest for exported identifiers in supported public packages and compare in CI.

The gate should distinguish:

- additive compatible change;
- signature change;
- removal;
- deprecation;
- package move.

For pre-v1, intentional breaking changes may be allowed only with explicit version/changelog annotation.

---

## API-002 — Classify panic-on-programmer-error APIs

Status: **TODO**

Flow registration helpers currently panic on nil flow/handler and have tests asserting that behavior. Decide/document whether each exported `Must*`/registration helper is intentionally panic-based or should return an error in a future minor version.

Do not casually convert existing panic contracts in a patch release.

---

## API-003 — Audit deprecated aliases and root facade size

Status: **TODO**

Inventory:

- deprecated aliases such as `Register`;
- root facade exports;
- duplicate constructors;
- low-level runtime exports that should remain internal.

Produce a pre-v1 migration/deprecation table before removing anything.

---

## API-004 — Split large root files by responsibility without changing package API

Status: **TODO / maintainability**

After compatibility tests exist, split large files such as `axiom.go` into focused implementation files:

- app/load/compile;
- engine options;
- activity registration/typed adapters;
- Pebble facade;
- public aliases/constants.

This is a source-layout refactor only; exported names stay in package `axiom`.

---

# 12. ARCH — Extract a shared durable kernel only after contracts are stable

Priority: **P2/P3.**

## ARCH-001 — Inventory duplicated durable primitives

Status: **TODO**

Compare core Axiom and ADGO implementations of:

- identity framing;
- retry/backoff;
- lease/fencing;
- durability capability;
- version/CAS semantics;
- clock adapters;
- persisted-format version handling;
- lock ownership;
- error classification.

Mark each as:

- genuinely identical and extractable;
- superficially similar but different contract;
- frontend/runtime-specific and should remain separate.

---

## ARCH-002 — Define `internal/durable` dependency direction

Status: **TODO**

Target shape:

```text
frontends / DSL / Go APIs
        ↓
immutable plans / IR
        ↓
Axiom runtime       ADGO runtime
        ↘           ↙
      shared durable primitives
              ↓
         storage SPI
```

Do **not** merge Axiom and ADGO engines into one large runtime.

---

## ARCH-003 — Extract only pure, proven primitives

Status: **TODO**

Good initial candidates after ERR/TIME/STORE contracts:

- retry/backoff calculation;
- lease/fencing predicates;
- canonical identity framing;
- durability levels/capability helpers;
- format-version validation helpers where schemas remain separate.

Each extraction must have no behavior change and must keep package dependency direction acyclic.

---

## ARCH-004 — Add architecture dependency tests

Status: **TODO**

Prevent:

- `internal/durable` importing high-level runtime packages;
- public frontend packages depending on concrete persistence implementations unnecessarily;
- cyclic Axiom↔ADGO coupling;
- test-only helpers leaking into production APIs.

---

# 13. CI — Complete the production verification DAG

Priority: **P1/P2.**

## CI-001 — Restore cross-platform green baseline

Status: **DONE**
Implementation commit: `9f51ae6ecbb0328c86214cd2f20ba473b67b8b0a`

Same as P0-001. Cross-platform CI matrix (Linux, Windows, macOS) is verified green.

---

## CI-002 — Add focused concurrency diagnostics

Status: **TODO**

Keep full race, but add a small named diagnostic job/test selection for:

- lease/fencing;
- concurrent stores;
- Flow dispatch;
- FileStore/admission locks;
- scheduler concurrency.

This shortens diagnosis when the full race job fails.

---

## CI-003 — Add nightly subprocess/crash suite

Status: **TODO**

Nightly should include:

- FileStore multiprocess contention/recovery;
- admission multiprocess lock recovery;
- Flow crash/failpoints;
- persisted-format compatibility fixtures;
- transaction contention stress;
- repeated race.

---

## CI-004 — Add API compatibility and persisted compatibility jobs

Status: **TODO**

These should become explicit jobs rather than being hidden inside generic unit tests once fixtures are stable.

---

## CI-005 — Keep generated wrapper runtime verification isolated

Status: **DONE/POLICY**

Do not reintroduce nested `go mod tidy` or network dependency resolution inside `go test -race`.

---

# 14. RELEASE — Make release code and release policy identical

Priority: **P1 before publishing a stable artifact.**

## REL-001 — Fix frozen release branch resolution

Status: **TODO**

### Evidence

`docs/versioning.md` promises that manual publication resolves `release/<version>`, verifies it is an ancestor of `main`, and publishes that frozen SHA.

Current `.github/workflows/release.yml` manual path instead sets:

```bash
sha="$(git rev-parse HEAD)"
```

This can publish the workflow-dispatch branch HEAD rather than the frozen candidate described by policy.

### Change

Make workflow behavior match the documented contract:

1. validate SemVer input;
2. require dispatch from allowed branch/ref;
3. resolve `refs/heads/release/<version>`;
4. verify it exists;
5. verify candidate is an ancestor of `main`;
6. verify release notes policy;
7. reject existing tag/release unless an explicit safe recovery path is used;
8. checkout/publish the exact frozen SHA.

Add workflow/script tests for metadata resolution where practical.

---

## REL-002 — Upgrade release verification to current main gates

Status: **TODO**

Release verification currently runs a narrower race command than main CI.

Before publishing, require at least:

- module hygiene;
- `go vet ./...`;
- `go test ./...`;
- `go test -race ./...` on supported release runner;
- compatibility fixtures;
- codegen verification;
- security gate or verified reusable workflow result;
- benchmark regression review.

Do not publish a release from a candidate that would fail current main requirements.

---

## REL-003 — Keep SBOM/checksum generation and add provenance

Status: **PARTIAL**

Current release workflow already creates CycloneDX SBOM and SHA256SUMS. Preserve them and add SEC-004 provenance/attestation.

---

## REL-004 — Publish and verify the first real GitHub Release

Status: **TODO**

At audit time the GitHub Releases collection is empty even though `release/v0.1.0` exists.

Only publish after:

- current `main` is green;
- REL-001/REL-002 are fixed;
- release notes match the candidate;
- security is green.

After publication verify:

- tag SHA == frozen candidate SHA;
- binary archives are present;
- SBOM present;
- SHA256SUMS validates every release asset;
- container tags point to the intended version/SHA;
- release notes name the persisted/API compatibility contract.

---

# 15. GOV — Repository governance and hygiene

Priority: **P1/P2.**

## GOV-001 — Add CODEOWNERS

Status: **TODO**

See P0-004.

---

## GOV-002 — Protect `main`

Status: **EXTERNAL**

See P0-004.

---

## GOV-003 — Clean stale implementation branches after verification

Status: **TODO / maintenance**

The repository still contains many historical `agent/*` branches plus the frozen release branch.

Do not bulk-delete blindly.

Procedure:

1. classify branches as merged, superseded, active, or frozen-release;
2. retain `release/*` according to release policy;
3. verify no open PR/workflow depends on an old branch;
4. delete only clearly merged/superseded branches;
5. document branch naming/lifetime policy.

This is not a production runtime blocker but reduces operational confusion.

---

# 16. OPS — Production observability and recovery

Priority: **P2.**

## OPS-001 — Define stable metric cardinality contract

Status: **TODO/PARTIAL**

Audit all metrics and prohibit unbounded labels such as raw execution IDs, task IDs, or arbitrary user keys.

Define stable metrics for:

- execution states;
- queue depth;
- lease expirations/fencing;
- retries/exhaustion;
- store latency/error rate;
- outbox backlog;
- repair attempts;
- scheduler throttling;
- admission denial;
- schema/corruption errors.

---

## OPS-002 — Separate liveness and readiness

Status: **TODO**

Readiness should fail/degrade when the process cannot safely accept work, including where relevant:

- durable store unavailable;
- schema incompatibility;
- coordinator lease failure;
- severe outbox backlog;
- repair subsystem exhausted;
- migration required.

Liveness should remain narrower and not restart a healthy-but-degraded process unnecessarily.

---

## OPS-003 — Expand runbooks

Status: **PARTIAL/TODO**

Ensure operators have explicit procedures for:

- stuck/expired leases;
- stale FileStore/admission locks;
- corrupted JSON/Pebble records;
- future/unsupported schema marker;
- retry storm;
- outbox backlog/poison effect;
- repair exhaustion;
- retention mistakes;
- failed migration;
- release rollback;
- store backup/restore verification.

---

# 17. PROP — Advanced correctness and crash equivalence

Priority: **P2/P3.**

## PROP-001 — Replay equivalence properties

Status: **TODO/PARTIAL**

For supported deterministic paths, prove that replay from durable history yields the same state/decisions as live execution.

Add randomized event sequences and shrinking-friendly test data.

---

## PROP-002 — Backend equivalence

Status: **TODO**

Run equivalent workloads against Memory/File/Pebble where capabilities overlap and compare:

- final state;
- event/task ordering;
- version progression;
- retry decisions;
- dedup behavior;
- terminal states.

---

## PROP-003 — Crash equivalence/failpoint framework

Status: **TODO**

Create reusable failpoints around durable boundaries rather than bespoke booleans in individual tests.

Use them for:

- core transaction commit;
- task lease/complete/fail;
- FileStore lock recovery;
- Flow outbox;
- migration/format adoption.

---

## PROP-004 — No-resurrection invariants

Status: **TODO**

Property-test that terminal/canceled/superseded work cannot be unintentionally resurrected by:

- lease recovery;
- retry scheduling;
- inbox redelivery;
- migration;
- repair;
- stale worker completion.

---

## PROP-005 — Budget and repair locality invariants

Status: **TODO/PARTIAL**

Expand existing targeted tests into randomized properties:

- budget usage never decreases unexpectedly;
- bounded repair cannot exceed configured scope/budget;
- targeted repair does not invalidate unrelated completed work;
- migration does not reactivate invalid work.

---

# 18. Recommended execution order

This order is mandatory unless a newly discovered P0 supersedes it.

## Milestone M0 — Restore trustworthy baseline

1. **P0-001** Windows admission lock portability.
2. **P0-002** red-main rule.
3. **P0-003** correct Pebble codec/persisted-format docs.
4. Re-run all CI/security/checksum gates until `main` is fully green.
5. **P0-004 / GOV-002** enable branch protection and required checks when repository settings can be changed.
6. **GOV-001** CODEOWNERS.

Exit: cross-platform green baseline + race green + protected merge path.

## Milestone M1 — Finish deterministic runtime contracts

7. TIME-001 clock inventory.
8. TIME-002 admission clock.
9. TIME-003 retry timer clock unification.
10. TIME-004 remaining semantic deadlines.
11. TIME-005 wall-clock architecture guard.
12. ERR-001 typed retryability.
13. ERR-002/ERR-003 error taxonomy.

Exit: no mixed semantic/wall time in durable decisions and no string-formatted control flow.

## Milestone M2 — Finish persistence compatibility

14. STORE-001 ADGO Store contract.
15. STORE-002 context semantics.
16. STORE-003 ADGO Pebble format marker.
17. STORE-004 persisted surface inventory.
18. STORE-005 golden compatibility fixtures.
19. STORE-006 subprocess lock/admission suite.
20. CI-003/CI-004 explicit nightly/compatibility jobs.

Exit: all durable surfaces have known contracts and upgrade behavior.

## Milestone M3 — Crash correctness

21. FLOW-001 effect failpoint matrix.
22. PROP-003 reusable crash failpoints.
23. PROP-001 replay equivalence.
24. PROP-002 backend equivalence.
25. PROP-004 no-resurrection properties.
26. FLOW-002/003 outbox operations/backpressure.

Exit: crash/replay/recovery claims are tested at durable boundaries.

## Milestone M4 — Performance work

27. TYPED-001 semantic freeze.
28. TYPED-002 benchmark baseline.
29. TYPED-003/004/005 optimization.
30. PERF-001/002 relative benchmark infrastructure.
31. SCALE-001/002 contention profiling.
32. SCALE-003 isolation design.
33. SCALE-004 core Pebble concurrency refactor.
34. SCALE-005 ADGO Pebble concurrency refactor.
35. PERF-003/004 extended ADGO/storage benchmarks.

Exit: optimizations are measured, semantics-preserving, and regression-gated.

## Milestone M5 — Security/API/release

36. SEC-001 remove broad gosec exclusions incrementally.
37. SEC-002 workflow permission minimization.
38. API-001 compatibility gate.
39. API-002/003 public contract cleanup plan.
40. REL-001 frozen candidate resolution.
41. REL-002 release gates match main.
42. SEC-004/REL-003 provenance.
43. REL-004 publish and verify real release.

Exit: releases are reproducible, policy-compliant and mechanically protected.

## Milestone M6 — Architecture and operations

44. ARCH-001 duplicate durable primitive inventory.
45. ARCH-002 dependency design.
46. ARCH-003 selective extraction.
47. ARCH-004 dependency guard.
48. API-004 source-layout split.
49. OPS-001/002/003 production operations completion.
50. GOV-003 stale branch cleanup.
51. Remaining PROP properties.

Exit: maintainable long-term architecture with explicit operational contract.

---

# 19. Task-level verification template

Every implementation task should end with a record like:

```text
Task: <ID>
Baseline HEAD: <sha>
Implementation commit: <sha>
Changed files: <paths>
Behavior changed: yes/no
Persisted format changed: yes/no
Public API changed: yes/no
Narrow tests: <command/result>
Race tests: <command/result or N/A>
Cross-platform CI: Linux / Windows / macOS
Security: pass/fail/N/A
Bench before: <artifact or N/A>
Bench after: <artifact or N/A>
Docs updated: <paths or N/A>
Plan status updated: yes
Residual risk: <short statement>
```

If a task cannot satisfy its acceptance criteria, leave it incomplete and record the blocker. Do not mark partial behavior as DONE.

---

# 20. Immediate next implementation batch

Do not begin with Pebble concurrency or `ActTyped` optimization.

The next batch is exactly:

1. fix `TestFileAdmissionLockCleanupDoesNotDeleteReplacementOwner` portability without hiding the invariant;
2. verify `go test ./adgo` on Windows semantics through CI;
3. verify `go test ./...` on Linux/Windows/macOS;
4. verify `go test -race ./...`;
5. correct public JSON/Gob default documentation and document format pinning;
6. only when all gates are green, start TIME-001/TIME-002;
7. update this document with implementation commit SHAs.

This batch restores a reliable foundation before any additional architecture or performance work.
