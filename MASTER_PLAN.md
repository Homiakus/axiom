# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`
Target branch: `main`
Baseline before plan bootstrap: `faf3fba67a377b354d8fa1a5745cef0b8b96f0c7`
Last qualified implementation commit: `094de51e4e42d72d4bdb4f813f342cee71f9ac87`
Last reconciliation: 2026-08-31

> This file is the only execution roadmap. `docs/PRODUCTION_STABILIZATION_PLAN.md`, `adgo/AGENT_PLATFORM_PLAN_RU.md`, audit reports and historical release documents are evidence/reference inputs, not parallel execution plans. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Re-read remote `main`, this file, `CONTRIBUTING.md`, `DEVELOPMENT.md` and relevant contracts before substantial work.
2. Every substantial change has `T-XXX`; every substantial unexpected problem has `F-XXX` before architecture/API/security/persistence behavior is changed.
3. Red `main` blocks unrelated work.
4. One logical task = one logical commit containing implementation, tests, relevant docs and plan reconciliation whenever technically possible.
5. Never force-push.
6. Prefer executable invariants, root-cause fixes, fail-closed behavior and small reversible transitions.
7. Critical policy/state/validation logic uses mutation testing where applicable. Performance changes require measurement first.
8. A commit cannot embed its own final SHA without changing that SHA. An iteration therefore records `Commit: <this commit>`; the next synchronization checkpoint records the actual verified remote SHA.
9. Post-push remote HEAD and CI/security/quality gates must be green before an iteration is qualified.

Task states: `TODO`, `READY`, `IN_PROGRESS`, `VERIFYING`, `BLOCKED`, `DONE`, `DEFERRED`, `REJECTED`.
Finding states: `OPEN`, `INVESTIGATING`, `RESOLVED`, `ACCEPTED_RISK`, `REJECTED`.

---

## 1. Current architecture and invariants

- Declarative Go `model`, AXM and TOML converge on the canonical compiled Axiom runtime.
- Typed `Flow` remains a separate reducer-oriented surface.
- `adgo` remains a separate durable graph/coordinator/worker runtime.
- Shared low-level durable primitives may be extracted only after contracts prove they are behavior-identical; Core and ADGO engines must not be merged into a mega-runtime.
- External effects are at-least-once, never falsely exactly-once.
- Idempotency/reconciliation is explicit at external effect boundaries.
- Durable intents/tasks are persisted before external execution where the contract requires it.
- Stale workers cannot commit through fencing.
- Execution/plan/schema identity is explicit; incompatible persisted formats fail closed.
- Same-execution mutation is serialized/validated while independent executions may progress concurrently.
- Semantic durable time uses explicit clock abstractions.
- Parsed syntax is not a runtime guarantee until executable behavior proves it.
- Cross-platform/race/security gates are not weakened to regain green.

Repository/process state:

- `MASTER_PLAN.md` was bootstrapped by T-001 and is now authoritative.
- `AGENTS.md` is absent; active repository instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` branch protection disabled; T-010 remains an external blocker.
- T-002 commit `094de51e4e42d72d4bdb4f813f342cee71f9ac87` passed `ci`, `security`, `quality-loop` and `module-checksum`.
- No GitHub Release had been published at the audited baseline.

---

## 2. Findings

### F-001 — Missing authoritative living execution plan

**Status:** RESOLVED by T-001
**Severity:** High
**Root cause:** topic/era plans accumulated without a final ownership boundary.
**Prevention:** this file is the single execution roadmap and contains continuation checkpoints.

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
**Resolution:** strict SemVer validation, exact remote `release/<version>` fetch, ancestor validation, duplicate-tag rejection and frozen SHA output are executable and tested.

### F-004 — Release verification is weaker than normal `main`

**Status:** IN_PROGRESS via T-003
**Category:** CI/CD
**Severity:** High
**Root cause:** release verification and normal verification are separately maintained DAGs.
**Direction:** call current reusable `ci` and `security` workflows on the frozen SHA.

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
**Resolution:** resolver classifies SemVer correctly; T-003 removes tag-push publication entirely so current release tooling has one authoritative entrypoint.

### F-011 — Release publication was more permissive than documented policy

**Status:** IN_PROGRESS via T-003
**Category:** Release / supply chain
**Severity:** High
**Evidence:** generated-note fallback and create-or-upload/clobber behavior contradicted documented fail-closed policy.
**Direction:** require candidate notes, reject existing GitHub Release, create once with explicit target, verify tag commit.

### F-012 — Security workflow requested unused repository-wide `security-events: write`

**Status:** RESOLVED by T-003
**Category:** CI security / permissions
**Severity:** Medium
**Evidence:** current security jobs run Gitleaks, govulncheck and text-mode gosec; none uploads SARIF/security events.
**Root cause:** stale broad workflow-level permission survived after scanner topology changed.
**Resolution:** reusable security workflow is read-only (`contents: read`). Further release job-level permission minimization remains T-041.

### F-013 — Tag-push release can execute obsolete release tooling from the frozen tag

**Status:** IN_PROGRESS via T-003
**Category:** Release / supply chain
**Severity:** High
**Confidence:** High
**Evidence:** GitHub Actions executes the workflow version present in the event-associated SHA/ref. A frozen tag can therefore carry an older `release.yml` and bypass current hardened main tooling.
**Root cause:** two publication entrypoints with different workflow-code ownership.
**Impact:** security/correctness fixes in current release tooling do not govern externally pushed old frozen tags.
**Direction:** publication is `workflow_dispatch` from `main` only; tag creation is an output of the hardened publisher, not a trigger.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process

#### T-001 — Establish authoritative `MASTER_PLAN.md`
**Status:** DONE
**Implementation commit:** `414f01c84ec215b29784cbfa7e5987cb35cdea41`

#### T-010 — Protect `main` with required checks
**Status:** BLOCKED — external GitHub setting
**Priority:** P0
**Minimum external action:** enable branch/ruleset protection with exact active required checks, no force push/delete, and appropriate PR/review requirements.
**Independent work:** source hardening continues because direct-main execution was explicitly requested and the connector cannot change this setting.

### M1 — Release correctness and provenance

#### T-002 — Frozen release metadata resolution
**Status:** DONE
**Priority:** P0/P1
**Implementation commit:** `094de51e4e42d72d4bdb4f813f342cee71f9ac87`
**Qualification:** `ci`, `security`, `quality-loop`, `module-checksum` all PASS.
**Tests:** synthetic bare-remote success/negative suite; ancestor and prerelease mutants killed.

#### T-003 — Publication and verification must match one release contract
**Status:** IN_PROGRESS
**Priority:** P1
**Depends on:** T-002

Acceptance:
- exactly one publication entrypoint: `workflow_dispatch` from `main`;
- frozen candidate must contain non-empty `docs/releases/<version>.md`;
- existing GitHub Release is rejected fail-closed;
- current normal `ci` workflow is reusable and verifies the frozen SHA, including full `go test -race ./...` and cross-platform tests;
- current security workflow is reusable and scans the frozen SHA;
- reusable workflow concurrency cannot collide with release caller concurrency;
- every verification checkout honors the supplied candidate ref;
- publication has no generated-note, upload or clobber fallback;
- `gh release create` explicitly uses `--target "$TARGET_SHA"`;
- created tag is fetched/resolved and must equal the frozen SHA;
- executable workflow contract test guards the above and kills meaningful policy mutants;
- docs and workflow describe the same behavior.

Provenance/attestation and broader release permission minimization are reviewed here but implemented under T-041 so release correctness and supply-chain identity remain separate atomic concerns.

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

- **T-040 — remove broad gosec exclusions incrementally:** READY, P1.
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
- REJECTED: weaken tests/security checks to restore green.
- REJECTED: optimize before measurement or correctness closure.
- REJECTED: tag-push as a second release publication entrypoint.
- REJECTED: hidden create-or-upload/clobber recovery in normal release publication.
- DEFERRED: generated typed-activity specialization until profiling proves value after semantic freeze.

---

## 5. Iteration log

### Iteration 1 — T-001

- Root cause fixed: YES.
- Result: authoritative plan created.
- Implementation commit: `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- Post-push qualification: `ci`, `security`, `quality-loop`, `module-checksum` PASS.
- Process learning: self-SHA cannot be embedded in the same commit; actual SHA is confirmed by next synchronization checkpoint.

### Iteration 2 — T-002

SELF REVIEW
- Root cause fixed: YES for candidate metadata selection.
- Findings resolved: F-003, F-010.
- Tests: synthetic remote tests cover frozen SHA, malformed SemVer, missing/divergent branch, duplicate tag and prerelease classification.
- Mutation: ancestor-enforcement removal KILLED; forced-prerelease mutant KILLED.
- Race/performance/runtime compatibility: production Go code not touched.
- Security: untrusted version validated before refspec construction; exact remote refs fetched; unexpected remote-tag lookup errors fail closed.
- Implementation commit: `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.
- Post-push qualification: all four repository workflows PASS.
- Process learning: critical release policy belongs in a testable script, not inline-only YAML.

### Iteration 3 — T-003

Selected task: converge release publication and verification on one executable contract.

Why now: T-002 unlocked deterministic frozen identity; T-010 is externally blocked; release still has high-blast-radius verification/publication drift.

Pre-flight contract:
- Root cause: normal CI/security and release verification are duplicate DAGs; publication also has permissive fallbacks and dual trigger ownership.
- Affected invariants: frozen candidate identity, verification parity, fail-closed publication, least privilege, immutable tag target.
- Change surface: `.github/workflows/{ci,security,release}.yml`, `scripts/test_release_workflow.sh`, `docs/versioning.md`, `MASTER_PLAN.md`.
- Protected surface: all Go runtime/API/store/persistence/ADGO/Flow behavior.
- Observable contract: release verifies the exact frozen SHA using the same current CI/security DAG and creates one exact-target release only.
- Characterization: old release raced only selected packages, could generate notes, could upload/clobber an existing release, and tag-push could execute old release tooling.
- Compatibility: no library/runtime change; release process intentionally becomes stricter.
- Failure modes: reusable-workflow ref drift, caller/callee concurrency collision, insufficient token permissions, missing candidate notes, existing release, tag mismatch, API lookup failure.
- Rollback: revert one tooling commit; no persisted runtime data is altered.

Edge-space projection:
- INPUT: malformed/valid SemVer, stable/prerelease flag.
- STATE: missing notes, empty notes, existing tag, existing release, missing/divergent frozen branch.
- EXTERNAL STATE: GitHub API unavailable/auth failure, remote tag changed, candidate SHA old but valid.
- CONCURRENCY: caller and reusable workflow concurrency groups must not alias.
- PERMISSIONS: verification is read-only; publication retains only currently required publisher rights pending T-041.
- VERSION: current main tooling verifies old frozen candidate code by explicit checkout ref.

Characterization and local verification:
- current candidate-selection suite PASS before modification;
- planned YAML parses successfully;
- release workflow static contract PASS;
- explicit-target removal mutant KILLED;
- generated-notes fallback mutant KILLED;
- verification-checkout candidate-ref mutant KILLED;
- reintroduced tag-push entrypoint mutant KILLED.

SELF REVIEW before push:
- Root cause fixed: YES by reusable verification DAGs rather than copied command lists.
- New findings: F-012 and F-013 recorded; F-012 fixed in-scope, F-013 fixed by single entrypoint.
- Simplification: tag-push publication and permissive recovery paths are deleted rather than wrapped.
- Security: verification token is read-only; API errors fail closed; current broader release publisher permissions are deferred explicitly to T-041.
- Performance: no runtime path touched.
- Compatibility: Go/public/persisted contracts unchanged.
- Plan reconciliation: T-003 owns release correctness; provenance/remaining permission minimization moved to T-041 to avoid scope inflation.

Commit: <this commit>
Status: VERIFYING — pending remote synchronization, atomic push and GitHub Actions qualification.

---

## 6. Continuation checkpoint

CURRENT HEAD: last qualified `094de51e4e42d72d4bdb4f813f342cee71f9ac87`; Iteration 3 commit pending.
CURRENT QUALIFIED MILESTONE: M1 release correctness through T-002.

OPEN CRITICAL/HIGH:
- F-002 unprotected main — external blocker;
- F-004/F-011/F-013 — T-003 in verification;
- F-005 Core/ADGO durable primitive duplication;
- F-006 Flow crash-boundary proof gap;
- F-007 global gosec exclusions;
- F-008 no mechanical API compatibility gate.

BLOCKERS:
- T-010 requires external GitHub repository settings.

NEXT TASK AFTER T-003 QUALIFIES:
- T-040 — begin one-rule-at-a-time gosec suppression removal.

WHY NEXT:
- security blind spots are High severity, source-controlled and independently reducible; each rule can be an atomic finding/fix iteration with direct negative-test evidence. T-030 remains the next major correctness milestone after the scanner boundary is trustworthy.

CRITICAL FILES:
- `MASTER_PLAN.md`
- `.github/workflows/{ci,security,release}.yml`
- `scripts/resolve_release_candidate.sh`
- `scripts/test_release_candidate.sh`
- `scripts/test_release_workflow.sh`
- `docs/versioning.md`

IMPORTANT DECISIONS:
- one release publisher entrypoint from current `main`;
- frozen code identity and current verification-tool identity are separate by design;
- release verification reuses normal CI/security DAGs;
- exceptional release recovery is never hidden in the normal path.

REJECTED OPTIONS:
- duplicated inline release test list;
- tag-push publication;
- generated notes fallback;
- `gh release upload --clobber` fallback;
- relying on checkout state instead of explicit frozen ref.
