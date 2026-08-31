# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`
Target branch: `main`
Baseline before plan bootstrap: `faf3fba67a377b354d8fa1a5745cef0b8b96f0c7`
Last qualified implementation commit: `094de51e4e42d72d4bdb4f813f342cee71f9ac87`
Current unqualified remote HEAD before Iteration 4: `8e1b11560305d56010049c992968c11f3197ca9e`
Last reconciliation: 2026-08-31

> This file is the only execution roadmap. `docs/PRODUCTION_STABILIZATION_PLAN.md`, `adgo/AGENT_PLATFORM_PLAN_RU.md`, audit reports and historical release documents are evidence/reference inputs, not parallel execution plans. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Re-read remote `main`, this file, `CONTRIBUTING.md`, `DEVELOPMENT.md` and relevant contracts before substantial work.
2. Every substantial change has `T-XXX`; every substantial unexpected problem has `F-XXX` before the corresponding architecture/API/security/persistence behavior is changed.
3. Red `main` blocks unrelated work. A flaky failure is a failure until its root cause is explained and removed.
4. One logical task = one logical commit containing implementation, tests, relevant docs and plan reconciliation whenever technically possible.
5. Never force-push.
6. Prefer executable invariants, root-cause fixes, fail-closed behavior and small reversible transitions.
7. Critical policy/state/validation logic uses mutation testing where applicable. Concurrency/timing defects prefer deterministic scheduling/clock evidence plus race/shuffle stress when mutation cannot express the semantic ordering.
8. Performance changes require measurement first.
9. A commit cannot embed its own final SHA without changing that SHA. An iteration records `Commit: <this commit>`; the next synchronization checkpoint records the actual verified remote SHA.
10. Post-push remote HEAD and CI/security/quality gates must be green before an iteration is qualified.

Task states: `TODO`, `READY`, `IN_PROGRESS`, `VERIFYING`, `BLOCKED`, `DONE`, `DEFERRED`, `REJECTED`.
Finding states: `OPEN`, `INVESTIGATING`, `VERIFYING`, `RESOLVED`, `ACCEPTED_RISK`, `REJECTED`.

---

## 1. Architecture and critical invariants

- Declarative Go `model`, AXM and TOML converge on the canonical compiled Axiom runtime.
- Typed `Flow` remains a separate reducer-oriented surface.
- `adgo` remains a separate durable graph/coordinator/worker runtime.
- Core and ADGO engines must not be merged into a mega-runtime; only behavior-identical low-level durable primitives may be shared after executable characterization.
- External effects are at-least-once, never falsely exactly-once.
- Idempotency/reconciliation is explicit at external effect boundaries.
- Durable intents/tasks are persisted before external execution where the contract requires it.
- Stale workers cannot commit through fencing.
- Execution/plan/schema identity is explicit; incompatible persisted formats fail closed.
- Same-execution mutation is serialized/validated while independent executions may progress concurrently.
- Semantic durable time uses explicit clock abstractions. If a caller can observe an operation as started, timers governing that operation must already be armed against the same semantic clock.
- Parsed syntax is not a runtime guarantee until executable behavior proves it.
- Cross-platform/race/security/quality gates are not weakened to regain green.

Repository/process state:

- `MASTER_PLAN.md` is authoritative; `AGENTS.md` is absent; active instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` branch protection disabled; T-010 remains an external blocker.
- T-002 commit `094de51e4e42d72d4bdb4f813f342cee71f9ac87` is the last fully qualified implementation commit.
- T-003 commit `8e1b11560305d56010049c992968c11f3197ca9e` has `ci`, `security` and `module-checksum` PASS but `quality-loop` FAIL because of F-014; therefore it is not qualified yet.
- No GitHub Release had been published at the audited baseline.

---

## 2. Findings

### F-001 — Missing authoritative living execution plan
**Status:** RESOLVED by T-001
**Severity:** High
**Root cause:** topic/era plans accumulated without a final ownership boundary.

### F-002 — `main` is not protected
**Status:** OPEN / external configuration
**Category:** Governance / CI integrity
**Severity:** Critical process risk
**Evidence:** GitHub branch metadata reports `protected=false` and no required checks.
**Impact:** direct pushes can bypass otherwise strong source-controlled verification.
**Affected task:** T-010.

### F-003 — Manual release selected caller HEAD instead of frozen candidate
**Status:** RESOLVED by T-002
**Category:** Release / supply chain
**Severity:** High

### F-004 — Release verification was weaker than normal `main`
**Status:** VERIFYING via T-003
**Category:** CI/CD
**Severity:** High
**Root cause:** release verification and normal verification were separately maintained DAGs.
**Implemented direction:** current reusable `ci` and `security` workflows verify the frozen SHA.

### F-005 — Core and ADGO duplicate durable primitives without executable anti-drift boundary
**Status:** OPEN
**Category:** Architecture
**Severity:** High
**Affected tasks:** T-020..T-023.

### F-006 — Durable Flow crash boundaries lack comprehensive failpoint qualification
**Status:** OPEN
**Category:** Persistence / reliability
**Severity:** High
**Affected tasks:** T-030..T-033.

### F-007 — Security workflow has broad global gosec exclusions
**Status:** OPEN
**Category:** Security
**Severity:** High
**Evidence:** `G101,G104,G115,G301,G302,G304,G306,G404,G703` are excluded globally.
**Affected task:** T-040.

### F-008 — Public compatibility promises lack a mechanical API gate
**Status:** OPEN
**Category:** API / compatibility
**Severity:** High
**Affected tasks:** T-050/T-051.

### F-009 — Documentation has semantic drift
**Status:** OPEN
**Category:** Documentation / process
**Severity:** Medium
**Evidence:** stale `first/latest` architecture text, README workflow badge path drift, DEVELOPMENT race-command drift.
**Affected tasks:** T-060/T-061.

### F-010 — Tag-trigger prerelease detection treated every `v*` tag as prerelease
**Status:** RESOLVED by T-002; tag publication trigger removed by T-003
**Category:** Release correctness
**Severity:** High

### F-011 — Release publication was more permissive than documented policy
**Status:** VERIFYING via T-003
**Category:** Release / supply chain
**Severity:** High
**Implemented direction:** require candidate notes; reject existing release; no generated-note/upload/clobber fallback; explicit target SHA and post-create tag verification.

### F-012 — Security workflow requested unused repository-wide `security-events: write`
**Status:** RESOLVED by T-003
**Category:** CI security / permissions
**Severity:** Medium
**Resolution:** reusable verification security workflow is read-only (`contents: read`).

### F-013 — Tag-push release could execute obsolete release tooling from the frozen tag
**Status:** VERIFYING via T-003
**Category:** Release / supply chain
**Severity:** High
**Root cause:** two publication entrypoints with different workflow-code ownership.
**Implemented direction:** publication is `workflow_dispatch` from current `main` only; tag creation is an output, never a publication trigger.

### F-014 — Hedged activity can register its semantic timer after primary execution becomes observable

**Status:** IN_PROGRESS via T-004
**Category:** Concurrency / deterministic time / CI reliability
**Severity:** High
**Confidence:** High

**Evidence:** `quality-loop` run `33427488084`, job `99604582523`, exact shuffle seed `1788202523607722540`, failed under race with:

`TestHedgedActivityDeterministicClock: timed out waiting for hedged activity result`

The same `adgo/speculation.go` blob (`e5d43d45fce56b81832894643792f4fc9721405f`) existed on fully green `094de51e...`, proving the defect predates T-003.

**Observed behavior:** `NewHedgedActivity` launched `variants[0]` before calling `newSpeculativeTimer`. The primary handler could therefore signal `v1Started`; the test/caller could advance `ManualClock` by `HedgeDelay`; only afterward could the timer be registered.

**Expected behavior:** once primary execution is observable, the hedge timer must already be registered against the same semantic clock.

**Root cause:** timer registration happened after asynchronous primary launch. `ManualClock.NewTimer` computes its deadline from logical `Now()` at registration, so late registration after a +5s advance moves the expected +5s hedge deadline to +10s.

**Impact:** deterministic-time semantics depend on goroutine scheduling; race/shuffle quality checks become flaky and simulations can delay hedging by an entire interval.

**Blast radius:** `NewHedgedActivity` users with injected/manual timer clocks; quality-loop trustworthiness.

**Affected invariants:** semantic clock ordering; deterministic replay/simulation; no unexplained flaky tests; red-main invariant.

**Affected task:** T-004.

**Recommended direction:** arm the first semantic hedge timer before primary goroutine launch; keep subsequent timer reset behavior unchanged.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process / red-main recovery

#### T-001 — Establish authoritative `MASTER_PLAN.md`
**Status:** DONE
**Implementation commit:** `414f01c84ec215b29784cbfa7e5987cb35cdea41`

#### T-004 — Restore deterministic hedged timing and green quality-loop
**Status:** VERIFYING
**Priority:** P0 red-main blocker
**Finding:** F-014
**Depends on:** none

Acceptance:
- primary handler cannot become observable before first `HedgeDelay` timer is registered;
- existing `TestHedgedActivityDeterministicClock` passes under race/shuffle, including the failure pattern that exposed F-014;
- no sleep/retry/test-disable workaround is introduced;
- ordinary hedged result selection, cancellation, budget aggregation and subsequent hedge timer resets remain unchanged;
- `go test -race ./...`, `quality-loop`, `ci`, `security`, and `module-checksum` all pass post-push;
- T-003 can then be qualified because its only red gate was this pre-existing defect.

#### T-010 — Protect `main` with required checks
**Status:** BLOCKED — external GitHub setting
**Priority:** P0
**Minimum external action:** enable branch/ruleset protection with exact active required checks, no force push/delete, and appropriate PR/review requirements.

### M1 — Release correctness and provenance

#### T-002 — Frozen release metadata resolution
**Status:** DONE
**Priority:** P0/P1
**Implementation commit:** `094de51e4e42d72d4bdb4f813f342cee71f9ac87`
**Qualification:** `ci`, `security`, `quality-loop`, `module-checksum` PASS.

#### T-003 — Publication and verification match one release contract
**Status:** VERIFYING — blocked only by T-004 red-main recovery
**Priority:** P1
**Depends on:** T-002

Implemented acceptance surface:
- one `workflow_dispatch` publication entrypoint from `main`;
- non-empty frozen candidate release notes required;
- existing release rejected fail-closed;
- reusable current `ci` and `security` workflows verify exact candidate SHA;
- no caller/callee concurrency-group collision;
- no generated-note/upload/clobber fallback;
- `gh release create --target "$TARGET_SHA"` plus post-create remote tag SHA verification;
- executable workflow contract tests protect these policies.

Provenance/attestation and broader release permission minimization remain T-041.

### M2 — Durable correctness closure
- **T-030 — reusable deterministic durable failpoint framework:** TODO, P1.
- **T-031 — Flow intent/effect/ack crash matrix:** TODO, P1, depends T-030.
- **T-032 — no-resurrection/backend/crash equivalence properties:** TODO, P1/P2, depends T-030.
- **T-033 — Flow backlog/backpressure/observability contracts:** TODO, P2, depends T-031.

### M3 — Shared durable primitives without merging engines
- **T-020 — inventory Core vs ADGO durable primitive contracts:** TODO, P1.
- **T-021 — define acyclic `internal/durable` boundary:** TODO, P1/P2, depends T-020.
- **T-022 — extract only behavior-identical pure primitives:** TODO, P2, depends T-021.
- **T-023 — architecture dependency/anti-drift tests:** TODO, P2, depends T-021.

### M4 — Security boundary reduction
- **T-040 — remove broad gosec exclusions incrementally:** READY, P1; must not start until T-004 restores green `main`.
- **T-041 — minimize remaining workflow permissions, add release provenance/attestation and pin container bases by digest:** TODO, P2.

### M5 — API and compatibility freeze
- **T-050 — mechanical public API compatibility baseline/gate:** TODO, P1/P2.
- **T-051 — typed error taxonomy and stable/experimental surface classification:** TODO, P2, depends T-050.
- **T-052 — reduce/deprecate unnecessary root aliases before v1:** TODO, P2/P3, depends T-050/T-051.

### M6 — Documentation as executable contract
- **T-060 — repair current semantic documentation drift:** READY, P1/P2.
- **T-061 — docs/architecture drift guardrails:** TODO, P2, depends T-060.

### M7 — Operations and performance qualification
- **T-070 — bounded-cardinality metrics and readiness/liveness:** TODO, P2.
- **T-071 — recovery/corruption/lease/outbox/migration runbooks:** TODO, P2.
- **T-072 — versioned relative benchmark baseline/regression comparison:** TODO, P2.
- **T-073 — long-history/high-cardinality/reopen benchmarks:** TODO, P2/P3.

---

## 4. Rejected / deferred directions

- REJECTED: merge Core Axiom and ADGO into one mega-runtime.
- REJECTED: claim exactly-once external effects.
- REJECTED: weaken, skip, retry-away or remove tests/security checks to restore green.
- REJECTED: treat `FAIL -> retry -> PASS` as qualification for F-014.
- REJECTED: optimize before measurement or correctness closure.
- REJECTED: tag-push as a second release publication entrypoint.
- REJECTED: hidden create-or-upload/clobber recovery in normal release publication.
- DEFERRED: generated typed-activity specialization until profiling proves value after semantic freeze.

---

## 5. Iteration log

### Iteration 1 — T-001
- Result: authoritative plan created.
- Implementation commit: `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- Qualification: all repository gates PASS.
- Learning: self-SHA is recorded by the following synchronization checkpoint.

### Iteration 2 — T-002
- Root cause fixed: frozen release identity moved from inline YAML assumption to executable/tested resolver.
- Findings resolved: F-003, F-010.
- Mutation: ancestor-enforcement removal KILLED; forced-prerelease mutant KILLED.
- Implementation commit: `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.
- Qualification: `ci`, `security`, `quality-loop`, `module-checksum` PASS.

### Iteration 3 — T-003
- Root cause addressed: release verification now reuses current verification DAGs; publication is single-entrypoint and fail-closed.
- Findings addressed: F-004, F-011, F-012, F-013.
- Contract mutants killed: explicit target removal; generated-notes fallback; wrong verification checkout ref; tag-push reintroduction.
- Implementation commit: `8e1b11560305d56010049c992968c11f3197ca9e`.
- Post-push: `ci`, `security`, `module-checksum` PASS; `quality-loop` FAIL on pre-existing F-014, therefore T-003 remains VERIFYING rather than being falsely marked DONE.
- Process learning: a stricter verification DAG is valuable partly because it exposes latent flakiness; red-main classification must distinguish changed-code regression from pre-existing defect without relaxing the gate.

### Iteration 4 — T-004

Selected task: restore deterministic hedged timing and green `main`.

Why now: `quality-loop` is red; all unrelated work is prohibited until the failure is resolved.

Pre-flight contract:
- Root cause: asynchronous primary launch precedes semantic timer registration.
- Affected invariants: deterministic semantic time, hedge-delay contract, no flaky tests, red-main.
- Change surface: `adgo/speculation.go`, `MASTER_PLAN.md`.
- Protected surface: public API signatures, stores/persistence, Flow, release workflows, result selection/budget semantics, later hedge reset behavior.
- Observable contract: after primary starts, advancing an injected manual clock by exactly `HedgeDelay` launches the next hedge.
- Characterization: CI race/shuffle seed `1788202523607722540` timed out in `TestHedgedActivityDeterministicClock`; source history proves the defect predates T-003.
- Compatibility: no API or persisted-format change; only ordering is tightened to intended semantics.
- Failure modes: accidentally starting hedge countdown too late; changing reset behavior; leaking timer; changing cancellation/result-drain behavior.
- Rollback: revert this single logical commit if verification exposes a semantic regression.

Edge-space projection:
- INPUT: positive/default hedge delay, one/multiple variants.
- STATE: primary running/completed/cancelled before hedge.
- CONCURRENCY: primary goroutine scheduled before/after caller; race detector enabled.
- TIMING: manual clock advances immediately after primary-start observation; exact deadline boundary; wall-clock scheduling pressure.
- FAILURE: primary error/cancellation; hedge success/error.
- CONFIGURATION: injected TimerClock vs wall clock; MaxParallel boundaries.

Implementation:
- create the first `HedgeDelay` timer before launching the primary goroutine;
- leave all subsequent reset and result-selection logic unchanged.

Verification before push:
- exact failure evidence captured from GitHub Actions logs;
- fully green parent commit confirms F-014 is pre-existing, not caused by release changes;
- `gofmt` parses/formats the modified Go file successfully;
- full local repository execution is unavailable in the current isolated runtime because external DNS cannot clone GitHub; this is classified as environment limitation, not a PASS and not a reason to weaken gates;
- regression proof is the existing deterministic-clock test plus post-push race/shuffle quality-loop.

Mutation:
- conventional source mutation is not a reliable oracle for this scheduler-order invariant; the semantically meaningful attack is restoring `launch(primary) -> arm(timer)`, which is exactly the ordering proven flaky by the captured race/shuffle run. Post-push race/shuffle is the required test-of-tests signal.

SELF REVIEW before push:
- Root cause fixed: YES — timer registration can no longer lag behind observable primary start.
- New abstraction: NO.
- Coupling/dependency direction: unchanged.
- Security/persistence/API: unchanged.
- Performance: one timer is created at the same invocation boundary, only a few statements earlier; no new allocation or algorithmic work.
- Simplification: one ordering change plus explanatory invariant comment.
- Plan reconciliation: F-014/T-004 inserted ahead of T-040; T-003 stays VERIFYING until all gates become green.

Commit: <this commit>
Status: VERIFYING — pending atomic push and GitHub Actions qualification.

---

## 6. Continuation checkpoint

CURRENT HEAD: `8e1b11560305d56010049c992968c11f3197ca9e` before Iteration 4 push.
CURRENT QUALIFIED MILESTONE: M1 through T-002; T-003 code complete but not yet qualified.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external blocker;
- F-014 hedged timer-registration race — T-004 VERIFYING;
- F-004/F-011/F-013 — T-003 VERIFYING, expected to qualify after T-004 restores green;
- F-005 Core/ADGO durable primitive duplication;
- F-006 Flow crash-boundary proof gap;
- F-007 global gosec exclusions;
- F-008 no mechanical API compatibility gate.

BLOCKERS:
- T-010 requires external GitHub repository settings.
- T-040 and all unrelated work are temporarily blocked by red-main until T-004 qualifies.

NEXT TASK AFTER T-004 QUALIFIES:
- T-040 — remove one global gosec exclusion at a time with finding classification and negative-test evidence.

WHY NEXT:
- security blind spots are High severity, source-controlled and independently reducible; once the quality gate is trustworthy again, each rule can be an atomic iteration before the larger T-030 durability campaign.

CRITICAL FILES:
- `MASTER_PLAN.md`
- `adgo/speculation.go`
- `adgo/time_durable_test.go`
- `internal/durabletime/clock.go`
- `.github/workflows/quality-loop.yml`
- `.github/workflows/{ci,security,release}.yml`

VERIFICATION COMMANDS:
- `go test -race -run '^TestHedgedActivityDeterministicClock$' ./adgo`
- `bash scripts/quality_loop.sh fast`
- `go test -race ./...`
- repository `ci`, `security`, `quality-loop`, `module-checksum` workflows.
