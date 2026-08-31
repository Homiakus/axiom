# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`
Target branch: `main`
Baseline reconstructed from remote `main`: `faf3fba67a377b354d8fa1a5745cef0b8b96f0c7`
Last reconciliation: 2026-08-31

> This file is the only execution roadmap. Existing documents such as `docs/PRODUCTION_STABILIZATION_PLAN.md`, `adgo/AGENT_PLATFORM_PLAN_RU.md`, audit reports and historical release plans are evidence/reference inputs, not parallel execution plans. When they disagree with code, tests, current CI or this plan after reconciliation, observable behavior and executable invariants win.

---

## 0. Operating protocol

Work uses the loop:

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

Rules:

1. Read current remote `main`, this file, `CONTRIBUTING.md`, `DEVELOPMENT.md` and relevant architecture/runtime contracts before substantial work.
2. Every substantial change has a `T-XXX` task. Every substantial unexpected problem has an `F-XXX` finding.
3. Findings are recorded before architectural/public-API/security/persistence behavior is changed.
4. Red `main` blocks unrelated implementation work.
5. One logical task = one logical commit including tests/docs/plan reconciliation.
6. Never force-push.
7. Prefer root-cause removal over symptom patches and executable invariants over prose-only rules.
8. Mutation testing is required where technically useful for critical policy/state/validation logic.
9. Performance changes require a measured baseline.
10. After every successful push, record an iteration log and continuation checkpoint here.

Task states: `TODO`, `READY`, `IN_PROGRESS`, `VERIFYING`, `BLOCKED`, `DONE`, `DEFERRED`, `REJECTED`.

Finding states: `OPEN`, `INVESTIGATING`, `RESOLVED`, `ACCEPTED_RISK`, `REJECTED`.

---

## 1. Reconstructed repository state

### Current baseline

- Remote branch: `main`.
- Reconstructed HEAD before this plan existed: `faf3fba67a377b354d8fa1a5745cef0b8b96f0c7`.
- `MASTER_PLAN.md` was absent at that baseline.
- `AGENTS.md` is absent; repository instructions currently live primarily in `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- Current `main` branch metadata reports branch protection disabled.
- Latest inspected `ci`, `module-checksum`, `quality-loop` and scheduled `security` runs for the reconstructed baseline were green.
- Repository is pre-v1; no GitHub Release had been published at the audited baseline.

### Architectural shape

- Declarative Go `model`, AXM and TOML compile to the canonical Axiom plan/runtime.
- Typed `Flow` is a separate reducer-oriented API.
- `adgo` is a durable graph/coordinator/worker runtime for long-running and agent workloads.
- Core Axiom and ADGO intentionally remain separate engines, but duplicate durable primitives must not drift.
- Memory and Pebble are core store paths; ADGO additionally has File/Pebble production storage paths.

### Critical invariants already established

Do not weaken without a dedicated finding/task:

- external effects are at-least-once, never falsely exactly-once;
- durable intents/tasks are persisted before external execution where the durable contract requires it;
- external effect handlers require idempotency/reconciliation semantics;
- stale workers are fenced from committing late results;
- execution/plan identity is pinned and migration is explicit;
- persisted schemas/codecs fail closed on incompatible/future identity;
- same-execution mutation is serialized/validated while independent executions may progress concurrently;
- semantic orchestration time uses explicit clock abstractions where durable decisions depend on time;
- red cross-platform CI is a production regression;
- parsed syntax is not documented as a runtime guarantee until code/tests prove it.

---

## 2. Findings

### F-001 — Missing authoritative living execution plan

**Status:** RESOLVED by `T-001`

**Category:** Engineering process / planning

**Severity:** High

**Confidence:** High

**Evidence:** `MASTER_PLAN.md` returned not found on reconstructed `main`; execution state was split across `docs/PRODUCTION_STABILIZATION_PLAN.md`, ADGO plans, audits and release docs.

**Observed behavior:** a new engineering session could not reconstruct one authoritative task/finding/checkpoint state from the repository.

**Expected behavior:** one living plan contains priorities, dependencies, findings, iteration logs and continuation checkpoints.

**Root cause:** planning artifacts accumulated by topic/era without a final ownership boundary.

**Impact:** duplicated work, stale task status, plan worship, lost findings and expensive context recovery.

**Affected invariants:** repository recoverability; single source of truth; autonomous continuation.

**Recommended direction:** keep this file authoritative; historical plans become evidence/reference only.

---

### F-002 — `main` is not protected

**Status:** OPEN / external configuration

**Category:** Governance / CI integrity

**Severity:** Critical process risk

**Confidence:** High

**Evidence:** GitHub branch metadata reports `protected=false` and no required status checks.

**Observed behavior:** a direct push can update `main` without GitHub enforcing the otherwise strong CI/security gates.

**Expected behavior:** required checks and safe branch rules mechanically protect `main`.

**Root cause:** repository settings are weaker than the source-controlled verification DAG.

**Impact:** one accidental or compromised push can bypass correctness/security gates and publish incompatible persisted/runtime behavior.

**Blast radius:** entire repository and downstream consumers.

**Affected tasks:** `T-010`.

**Recommended direction:** enable branch/ruleset protection with exact active check names; keep a source-controlled validation/checklist for the external setting.

---

### F-003 — Manual release does not implement the documented frozen-SHA contract

**Status:** OPEN

**Category:** Release / supply chain

**Severity:** High

**Confidence:** High

**Evidence:** `docs/versioning.md` says manual release resolves `release/<version>` and publishes that reviewed SHA; `.github/workflows/release.yml` currently derives manual `target_sha` from checkout `HEAD`.

**Observed behavior:** workflow-dispatch source revision can become the release target instead of the frozen release branch.

**Expected behavior:** malformed/missing/non-ancestor candidate is rejected and release/tag is pinned explicitly to the frozen candidate SHA.

**Root cause:** release policy evolved beyond workflow implementation.

**Impact:** wrong commit can be released despite correct documentation/review process.

**Affected tasks:** `T-002`, `T-003`.

**Recommended direction:** characterize metadata resolution in a testable script, make workflow consume it, verify tag target explicitly.

---

### F-004 — Release verification is weaker than normal `main` verification

**Status:** OPEN

**Category:** CI/CD

**Severity:** High

**Confidence:** High

**Evidence:** normal CI runs `go test -race ./...`; release workflow currently races only root/internal runtime/store subsets and does not reproduce all compatibility/security gates.

**Impact:** a candidate can pass release verification while failing a gate normally expected on `main`.

**Affected tasks:** `T-003`.

---

### F-005 — Core and ADGO duplicate durable primitives without an executable anti-drift boundary

**Status:** OPEN

**Category:** Architecture

**Severity:** High

**Confidence:** High

**Evidence:** both runtimes own overlapping retry/backoff, lease/fencing, clock, version/CAS, persistence-format and failure-classification concepts; existing stabilization roadmap already identified `ARCH-001..004` but extraction is unfinished.

**Expected behavior:** engines remain separate while genuinely identical low-level durable primitives have one implementation/contract and dependency tests prevent reverse coupling.

**Root cause:** ADGO grew rapidly as a production runtime before common low-level contracts were fully frozen.

**Impact:** semantic drift and duplicate bug fixes.

**Affected tasks:** `T-020` through `T-023`.

---

### F-006 — Durable Flow crash boundaries are not comprehensively failpoint-qualified

**Status:** OPEN

**Category:** Persistence / reliability

**Severity:** High

**Confidence:** High

**Evidence:** durable outbox exists, but deterministic crash/failpoint coverage across intent commit, effect call, success-before-ack and ack commit remains outstanding in the stabilization evidence.

**Impact:** at-least-once behavior may be correct by design yet insufficiently proven at the exact boundaries that can duplicate real-world effects.

**Affected tasks:** `T-030` through `T-033`.

---

### F-007 — Security scan has broad global gosec exclusions

**Status:** OPEN

**Category:** Security

**Severity:** High

**Confidence:** High

**Evidence:** security workflow globally excludes `G101,G104,G115,G301,G302,G304,G306,G404,G703`.

**Expected behavior:** rules are enabled; intentional exceptions are local and justified.

**Impact:** green security status has known blind spots, especially relevant to filesystem/storage paths.

**Affected tasks:** `T-040`.

---

### F-008 — Public compatibility promises lack a mechanical API gate

**Status:** OPEN

**Category:** API / compatibility

**Severity:** High

**Confidence:** High

**Evidence:** versioning policy declares exported identifiers, runtime guarantees, diagnostic codes, persisted formats and generated contracts public, while no API-manifest/apidiff-style required job is established.

**Affected tasks:** `T-050`, `T-051`.

---

### F-009 — Documentation has semantic drift from implementation/CI

**Status:** OPEN

**Category:** Documentation / process

**Severity:** Medium

**Confidence:** High

**Evidence examples:** `ARCHITECTURE.md` contains stale production semantics for `first/latest`; README CI badge references `test.yml` while active workflow is `ci.yml`; `DEVELOPMENT.md` describes a narrower race command than current `ci.yml`.

**Impact:** maintainers and consumers may choose behavior based on obsolete contracts.

**Affected tasks:** `T-060`, `T-061`.

---

## 3. Prioritized task DAG

### Milestone M0 — Trustworthy execution process

#### T-001 — Establish authoritative `MASTER_PLAN.md`

**Status:** IN_PROGRESS

**Priority:** P0 process foundation

**Depends on:** none

**Goal:** make repository state reconstructable without conversation memory and remove parallel-roadmap ambiguity.

**Acceptance:**
- this file exists on `main`;
- findings and tasks are evidence-based;
- previous stabilization/agent plans are explicitly reference-only;
- first iteration log and checkpoint are recorded;
- future substantial commits reconcile this file.

#### T-010 — Protect `main` with required checks

**Status:** BLOCKED (external GitHub repository setting)

**Priority:** P0

**Depends on:** exact current workflow/check-name inventory

**Minimum external action:** enable a branch/ruleset that requires active CI/security compatibility gates, disallows force push/delete, and requires PR/review as appropriate.

**Independent work:** all source-controlled hardening tasks can proceed; direct-main pushes are performed only because the user explicitly requested them while protection remains external/unconfigured.

---

### Milestone M1 — Release correctness and provenance

#### T-002 — Make frozen release candidate resolution executable and testable

**Status:** READY

**Priority:** P0/P1

**Depends on:** T-001

**Goal:** eliminate policy/workflow drift for manual releases.

**Acceptance:**
- SemVer input validated;
- dispatch/manual candidate resolves `release/<version>` rather than arbitrary checkout HEAD;
- missing branch is rejected;
- candidate must be ancestor of current `main`;
- duplicate tag/release policy is explicit;
- release target SHA becomes deterministic/testable outside YAML where practical;
- regression/negative tests cover malformed version, missing branch and non-ancestor candidate.

#### T-003 — Make release verification at least as strong as `main`

**Status:** TODO

**Priority:** P1

**Depends on:** T-002

**Acceptance:** release verification includes module hygiene, vet, full tests, full relevant race, compatibility/codegen/security gates or verified reusable equivalents; release creation explicitly targets the frozen SHA; tag SHA is verified after creation; provenance/attestation strategy is documented/implemented.

---

### Milestone M2 — Durable correctness closure

#### T-030 — Build reusable deterministic failpoint framework for durable boundaries

**Status:** TODO

**Priority:** P1

**Depends on:** T-001

#### T-031 — Qualify durable Flow intent/effect/ack crash matrix

**Status:** TODO

**Priority:** P1

**Depends on:** T-030

**Required boundaries:** before state+intent commit; after commit before effect; during effect; after effect success before ack; during ack; after ack; reopen/recovery.

#### T-032 — Add no-resurrection/backend/crash equivalence properties

**Status:** TODO

**Priority:** P1/P2

**Depends on:** T-030

#### T-033 — Add Flow outbox backpressure and operational observability contracts

**Status:** TODO

**Priority:** P2

**Depends on:** T-031

---

### Milestone M3 — Shared durable primitives without merging engines

#### T-020 — Inventory Core vs ADGO durable primitive contracts

**Status:** TODO

**Priority:** P1 architectural foundation

**Depends on:** correctness contracts for touched primitives must be green

**Classify:** retry/backoff; lease/fencing; durability capability; CAS/version semantics; clock adapters; persisted-format validation; lock ownership; error classification.

#### T-021 — Define acyclic `internal/durable` dependency boundary

**Status:** TODO

**Priority:** P1/P2

**Depends on:** T-020

#### T-022 — Extract only behavior-identical pure durable primitives

**Status:** TODO

**Priority:** P2

**Depends on:** T-021

**Protected decision:** do not merge Core Axiom Engine and ADGO Engine into a single mega-runtime.

#### T-023 — Add architecture dependency/anti-drift tests

**Status:** TODO

**Priority:** P2

**Depends on:** T-021

---

### Milestone M4 — Security boundary reduction

#### T-040 — Remove broad gosec exclusions incrementally

**Status:** TODO

**Priority:** P1

**Method:** enable one rule at a time; classify findings; fix real issues; replace intentional cases with narrow local justified suppression; add negative/security tests when a real boundary is found.

#### T-041 — Minimize workflow permissions and pin container bases by digest

**Status:** TODO

**Priority:** P2

**Depends on:** T-040 may be independent by file scope; keep separate commits.

---

### Milestone M5 — API and compatibility freeze

#### T-050 — Add mechanical exported API compatibility baseline/gate

**Status:** TODO

**Priority:** P1/P2

**Acceptance:** additive/change/removal/deprecation can be distinguished; intentional pre-v1 break requires explicit changelog/migration annotation.

#### T-051 — Publish stable typed error taxonomy and compatibility surface classification

**Status:** TODO

**Priority:** P2

**Depends on:** T-050

#### T-052 — Reduce/deprecate unnecessary root facade aliases before v1

**Status:** TODO

**Priority:** P2/P3

**Depends on:** T-050, T-051

---

### Milestone M6 — Documentation as executable contract

#### T-060 — Repair current semantic documentation drift

**Status:** TODO

**Priority:** P1/P2

**Scope:** architecture `first/latest`, README workflow badge, development race/full-CI descriptions, historical audit status.

#### T-061 — Add docs/architecture drift guardrails for key capabilities

**Status:** TODO

**Priority:** P2

**Depends on:** T-060

---

### Milestone M7 — Operations and performance qualification

#### T-070 — Define bounded-cardinality operational metrics and readiness/liveness

**Status:** TODO

**Priority:** P2

#### T-071 — Complete recovery/corruption/lease/outbox/migration runbooks

**Status:** TODO

**Priority:** P2

#### T-072 — Add versioned relative benchmark baseline/regression comparison

**Status:** TODO

**Priority:** P2

#### T-073 — Add long-history/high-cardinality/reopen benchmarks

**Status:** TODO

**Priority:** P2/P3

**Datasets:** 1K/10K/100K history entries; large task queues/inboxes/outbox; many executions; reopen/recovery cost.

---

## 4. Deferred / rejected directions

- **REJECTED:** merge Core Axiom and ADGO into one generalized mega-runtime. They have different orchestration models; share only proven identical primitives.
- **REJECTED:** claim exactly-once external effects. Preserve at-least-once + idempotency/reconciliation.
- **REJECTED:** weaken or skip cross-platform/race/security tests to restore green.
- **REJECTED:** optimize before measurement or before correctness contracts are green.
- **DEFERRED:** generator specialization for typed activities until generic typed mapping remains semantically frozen and profiling proves need.

---

## 5. Convergence criterion

The repository converges only when:

- Critical findings = 0;
- High findings = 0 or explicitly accepted/deferred with evidence;
- P0 closed; P1 closed or justified;
- `main` is mechanically protected and CI is trustworthy;
- no unexplained flaky/red gates;
- race/security/compatibility gates are green;
- critical durable crash/recovery boundaries are failpoint/property qualified;
- public/persisted contracts have compatibility protection;
- Core/ADGO duplicate durable architecture has an explicit anti-drift boundary;
- security suppressions are narrow and justified;
- benchmark regressions are measured against reviewed baselines;
- docs match executable behavior;
- re-audit finds no new fundamental architecture/correctness/security/reliability problem;
- final verified state is on `main`.

---

## 6. Iteration log

### Iteration 1 — T-001

**Task:** establish the living master execution plan.

**Findings addressed:**
- F-001.

**Unexpected findings incorporated:**
- F-002 through F-009 reconstructed from current repository/code/CI evidence rather than conversation memory.

**Pre-flight contract:**
- Root cause: no single repository-owned execution state.
- Affected invariants: recoverability, task/finding ownership, source-of-truth hierarchy.
- Change surface: new `MASTER_PLAN.md` only.
- Protected surface: all Go code, public API, persistence, CI workflows, release workflow.
- Observable contract: a fresh session can recover priorities/findings/checkpoint from repository.
- Characterization: `MASTER_PLAN.md` absent on reconstructed baseline.
- Compatibility: none; documentation/process-only.
- Failure modes: duplicate roadmap status, stale evidence, overclaiming completed work.
- Rollback: delete this file and revert commit; no runtime state affected.
- Verification: fetch file from remote `main`, verify commit/head and internal task/finding consistency.

**Edge-space projection:** repository changed concurrently / stale baseline / missing prior plan / conflicting historical plan / external branch settings. No production runtime edge-space is touched.

**Mutation:** not applicable to documentation-only bootstrap.

**Race/security/performance:** not applicable to production behavior; security/governance findings are recorded without changing policy in this commit.

**Process improvement:** future session recovery is now repository-first and historical roadmaps are demoted to evidence/reference.

**Commit:** pending

**Push:** `main`

**Result:** VERIFYING

---

## 7. Continuation checkpoint

CURRENT HEAD: pending Iteration 1 commit verification

CURRENT QUALIFIED MILESTONE: M0 — trustworthy execution process

ARCHITECTURE:
- canonical compiled Axiom runtime remains the business/state-machine core;
- typed Flow remains a separate lightweight reducer surface;
- ADGO remains a separate durable graph/worker runtime;
- shared low-level durable primitives may be extracted only after contract inventory.

CRITICAL INVARIANTS:
- at-least-once external effects + idempotency/reconciliation;
- fencing against stale workers;
- explicit durable plan/schema identity;
- red-main blocks unrelated changes;
- no force push;
- code/tests outrank plans/docs.

COMPLETED THIS ITERATION:
- T-001 in verification.

RESOLVED FINDINGS:
- F-001 when remote commit is confirmed.

OPEN CRITICAL/HIGH FINDINGS:
- F-002 unprotected main (external);
- F-003 frozen-SHA release mismatch;
- F-004 weaker release verification;
- F-005 duplicate durable primitives without anti-drift boundary;
- F-006 Flow crash-boundary proof gap;
- F-007 global gosec exclusions;
- F-008 no mechanical public API compatibility gate.

BLOCKERS:
- T-010 requires external repository settings; source work can proceed independently.

NEXT TASK:
- T-002 — frozen release candidate resolution.

WHY NEXT:
- it is a high-confidence release correctness root cause, source-controlled, atomic, high blast-radius reduction, and unlocks trustworthy release verification.

CRITICAL FILES:
- `MASTER_PLAN.md`;
- `.github/workflows/release.yml`;
- `docs/versioning.md`;
- `CONTRIBUTING.md`;
- `DEVELOPMENT.md`.

VERIFICATION COMMANDS / GATES:
- targeted release metadata tests to be introduced by T-002;
- normal `go test ./...` / CI when production/release tooling is changed;
- workflow syntax and current Actions result after push.

IMPORTANT DECISIONS:
- one execution plan only;
- do not merge Axiom and ADGO engines;
- release correctness precedes new feature work.

REJECTED OPTIONS:
- continue using `docs/PRODUCTION_STABILIZATION_PLAN.md` as a second active roadmap;
- fix unrelated documentation/runtime items before closing the release target root cause.

NEW PROCESS LEARNING:
- plans must contain a repository-resident continuation checkpoint, not rely on conversation history.
