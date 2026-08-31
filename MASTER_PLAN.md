# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`  
Target branch: `main`  
Last qualified HEAD before Iteration 5: `3a0ba3034f9202a38108fe412286f4e337a90f21`  
Last reconciliation: 2026-08-31

> This file is the only execution roadmap. Historical plans, audits and topic-specific documents are evidence/reference inputs, not parallel roadmaps. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Reconstruct state from remote `main`, this file, repository instructions and current CI before substantial work.
2. Every substantial change has a `T-XXX`; every substantial unexpected problem has an `F-XXX` before the corresponding architectural/API/security/persistence fix.
3. Red `main` blocks unrelated work. `FAIL -> retry -> PASS` is not qualification for a flaky test.
4. One logical iteration = one logical commit containing implementation, executable evidence, relevant docs and plan reconciliation whenever technically possible.
5. Never force-push.
6. Prefer root-cause fixes, executable invariants, fail-closed behavior and small reversible transitions.
7. Mutation testing is used where it can distinguish semantic contracts. Timing/concurrency defects prefer deterministic clocks/schedulers plus race/shuffle evidence.
8. Security suppressions must be scoped, mechanically constrained and justified; broad global suppressions are temporary debt, not accepted evidence.
9. Performance changes require measurement first.
10. A commit cannot embed its own final SHA without changing that SHA. Iteration logs use `Commit: <this commit>` and the following synchronization checkpoint records the actual verified SHA.
11. Post-push remote HEAD and relevant `ci`, `security`, `quality-loop`, and `module-checksum` gates must be green before an iteration is qualified.

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
- Durable intents/tasks are persisted before external execution where required by contract.
- Stale workers cannot commit through fencing.
- Execution/plan/schema identity is explicit; incompatible persisted formats fail closed.
- Same-execution mutation is serialized/validated while independent executions may progress concurrently.
- Semantic durable time uses explicit clock abstractions. If an operation is externally observable as started, timers governing it must already be armed against the same semantic clock.
- Retry jitter is deterministic by execution/node identity and attempt; it is not a source of secrets, tokens, authorization entropy or lock ownership.
- Security-sensitive random identifiers use cryptographic randomness.
- Parsed syntax is not a runtime guarantee until executable behavior proves it.
- Cross-platform/race/security/quality gates are never weakened merely to regain green.

Repository/process state:

- `MASTER_PLAN.md` is authoritative; `AGENTS.md` is absent; active instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` branch protection disabled; T-010 remains an external blocker.
- `3a0ba3034f9202a38108fe412286f4e337a90f21` is fully qualified: `ci`, `security`, `quality-loop`, `module-checksum` all PASS.
- Full CI qualification at that HEAD includes Linux/macOS/Windows unit tests, `go test -race ./...`, lint, fuzz smoke, examples, codegen, downstream consumer isolation, release metadata contract and benchmark smoke.
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
**Evidence:** GitHub branch metadata reports `protected=false` and no required checks.  
**Impact:** direct pushes can bypass otherwise strong source-controlled verification.  
**Affected task:** T-010.

### F-003 — Manual release selected caller HEAD instead of frozen candidate
**Status:** RESOLVED by T-002  
**Category:** Release / supply chain  
**Severity:** High

### F-004 — Release verification was weaker than normal `main`
**Status:** RESOLVED by T-003 and qualified after T-004  
**Category:** CI/CD  
**Severity:** High  
**Resolution:** release verification reuses current `ci` and `security` workflows on the exact frozen candidate SHA.

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
**Category:** Security / signal quality  
**Severity:** High  
**Evidence before Iteration 5:** `G101,G104,G115,G301,G302,G304,G306,G404,G703` were globally excluded.  
**Direction:** remove one rule at a time, classify actual findings, use only narrow executable exceptions.

### F-008 — Public compatibility promises lack a mechanical API gate
**Status:** OPEN  
**Category:** API / compatibility  
**Severity:** High  
**Affected tasks:** T-050/T-051.

### F-009 — Documentation has semantic drift
**Status:** OPEN  
**Category:** Documentation / process  
**Severity:** Medium  
**Evidence:** stale architecture/runtime wording and development/CI command drift.  
**Affected tasks:** T-060/T-061.

### F-010 — Tag-trigger prerelease detection treated every `v*` tag as prerelease
**Status:** RESOLVED by T-002/T-003  
**Severity:** High

### F-011 — Release publication was more permissive than documented policy
**Status:** RESOLVED by T-003 and qualified after T-004  
**Severity:** High  
**Resolution:** frozen notes required; existing release rejected; no generated-note/upload/clobber fallback; explicit target SHA and tag verification.

### F-012 — Security workflow requested unused repository-wide `security-events: write`
**Status:** RESOLVED by T-003  
**Severity:** Medium

### F-013 — Tag-push release could execute obsolete release tooling from the frozen tag
**Status:** RESOLVED by T-003 and qualified after T-004  
**Severity:** High  
**Resolution:** publication is `workflow_dispatch` from current `main`; tag creation is an output, never a publication trigger.

### F-014 — Hedged activity registered its semantic timer after primary execution became observable
**Status:** RESOLVED by T-004  
**Category:** Concurrency / deterministic time / CI reliability  
**Severity:** High  
**Evidence:** quality-loop run `33427488084`, job `99604582523`, shuffle seed `1788202523607722540` timed out in `TestHedgedActivityDeterministicClock`.  
**Root cause:** primary goroutine could start before `ManualClock` timer registration, shifting an expected +5s deadline to +10s.  
**Resolution:** first hedge timer is armed before primary launch.  
**Qualification:** full CI/race, security, quality-loop and module checksum PASS on `3a0ba303...`.

### F-015 — One intentional deterministic `math/rand` use caused repository-wide G404 blindness

**Status:** VERIFYING via T-040  
**Category:** Security / static-analysis policy  
**Severity:** High  
**Confidence:** High

**Evidence:** `security.yml` globally excluded `G404`. Production search found `math/rand` in `adgo/runtime.go`; the call is inside `backoff`, seeded deterministically from SHA-256 of execution/node identity plus attempt. Security-sensitive file-lock ownership already uses `crypto/rand`.

**Observed behavior:** accommodating one non-security deterministic jitter implementation disabled weak-RNG detection for every future production file.

**Expected behavior:** `G404` runs repository-wide; only the known deterministic jitter location is excluded, and an executable policy fails if that exception expands.

**Root cause:** a broad scanner suppression was used instead of classifying and mechanically constraining the false positive.

**Impact:** a future use of `math/rand` for tokens, ownership IDs, authentication material or other security decisions could pass SAST silently.

**Affected invariants:** cryptographic randomness for security identity; deterministic retry semantics; trustworthy CI signal.

**Affected task:** T-040.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process

#### T-001 — Establish authoritative `MASTER_PLAN.md`
**Status:** DONE  
**Implementation commit:** `414f01c84ec215b29784cbfa7e5987cb35cdea41`.

#### T-004 — Restore deterministic hedged timing and green quality-loop
**Status:** DONE  
**Finding:** F-014  
**Implementation commit:** `3a0ba3034f9202a38108fe412286f4e337a90f21`  
**Qualification:** `ci`, `security`, `quality-loop`, `module-checksum` PASS.

#### T-010 — Protect `main` with required checks
**Status:** BLOCKED — external GitHub setting  
**Priority:** P0  
**Minimum external action:** enable branch/ruleset protection with exact active required checks, no force push/delete, and appropriate review requirements.

### M1 — Release correctness and provenance

#### T-002 — Frozen release metadata resolution
**Status:** DONE  
**Implementation commit:** `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.

#### T-003 — Publication and verification match one release contract
**Status:** DONE  
**Implementation commit:** `8e1b11560305d56010049c992968c11f3197ca9e`  
**Qualification:** release tooling itself passed `ci`, `security`, and module gates at its commit; its only red quality gate was F-014, a pre-existing defect whose source blob predated T-003 and which is now fixed/qualified by T-004.

Implemented contract:
- one publication entrypoint from current `main`;
- exact frozen candidate identity;
- candidate release notes required;
- current reusable CI/security DAGs verify candidate SHA;
- existing release/tag paths fail closed;
- no generated-note/upload/clobber recovery;
- explicit release target and post-create tag SHA verification.

### M2 — Durable correctness closure

- **T-030 — reusable deterministic durable failpoint framework:** TODO, P1.
- **T-031 — Flow intent/effect/ack crash matrix:** TODO, P1, depends T-030.
- **T-032 — no-resurrection/backend/crash equivalence properties:** TODO, P1/P2, depends T-030.
- **T-033 — Flow backlog/backpressure/observability contracts:** TODO, P2, depends T-031.

### M3 — Shared durable primitives without merging engines

- **T-020 — inventory Core vs ADGO durable primitive contracts:** TODO, P1.
- **T-021 — define acyclic shared durable boundary:** TODO, P1/P2, depends T-020.
- **T-022 — extract only behavior-identical pure primitives:** TODO, P2, depends T-021.
- **T-023 — architecture dependency/anti-drift tests:** TODO, P2, depends T-021.

### M4 — Security boundary reduction

#### T-040 — Enable G404 globally and mechanically constrain deterministic retry-jitter exception
**Status:** VERIFYING  
**Priority:** P1  
**Finding:** F-015

Acceptance:
- `G404` is absent from global `-exclude`;
- current gosec still runs all other enabled rules unchanged;
- only `adgo/runtime.go` receives a path-scoped G404 exception;
- deterministic retry jitter code is unchanged, preserving timing/replay compatibility;
- executable `scripts/test_gosec_policy.sh` fails if G404 returns to global exclusions;
- the sentinel fails if `adgo/runtime.go` gains any `math/rand` API beyond the approved `rand.New` + `rand.NewSource` pair;
- gosec requires rule IDs and justifications for future inline suppressions;
- security workflow and all repository gates pass post-push.

#### T-042 — Classify and remove the next global gosec exclusion
**Status:** READY after T-040 qualifies  
**Priority:** P1  
**Selection rule:** inspect actual findings/signal quality first; do not mass-enable taint or filesystem rules blindly.

#### T-041 — Minimize remaining workflow permissions, add release provenance/attestation and pin container bases by digest
**Status:** TODO  
**Priority:** P2.

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
- REJECTED: weaken/skip/retry-away tests or scanners to regain green.
- REJECTED: replace deterministic retry jitter with cryptographic randomness merely to silence G404; it would change replay/timing behavior and solve the scanner rather than the contract.
- REJECTED: replace `math/rand` with an unreviewed home-grown PRNG to evade SAST.
- REJECTED: leave G404 globally disabled for one known false positive.
- REJECTED: tag-push as a second release publication entrypoint.
- DEFERRED: generated typed-activity specialization until profiling proves value after semantic freeze.

---

## 5. Iteration log

### Iteration 1 — T-001
- Authoritative plan established.
- Commit: `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- Qualification: all repository gates PASS.

### Iteration 2 — T-002
- Frozen candidate selection moved from inline assumptions to executable/tested resolver.
- Findings resolved: F-003, F-010.
- Meaningful mutants: ancestor-enforcement removal KILLED; forced-prerelease mutant KILLED.
- Commit: `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.
- Qualification: all repository gates PASS.

### Iteration 3 — T-003
- Release verification now reuses current verification DAGs; publication is single-entrypoint and fail-closed.
- Findings addressed: F-004, F-011, F-012, F-013.
- Contract mutants killed: explicit target removal; generated-notes fallback; wrong verification checkout ref; tag-push reintroduction.
- Commit: `8e1b11560305d56010049c992968c11f3197ca9e`.
- Qualification completed after T-004 removed the independent pre-existing quality-loop flake.

### Iteration 4 — T-004
- Finding: F-014.
- Root cause fixed: first semantic hedge timer is armed before primary execution can become observable.
- No API/persistence/result-selection changes.
- Commit: `3a0ba3034f9202a38108fe412286f4e337a90f21`.
- Post-push: full CI/race, security, quality-loop and module checksum PASS.
- Process learning: stricter quality gates can reveal pre-existing defects; classify causality without weakening the red-main rule.

### Iteration 5 — T-040

Selected task: replace repository-wide G404 blindness with a narrow executable exception boundary.

Why now: main is green again; F-007 is the highest-severity source-controlled security signal gap and can be reduced atomically.

Pre-flight contract:
- Root cause: global G404 exclusion exists to tolerate one deterministic non-security `math/rand` use.
- Affected invariants: security-sensitive entropy must be cryptographic; retry jitter must remain deterministic; SAST must detect new weak RNG uses.
- Change surface: `.github/workflows/security.yml`, `scripts/test_gosec_policy.sh`, `MASTER_PLAN.md`.
- Protected surface: `adgo/runtime.go` implementation, public APIs, retry values, persistence, Flow, release behavior.
- Observable contract: future weak RNG uses outside the approved deterministic jitter file fail G404; expansion inside the scoped file fails the policy sentinel.
- Characterization: G404 was in the global exclude list; production `math/rand` is limited to deterministic `backoff`; file-lock ownership uses `crypto/rand`.
- Compatibility: no production code change.
- Failure modes: path rule fails to match; sentinel becomes tautological; a new RNG API slips into scoped file; unsupported gosec flags.
- Rollback: revert one tooling/policy commit.

Verification before push:
- gosec v2.28.0 source confirms `-exclude-rules`, `-nosec-require-rules`, and `-nosec-require-justification` exist;
- policy script passes shell syntax checking;
- policy fixture PASS;
- first fixture attempt was correctly rejected as invalid evidence after a `Permission denied` execution mistake;
- corrected execution via `bash` PASS;
- mutant restoring global G404 exclusion: KILLED;
- mutant adding `rand.Intn` to the scoped runtime file: KILLED.

SELF REVIEW before push:
- Root cause fixed for G404: YES — scanner exception scope shrinks from repository-wide to one file with an executable expansion guard.
- Deterministic retry behavior changed: NO.
- New abstraction: NO.
- Security posture: improved; future inline suppressions must carry rule IDs and justifications.
- Remaining global exclusions: still debt under F-007; T-042 selects the next rule from evidence instead of enabling all at once.
- Performance/API/persistence compatibility: unchanged.

Commit: <this commit>  
Status: VERIFYING — pending atomic push and post-push qualification.

---

## 6. Continuation checkpoint

CURRENT HEAD BEFORE ITERATION 5 PUSH: `3a0ba3034f9202a38108fe412286f4e337a90f21`  
CURRENT QUALIFIED MILESTONE: M1 release correctness closed; red-main recovery closed; security suppression reduction starting.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external blocker;
- F-005 Core/ADGO durable primitive duplication;
- F-006 Flow crash-boundary proof gap;
- F-007 remaining global gosec exclusions;
- F-008 no mechanical API compatibility gate;
- F-015 G404 scope reduction — VERIFYING.

BLOCKERS:
- T-010 requires external GitHub repository settings.

NEXT TASK AFTER T-040 QUALIFIES:
- T-042 — classify and remove the next global gosec exclusion using actual scanner evidence.

WHY NEXT:
- each removed global suppression reduces a whole class of future false-green security states; rule-by-rule classification keeps signal quality high and avoids mass-noise changes before the larger T-030 persistence campaign.

CRITICAL FILES:
- `MASTER_PLAN.md`
- `.github/workflows/security.yml`
- `scripts/test_gosec_policy.sh`
- `adgo/runtime.go`
- `adgo/file_lock.go`

VERIFICATION:
- `bash scripts/test_gosec_policy.sh`
- security workflow `SAST Scan (gosec)`
- repository `ci`, `security`, `quality-loop`, `module-checksum` gates.
