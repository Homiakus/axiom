# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`
Target branch: `main`
Baseline reconstructed before plan bootstrap: `faf3fba67a377b354d8fa1a5745cef0b8b96f0c7`
Last verified completed-task commit before Iteration 2: `414f01c84ec215b29784cbfa7e5987cb35cdea41`
Last reconciliation: 2026-08-31

> This file is the only execution roadmap. `docs/PRODUCTION_STABILIZATION_PLAN.md`, `adgo/AGENT_PLATFORM_PLAN_RU.md`, audit reports and historical release documents are evidence/reference inputs, not parallel execution plans. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

Loop:

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

Rules:

1. Re-read remote `main`, this file, `CONTRIBUTING.md`, `DEVELOPMENT.md` and relevant contracts before substantial work.
2. Every substantial change has `T-XXX`; every substantial unexpected problem has `F-XXX` before the corresponding architecture/API/security/persistence fix.
3. Red `main` blocks unrelated implementation work.
4. One logical task = one logical commit containing implementation, tests, relevant docs and plan reconciliation whenever technically possible.
5. Never force-push.
6. Prefer executable invariants, root-cause fixes, fail-closed behavior and minimal reversible transitions.
7. Mutation testing is required where technically applicable to critical policy/state/validation logic. Performance work requires measurement first.
8. A commit cannot contain its own final SHA without changing that SHA. Therefore an iteration records `Commit: <this commit>` inside the commit and the actual remote SHA is written into the next synchronization checkpoint. This avoids a self-referential infinite commit chain.
9. After push, verify remote HEAD and CI state before treating the iteration as qualified.

Task states: `TODO`, `READY`, `IN_PROGRESS`, `VERIFYING`, `BLOCKED`, `DONE`, `DEFERRED`, `REJECTED`.
Finding states: `OPEN`, `INVESTIGATING`, `RESOLVED`, `ACCEPTED_RISK`, `REJECTED`.

---

## 1. Reconstructed current state

- `MASTER_PLAN.md` was absent at baseline and was bootstrapped by Iteration 1.
- `AGENTS.md` is absent; repository instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- `main` branch protection is disabled in GitHub metadata; this is an external blocker, not a source-code blocker.
- Baseline `ci`, `module-checksum`, `quality-loop` and scheduled `security` runs were green.
- Repository is pre-v1 and had no published GitHub Release at the audited baseline.
- Core Axiom declarative frontends converge to the compiled runtime; typed Flow is a separate reducer surface; ADGO is a separate durable graph/coordinator/worker runtime.

Critical invariants that must not be weakened:

- external effects are at-least-once, never falsely exactly-once;
- durable intent/task is persisted before external execution where the contract requires it;
- idempotency/reconciliation is explicit at external effect boundaries;
- stale workers cannot commit through fencing;
- execution/plan/schema identity is explicit and incompatible persisted formats fail closed;
- same-execution mutation is serialized/validated while independent executions may progress concurrently;
- semantic durable time comes from explicit clock abstractions;
- parsed syntax is not a runtime guarantee until executable behavior proves it;
- cross-platform/race/security gates are not weakened to regain green.

---

## 2. Findings

### F-001 — Missing authoritative living execution plan

**Status:** RESOLVED
**Category:** Engineering process
**Severity:** High
**Evidence:** `MASTER_PLAN.md` returned 404 on reconstructed baseline.
**Root cause:** topic/era plans accumulated without a final ownership boundary.
**Impact:** context recovery, duplicated work, stale status and parallel-roadmap ambiguity.
**Resolution:** T-001 established this file as the sole execution plan.

### F-002 — `main` is not protected

**Status:** OPEN / external configuration
**Category:** Governance / CI integrity
**Severity:** Critical process risk
**Evidence:** branch metadata reports `protected=false` and no required checks.
**Impact:** direct pushes can bypass otherwise strong source-controlled gates.
**Affected task:** T-010.

### F-003 — Manual release violates frozen-candidate policy

**Status:** OPEN, addressed by T-002/T-003
**Category:** Release / supply chain
**Severity:** High
**Evidence:** `docs/versioning.md` requires `release/<version>` candidate resolution; current manual workflow used checkout `HEAD`.
**Root cause:** policy evolved beyond executable workflow implementation.
**Impact:** wrong commit can be released despite correct review documentation.

### F-004 — Release verification is weaker than normal `main`

**Status:** OPEN
**Category:** CI/CD
**Severity:** High
**Evidence:** normal CI uses `go test -race ./...`; release verify races only selected packages and omits several main gates.
**Affected task:** T-003.

### F-005 — Core and ADGO duplicate durable primitives without executable anti-drift boundary

**Status:** OPEN
**Category:** Architecture
**Severity:** High
**Evidence:** overlapping retry/backoff, lease/fencing, clock, CAS/version, persisted-format and failure-classification concepts exist in both runtimes.
**Direction:** do not merge engines; inventory and extract only behavior-identical low-level primitives.
**Affected tasks:** T-020..T-023.

### F-006 — Durable Flow crash boundaries lack comprehensive failpoint qualification

**Status:** OPEN
**Category:** Persistence / reliability
**Severity:** High
**Impact:** duplicate external effects can only be trusted after intent/effect/ack crash boundaries are deterministically exercised.
**Affected tasks:** T-030..T-033.

### F-007 — Security workflow has broad global gosec exclusions

**Status:** OPEN
**Category:** Security
**Severity:** High
**Evidence:** global exclusions include `G101,G104,G115,G301,G302,G304,G306,G404,G703`.
**Direction:** enable one rule at a time, fix real findings, keep only local justified suppressions.
**Affected task:** T-040.

### F-008 — Public compatibility promises lack a mechanical API gate

**Status:** OPEN
**Category:** API / compatibility
**Severity:** High
**Impact:** pre-v1 evolution can silently break downstream consumers or documented contracts.
**Affected tasks:** T-050/T-051.

### F-009 — Documentation has semantic drift

**Status:** OPEN
**Category:** Documentation / process
**Severity:** Medium
**Evidence:** stale `first/latest` architecture text, README workflow badge path drift, DEVELOPMENT race-command drift.
**Affected tasks:** T-060/T-061.

### F-010 — Tag-trigger prerelease detection classifies every `v*` tag as prerelease

**Status:** IN_PROGRESS via T-002
**Category:** Release correctness
**Severity:** High
**Confidence:** High
**Evidence:** current workflow sets `pre=true` when `$ver` contains any ASCII letter; every valid tag starts with `v`.
**Observed behavior:** stable `v1.2.3` tag event is marked prerelease.
**Expected behavior:** tag-event prerelease state is derived from the SemVer prerelease component, not the leading `v`.
**Root cause:** loose text heuristic instead of SemVer parsing.
**Impact:** stable releases can be published with incorrect prerelease metadata.
**Affected task:** T-002.

### F-011 — Release publication path is more permissive than documented policy

**Status:** OPEN
**Category:** Release / supply chain
**Severity:** High
**Evidence:** publication falls back to generated notes when release notes are missing and uses create-or-upload behavior for an existing release, while `docs/versioning.md` says these cases are rejected.
**Root cause:** same policy/workflow drift family as F-003.
**Affected task:** T-003.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process

#### T-001 — Establish authoritative `MASTER_PLAN.md`

**Status:** DONE
**Priority:** P0 process foundation
**Result:** repository-first continuation state exists; historical plans are reference-only.
**Implementation commit verified after push:** `414f01c84ec215b29784cbfa7e5987cb35cdea41`.

#### T-010 — Protect `main` with required checks

**Status:** BLOCKED — external GitHub setting
**Priority:** P0
**Minimum external action:** enable branch/ruleset protection with exact active required checks, no force push/delete, and PR/review requirements appropriate to the repository.
**Independent work:** source-controlled hardening continues because the user explicitly requested direct-main execution while this external setting remains unavailable to the current tool surface.

### M1 — Release correctness and provenance

#### T-002 — Make frozen release metadata resolution executable and testable

**Status:** IN_PROGRESS
**Priority:** P0/P1
**Depends on:** T-001

Acceptance:
- strict SemVer tag validation before ref interpolation;
- manual dispatch must run from `main`;
- manual candidate resolves `release/<version>`, not checkout HEAD;
- missing release branch fails;
- candidate must be ancestor of remote `main`;
- existing remote tag fails;
- tag-event stable/prerelease classification follows SemVer, not arbitrary letters;
- deterministic synthetic-git regression suite covers success and negative cases;
- normal CI executes that suite and CI Completion Gate depends on it.

#### T-003 — Make publication/verification match the documented release contract

**Status:** TODO
**Priority:** P1
**Depends on:** T-002

Acceptance:
- release verification is at least as strong as main or reuses verified required workflows;
- release notes are required according to policy;
- existing tag/release is rejected unless an explicit separately designed recovery path is selected;
- release creation explicitly targets frozen SHA and remote tag SHA is verified after creation;
- provenance/attestation and permission scope are reviewed;
- docs and workflow say the same thing.

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

- **T-040 — remove broad gosec exclusions incrementally:** TODO, P1.
- **T-041 — minimize workflow permissions and pin container bases by digest:** TODO, P2.

### M5 — API and compatibility freeze

- **T-050 — mechanical public API compatibility baseline/gate:** TODO, P1/P2.
- **T-051 — typed error taxonomy and stable/experimental surface classification:** TODO, P2, depends T-050.
- **T-052 — reduce/deprecate unnecessary root aliases before v1:** TODO, P2/P3, depends T-050/T-051.

### M6 — Documentation as executable contract

- **T-060 — repair current semantic documentation drift:** TODO, P1/P2.
- **T-061 — add docs/architecture drift guardrails:** TODO, P2, depends T-060.

### M7 — Operations and performance qualification

- **T-070 — bounded-cardinality metrics and readiness/liveness:** TODO, P2.
- **T-071 — recovery/corruption/lease/outbox/migration runbooks:** TODO, P2.
- **T-072 — versioned relative benchmark baseline/regression comparison:** TODO, P2.
- **T-073 — long-history/high-cardinality/reopen benchmarks:** TODO, P2/P3.

---

## 4. Rejected / deferred directions

- REJECTED: merge Core Axiom and ADGO into one mega-runtime.
- REJECTED: claim exactly-once external effects.
- REJECTED: weaken tests/security checks to restore green.
- REJECTED: optimize before measurement or correctness closure.
- DEFERRED: generated typed-activity specialization until profiling proves value after semantic freeze.

---

## 5. Iteration log

### Iteration 1 — T-001

Selected task: bootstrap authoritative living plan.

SELF REVIEW
- Root cause fixed: YES.
- New findings: F-002..F-009 reconstructed from repository evidence.
- Tests/mutation/race/performance: not applicable to documentation-only bootstrap.
- Compatibility: unchanged.
- Process learning: continuation state must live in repository; self-SHA cannot be embedded in the same commit, so logs use `<this commit>` and next sync records the actual SHA.
- Commit: implementation commit later verified as `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- Push: `main`.
- Result: PASS for repository state; CI qualification is checked separately before subsequent writes.

### Iteration 2 — T-002

Selected task: frozen release metadata resolution.

Why now: highest-value source-controlled release correctness issue; T-010 is externally blocked.

Pre-flight contract:
- Root cause: security-critical release selection logic lives as untested inline YAML heuristics and diverged from release policy.
- Invariants: reviewed frozen candidate identity; SemVer integrity; ancestor relation to main; no duplicate remote tag; stable/prerelease classification correctness.
- Change surface: `scripts/resolve_release_candidate.sh`, `scripts/test_release_candidate.sh`, `.github/workflows/release.yml`, `.github/workflows/ci.yml`, `MASTER_PLAN.md`.
- Protected surface: Go runtime, public API, stores, persisted formats, ADGO/Flow semantics.
- Observable contract: manual dispatch targets frozen release branch; tag-trigger metadata marks stable tags stable.
- Characterization: existing manual path used `git rev-parse HEAD`; existing tag path treats leading `v` as evidence of prerelease.
- Compatibility: workflow behavior intentionally becomes stricter/fail-closed; runtime/API compatibility unchanged.
- Failure modes: refspec injection, stale local refs, missing branch, divergent candidate, duplicate tag, malformed SemVer, YAML syntax error.
- Rollback: revert this single release-tooling commit; no runtime/persisted data affected.
- Verification: synthetic local bare-remote suite, meaningful test-of-tests mutants, workflow parse sanity, then GitHub CI after push.

Edge-space projection:
- INPUT: stable/pre-release/build-metadata/malformed/leading-zero numeric prerelease.
- STATE: missing release branch / existing tag / frozen ancestor / divergent candidate.
- EXTERNAL STATE: local checkout refs stale or absent; resolver fetches exact remote refs.
- PERMISSIONS: manual dispatch on non-main fails.
- PLATFORM: release resolver is intentionally Linux/bash because release job is Ubuntu; consumer/runtime platform matrix remains unchanged.

Mutation target:
- ancestor enforcement removal must be killed;
- forcing stable tags to prerelease must be killed.

Local characterization/verification already executed before repository write:
- release candidate contract: PASS;
- removed ancestor enforcement mutant: KILLED;
- forced-prerelease mutant: KILLED.

Status: IN_PROGRESS — pending final remote synchronization, atomic commit and CI.
Commit: <this commit>

---

## 6. Continuation checkpoint

CURRENT HEAD: synchronize from remote before write; last verified completed-task commit `414f01c84ec215b29784cbfa7e5987cb35cdea41`.

CURRENT QUALIFIED MILESTONE: M1 — release correctness.

ARCHITECTURE:
- compiled Axiom runtime remains core business/state runtime;
- Flow remains separate reducer surface;
- ADGO remains separate durable graph/worker runtime;
- common durable extraction waits for contract inventory.

CRITICAL INVARIANTS:
- at-least-once + idempotency/reconciliation;
- stale-worker fencing;
- explicit durable identity/schema;
- code/tests outrank plans;
- red-main blocks unrelated work;
- no force push.

COMPLETED:
- T-001.

OPEN CRITICAL/HIGH:
- F-002 unprotected main (external);
- F-003/F-004/F-010/F-011 release correctness;
- F-005 duplicate durable primitives;
- F-006 Flow crash proof;
- F-007 security suppressions;
- F-008 API gate.

BLOCKERS:
- T-010 external repository setting.

NEXT TASK AFTER T-002:
- T-003 — strict release publication and verification parity.

WHY NEXT:
- same high-blast-radius release boundary; T-002 makes candidate identity testable, which unlocks safe publication hardening without mixing concerns.

CRITICAL FILES:
- `MASTER_PLAN.md`;
- `.github/workflows/release.yml`;
- `.github/workflows/ci.yml`;
- `scripts/resolve_release_candidate.sh`;
- `scripts/test_release_candidate.sh`;
- `docs/versioning.md`.

IMPORTANT DECISIONS:
- release ref input is validated before refspec construction;
- manual release candidate is a remote frozen branch, not local checkout state;
- stable tag prerelease state comes from SemVer prerelease component.

REJECTED OPTIONS:
- keep release logic embedded only in YAML;
- trust locally fetched branch refs without exact fetch;
- retry/fallback on invalid release metadata.
