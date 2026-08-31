# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`  
Target branch: `main`  
Last qualified HEAD before Iteration 6: `d611198a92f17011e487f5dba942bd2933da4a7a`  
Last reconciliation: 2026-08-31

> This file is the only execution roadmap. Historical plans, audits and topic-specific documents are evidence inputs, not parallel execution plans. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Reconstruct state from remote `main`, this file, repository instructions and current CI before substantial work.
2. Substantial work uses `T-XXX`; substantial unexpected information uses `F-XXX` before architecture/API/security/persistence behavior is changed.
3. Red `main` blocks unrelated work. Flaky `FAIL -> retry -> PASS` is not a pass.
4. One logical iteration = one logical commit with implementation, executable evidence, relevant docs and plan reconciliation whenever possible.
5. Never force-push.
6. Prefer root-cause fixes, executable invariants, fail-closed behavior and small reversible transitions.
7. Use mutation testing where it distinguishes semantic contracts; use deterministic clocks/schedulers plus race/shuffle for timing/concurrency contracts.
8. Security suppressions must be scoped, mechanically constrained and justified. Broad global suppressions are temporary debt.
9. Performance work requires measurement first.
10. Iteration logs use `Commit: <this commit>`; the following synchronization checkpoint records the actual verified SHA because a commit cannot embed its own final SHA.
11. An iteration is qualified only after remote HEAD plus relevant `ci`, `security`, `quality-loop`, and `module-checksum` gates are green.

Task states: `TODO`, `READY`, `IN_PROGRESS`, `VERIFYING`, `BLOCKED`, `DONE`, `DEFERRED`, `REJECTED`.  
Finding states: `OPEN`, `INVESTIGATING`, `VERIFYING`, `RESOLVED`, `ACCEPTED_RISK`, `REJECTED`.

---

## 1. Architecture and critical invariants

- Declarative Go `model`, AXM and TOML converge on the canonical compiled Axiom runtime.
- Typed `Flow` and `adgo` remain separate orchestration surfaces; do not merge them into a mega-runtime.
- Share only behavior-identical low-level durable primitives after executable characterization.
- External effects are at-least-once, never falsely exactly-once; idempotency/reconciliation is explicit.
- Durable intents/tasks are persisted before external execution where required.
- Stale workers cannot commit through fencing.
- Execution/plan/schema identity is explicit; incompatible persisted formats fail closed.
- Same-execution mutation is serialized/validated; independent executions may progress concurrently.
- Semantic durable time uses explicit clock abstractions; governing timers are armed before the operation becomes observably started.
- Retry jitter is deterministic by execution/node identity and attempt and is never used as security entropy.
- Security-sensitive random identifiers use cryptographic randomness.
- Production source must not embed credentials; tests may use obvious fixtures, and current gosec does not scan `_test.go` unless explicitly enabled.
- Parsed syntax is not a runtime guarantee until executable behavior proves it.
- Cross-platform/race/security/quality gates are never weakened to regain green.

Repository/process state:

- `MASTER_PLAN.md` is authoritative; `AGENTS.md` is absent; active instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` protection disabled; T-010 is an external blocker.
- `d611198a92f17011e487f5dba942bd2933da4a7a` is fully qualified: full CI/race, security, quality-loop and module checksum PASS.
- G404 is enabled repository-wide with one path-scoped, sentinel-constrained deterministic-jitter exception.
- No GitHub Release had been published at the audited baseline.

---

## 2. Findings

### F-001 — Missing authoritative living execution plan
**Status:** RESOLVED by T-001  
**Severity:** High

### F-002 — `main` is not protected
**Status:** OPEN / external configuration  
**Category:** Governance / CI integrity  
**Severity:** Critical process risk  
**Evidence:** branch metadata reports `protected=false`.  
**Task:** T-010.

### F-003 — Manual release selected caller HEAD instead of frozen candidate
**Status:** RESOLVED by T-002  
**Severity:** High

### F-004 — Release verification was weaker than normal `main`
**Status:** RESOLVED by T-003/T-004  
**Severity:** High

### F-005 — Core and ADGO duplicate durable primitives without executable anti-drift boundary
**Status:** OPEN  
**Category:** Architecture  
**Severity:** High  
**Tasks:** T-020..T-023.

### F-006 — Durable Flow crash boundaries lack comprehensive failpoint qualification
**Status:** OPEN  
**Category:** Persistence / reliability  
**Severity:** High  
**Tasks:** T-030..T-033.

### F-007 — Security workflow still has broad global gosec exclusions
**Status:** OPEN  
**Category:** Security / signal quality  
**Severity:** High  
**Current remaining global exclusions after T-040:** `G101,G104,G115,G301,G302,G304,G306,G703` before this iteration; T-042 removes G101.  
**Direction:** one rule per atomic iteration, based on actual finding/signal analysis.

### F-008 — Public compatibility promises lack a mechanical API gate
**Status:** OPEN  
**Category:** API / compatibility  
**Severity:** High  
**Tasks:** T-050/T-051.

### F-009 — Documentation has semantic drift
**Status:** OPEN  
**Category:** Documentation / process  
**Severity:** Medium  
**Tasks:** T-060/T-061.

### F-010 — Tag prerelease detection treated every `v*` tag as prerelease
**Status:** RESOLVED by T-002/T-003.

### F-011 — Release publication was more permissive than documented policy
**Status:** RESOLVED by T-003/T-004.

### F-012 — Security workflow requested unused `security-events: write`
**Status:** RESOLVED by T-003.

### F-013 — Tag-push release could execute obsolete release tooling
**Status:** RESOLVED by T-003/T-004.

### F-014 — Hedged activity timer-registration race
**Status:** RESOLVED by T-004  
**Evidence:** quality-loop shuffle seed `1788202523607722540`; fixed by arming first timer before primary launch.  
**Qualified commit:** `3a0ba3034f9202a38108fe412286f4e337a90f21`.

### F-015 — One deterministic `math/rand` use caused repository-wide G404 blindness
**Status:** RESOLVED by T-040  
**Severity:** High  
**Resolution:** G404 enabled globally; only `adgo/runtime.go` is path-scoped, with executable policy that fails if math/rand usage expands.  
**Qualified commit:** `d611198a92f17011e487f5dba942bd2933da4a7a`.

### F-016 — G101 hardcoded-credential detection is globally disabled without a production false positive

**Status:** VERIFYING via T-042  
**Category:** Security / credential leakage / signal quality  
**Severity:** High  
**Confidence:** High

**Evidence:** G101 remained in the global gosec exclusion list. Pre-flight code search found no production Go `password` literal assignment, no `token := "..."`, no `secret := "..."`; literal `BearerToken: "..."` appears only in `_test.go`. Gosec v2.28 G101 inspects credential-like identifiers plus high-entropy/specific secret patterns; current security workflow does not enable test-file scanning.

**Observed behavior:** a future hardcoded production credential can bypass G101 by configuration, leaving only other scanners to detect it.

**Expected behavior:** G101 runs repository-wide with no exception; policy guard prevents global re-suppression.

**Root cause:** inherited broad SAST exclusion outlived the code conditions that might have justified it.

**Impact:** defense-in-depth gap for AST/context-aware credential detection.

**Task:** T-042.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process

- **T-001 — authoritative `MASTER_PLAN.md`: DONE**, commit `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- **T-004 — deterministic hedge timing: DONE**, commit `3a0ba3034f9202a38108fe412286f4e337a90f21`, all gates PASS.
- **T-010 — protect `main`: BLOCKED**, external GitHub repository setting.

### M1 — Release correctness and provenance

- **T-002 — frozen release metadata resolution: DONE**, commit `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.
- **T-003 — single fail-closed publication/verification contract: DONE**, commit `8e1b11560305d56010049c992968c11f3197ca9e`, qualified after independent F-014 was removed.

### M2 — Durable correctness closure

- **T-030 — deterministic durable failpoint framework:** TODO, P1.
- **T-031 — Flow intent/effect/ack crash matrix:** TODO, P1, depends T-030.
- **T-032 — no-resurrection/backend/crash equivalence properties:** TODO, P1/P2, depends T-030.
- **T-033 — Flow backlog/backpressure/observability contracts:** TODO, P2, depends T-031.

### M3 — Shared durable primitives without merging engines

- **T-020 — inventory Core vs ADGO durable primitive contracts:** TODO, P1.
- **T-021 — define acyclic shared durable boundary:** TODO, P1/P2, depends T-020.
- **T-022 — extract behavior-identical pure primitives:** TODO, P2, depends T-021.
- **T-023 — architecture anti-drift tests:** TODO, P2, depends T-021.

### M4 — Security boundary reduction

#### T-040 — Enable G404 globally and constrain deterministic retry-jitter exception
**Status:** DONE  
**Finding:** F-015  
**Commit:** `d611198a92f17011e487f5dba942bd2933da4a7a`  
**Qualification:** policy sentinel, gosec G404, govulncheck, Gitleaks, full CI/race, quality-loop and module checksum PASS.

#### T-042 — Enable G101 globally without exceptions
**Status:** VERIFYING  
**Priority:** P1  
**Finding:** F-016

Acceptance:
- G101 is removed from global `-exclude`;
- no G101 path/inline exception is introduced;
- existing G404 scoped policy remains intact;
- policy sentinel rejects re-adding G101 or G404 to global exclusions;
- current production code remains unchanged;
- security workflow and all repository gates pass post-push.

#### T-043 — Classify and remove the next global gosec exclusion
**Status:** READY after T-042 qualifies  
**Priority:** P1  
**Selection:** inspect actual source/finding semantics first. Permission rules need explicit public-artifact vs secret-state policy; taint rules must not be mass-enabled blindly.

#### T-041 — Supply-chain provenance, remaining workflow permission minimization, container digest pinning
**Status:** TODO, P2.

### M5 — API and compatibility freeze

- **T-050 — mechanical public API compatibility gate:** TODO, P1/P2.
- **T-051 — typed error taxonomy and stable/experimental classification:** TODO, P2.
- **T-052 — reduce/deprecate unnecessary root aliases:** TODO, P2/P3.

### M6 — Documentation as executable contract

- **T-060 — repair semantic documentation drift:** READY, P1/P2.
- **T-061 — docs/architecture drift guardrails:** TODO, P2.

### M7 — Operations and performance qualification

- **T-070 — bounded-cardinality metrics/readiness/liveness:** TODO, P2.
- **T-071 — recovery/corruption/lease/outbox/migration runbooks:** TODO, P2.
- **T-072 — relative benchmark baseline/regression comparison:** TODO, P2.
- **T-073 — long-history/high-cardinality/reopen benchmarks:** TODO, P2/P3.

---

## 4. Rejected / deferred directions

- REJECTED: merge Core and ADGO into one mega-runtime.
- REJECTED: exactly-once claims for external effects.
- REJECTED: retry-away/disable/weaken tests or scanners to regain green.
- REJECTED: replace deterministic jitter with crypto randomness or a home-grown PRNG merely to silence SAST.
- REJECTED: leave G404 globally disabled for one deterministic false positive.
- REJECTED: keep G101 globally disabled when characterization finds no production exception requirement.
- REJECTED: tag-push as a second release publisher.
- DEFERRED: generated typed-activity specialization until profiling proves value.

---

## 5. Iteration log

### Iteration 1 — T-001
Authoritative plan established; commit `414f01c84ec215b29784cbfa7e5987cb35cdea41`; all gates PASS.

### Iteration 2 — T-002
Frozen release metadata made executable/testable; F-003/F-010 resolved; semantic mutants killed; commit `094de51e4e42d72d4bdb4f813f342cee71f9ac87`; all gates PASS.

### Iteration 3 — T-003
Release verification reuses current DAGs and publication is fail-closed/single-entrypoint; F-004/F-011/F-012/F-013 resolved; commit `8e1b11560305d56010049c992968c11f3197ca9e`.

### Iteration 4 — T-004
Pre-existing deterministic-clock flake found by quality-loop and root-caused to timer registration order; commit `3a0ba3034f9202a38108fe412286f4e337a90f21`; full CI/race/security/quality PASS.

### Iteration 5 — T-040
- Root cause: one deterministic retry-jitter `math/rand` use caused repository-wide G404 suppression.
- Change: G404 enabled globally; only `adgo/runtime.go` receives path-scoped G404; sentinel constrains approved RNG API set and strict `#nosec` metadata policy.
- Test-of-tests: invalid initial fixture evidence caused by permission error was discarded; corrected fixture PASS; global-G404 mutant KILLED; added-rand.Intn mutant KILLED.
- Commit: `d611198a92f17011e487f5dba942bd2933da4a7a`.
- Qualification: policy sentinel, gosec, govulncheck, Gitleaks, full CI/race, quality-loop, module checksum PASS.
- Process learning: security exception scope needs its own executable invariant, not just reviewer memory.

### Iteration 6 — T-042

Selected task: enable G101 hardcoded-credential detection globally.

Why now: it removes another High-severity false-green class without production behavior changes or known exception debt.

Pre-flight contract:
- Root cause: inherited global G101 exclusion has no current production false-positive evidence.
- Invariants: production credentials are external inputs, never source literals; test fixtures do not define production policy.
- Change surface: `.github/workflows/security.yml`, `scripts/test_gosec_policy.sh`, `MASTER_PLAN.md`.
- Protected surface: all Go production code, public API, persistence, runtime/retry/release behavior.
- Observable contract: G101 scans production repository code; no G101 exception exists.
- Characterization: production password literal search empty; production `token := "..."`/`secret := "..."` empty; literal BearerToken found only in test code; G101 rule semantics verified against gosec v2.28 source.
- Compatibility: none; scanner policy only.
- Failure modes: hidden production high-entropy credential fixture causes real finding; policy sentinel incorrectly permits re-exclusion; unrelated scanner regression.
- Rollback: revert one policy commit if evidence shows an intentional production literal that needs explicit redesign/classification.

Verification before push:
- updated policy script shell syntax PASS;
- fixture PASS;
- mutant restoring global G101 exclusion KILLED;
- regression mutant restoring global G404 exclusion KILLED.

SELF REVIEW before push:
- Root cause fixed for G101: YES if post-push gosec remains green with no exception.
- Production code changed: NO.
- New hidden state/source of truth: NO; existing security workflow remains authoritative and sentinel only constrains it.
- Security signal: stronger; Gitleaks remains independent defense in depth.
- Remaining exclusions: `G104,G115,G301,G302,G304,G306,G703`, tracked by F-007/T-043.

Commit: <this commit>  
Status: VERIFYING — pending atomic push and qualification.

---

## 6. Continuation checkpoint

CURRENT HEAD BEFORE ITERATION 6 PUSH: `d611198a92f17011e487f5dba942bd2933da4a7a`  
CURRENT QUALIFIED MILESTONE: release correctness closed; deterministic quality gate restored; G404 suppression debt closed.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external blocker;
- F-005 Core/ADGO durable duplication;
- F-006 Flow crash-boundary proof gap;
- F-007 remaining global gosec exclusions;
- F-008 no mechanical API compatibility gate;
- F-016 G101 enablement — VERIFYING.

NEXT TASK AFTER T-042 QUALIFIES:
- T-043 — choose the next global gosec exclusion using evidence, not numeric order.

WHY NEXT:
- security scanner false-green classes are currently cheap to remove atomically; once noisy permission/taint rules require architectural changes, priority must be recalculated against T-030 durability and T-050 compatibility gates.

VERIFICATION:
- `bash scripts/test_gosec_policy.sh`
- security workflow, especially `SAST Scan (gosec)`
- full repository `ci`, `quality-loop`, `module-checksum`.
