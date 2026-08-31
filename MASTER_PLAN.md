# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`  
Target branch: `main`  
Last qualified HEAD: `aa823e1613a44445fd552a634f8abc399116f39d`  
Last reconciliation: 2026-08-31

> This file is the only execution roadmap. Historical plans, audits and topic-specific documents are evidence inputs, not parallel execution plans. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Reconstruct state from remote `main`, this file, repository instructions and current CI before substantial work.
2. Substantial work uses `T-XXX`; substantial unexpected information uses `F-XXX` before architecture/API/security/persistence behavior is changed.
3. Red `main` blocks unrelated work. Flaky `FAIL -> retry -> PASS` is not qualification.
4. One logical iteration = one logical commit with implementation, executable evidence, relevant docs and plan reconciliation whenever technically possible.
5. Never force-push.
6. Prefer root-cause fixes, executable invariants, fail-closed behavior and small reversible transitions.
7. Use mutation testing where it distinguishes semantic contracts; use deterministic clocks/schedulers plus race/shuffle for timing/concurrency contracts.
8. Security suppressions must be narrow, mechanically constrained and justified. A path-scoped false-positive exception must have an executable counter-scan when the path could later contain real findings of the same rule.
9. Performance work requires measurement first.
10. Iteration logs use `Commit: <this commit>` when the commit SHA cannot yet be embedded; the next checkpoint records the verified SHA.
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
- Semantic durable time uses explicit clock abstractions; governing timers are armed before an operation becomes observably started.
- Retry jitter is deterministic by execution/node identity and attempt and is never used as security entropy.
- Security-sensitive random identifiers use cryptographic randomness.
- Production credentials are external inputs, never intentional source literals.
- Public protocol/version identifiers are not credentials; if a scanner misclassifies one, the exception must not create a blind spot for future credentials.
- Budget aggregation is fail-closed: invalid, negative, non-finite or overflowing usage from any executed speculative variant invalidates the aggregate instead of silently selecting a winner with partial accounting.
- Cleanup errors from durable-store initialization and explicit close paths are not silently discarded when they can be returned or joined with the primary failure.
- Parsed syntax is not a runtime guarantee until executable behavior proves it.
- Cross-platform/race/security/quality gates are never weakened merely to regain green.

Repository/process state:

- `MASTER_PLAN.md` is authoritative; `AGENTS.md` is absent; active instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` protection disabled; T-010 remains an external blocker.
- `aa823e1613a44445fd552a634f8abc399116f39d` is fully qualified: full CI/race, security, quality-loop and module checksum PASS.
- G404 is enabled repository-wide with one path-scoped, sentinel-constrained deterministic-jitter exception.
- G101 is enabled repository-wide. `adgo/http_worker.go` has one reviewed path-scoped G101 false positive, but a dedicated counter-scan re-runs G101 on `adgo` and permits exactly one finding tied to `HTTPWorkerProtocolVersion`; any additional G101 finding fails CI.
- Exact gosec v2.28.0 characterization at `aa823e1613a44445fd552a634f8abc399116f39d` measured 92 findings across the then-remaining exclusions: `G104=8`, `G115=22`, `G301=20`, `G302=6`, `G304=18`, `G306=4`, `G703=14`.
- T-043 selects G104 because its small finding set contains a real fail-closed budget-accounting defect; G306 is smaller but currently dominated by intentional public generated/benchmark file permissions and needs an explicit artifact-permission policy first.
- After the pending T-043 implementation, intended remaining global gosec exclusions are `G115,G301,G302,G304,G306,G703`.
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
**Remaining after pending T-043:** `G115,G301,G302,G304,G306,G703`.  
**Characterization evidence:** exact v2.28.0 informational scan at `aa823e1613a44445fd552a634f8abc399116f39d`: `G104=8`, `G115=22`, `G301=20`, `G302=6`, `G304=18`, `G306=4`, `G703=14`.  
**Direction:** one rule per atomic iteration, based on actual finding/signal analysis; do not mass-enable noisy rules.

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

### F-016 — G101 hardcoded-credential detection was globally disabled
**Status:** RESOLVED by T-042  
**Category:** Security / credential leakage / signal quality  
**Severity:** High  
**Root cause:** inherited broad SAST exclusion outlived its justification.  
**Resolution:** G101 removed from global `-exclude`; global re-suppression is rejected by `scripts/test_gosec_policy.sh`.  
**Qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

### F-017 — G101 misclassifies the public HTTP worker protocol version as a credential
**Status:** RESOLVED by T-042 failure recovery  
**Category:** Security / scanner signal quality  
**Severity:** Medium process risk; not a product vulnerability  
**Evidence:** gosec v2.28 reported `adgo/http_worker.go` `HTTPWorkerProtocolVersion = "adgo-worker-v1"` as G101/CWE-798 with LOW confidence.  
**Root cause:** G101 heuristic treats the public protocol-version constant as credential-like even though it carries no authentication secret.  
**Rejected fix:** restore G101 global exclusion or silently exclude the whole file without compensating evidence.  
**Resolution:** main scan excludes only `G101` for `adgo/http_worker.go`; `scripts/verify_g101_false_positive.sh` independently re-runs G101 on `adgo` and requires exactly one G101 finding, in `http_worker.go`, referencing `HTTPWorkerProtocolVersion`. A second credential-like finding therefore fails CI.  
**Test-of-tests:** expected fixture PASS; second-G101 mutant KILLED; wrong-path mutant KILLED; global-G101-restoration mutant KILLED; missing-counter-scan mutant KILLED.  
**Qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

### F-018 — Speculative budget aggregation ignored validation failure
**Status:** VERIFYING via T-043  
**Category:** Correctness / budget safety / security signal  
**Severity:** High  
**Evidence:** exact G104 characterization reported `adgo/speculation.go:213` ignoring the return value from `addBudget(&budget, value.result.Budget)`. `addBudget` rejects invalid negative/non-finite usage and arithmetic overflow, so discarding its error allows a speculative winner to be selected after failed budget aggregation.  
**Root cause:** speculative result selection treated budget accumulation as infallible even though the shared accounting helper is explicitly fallible.  
**Resolution under verification:** fail immediately when any executed variant returns invalid budget usage; regression test asserts that an invalid variant cannot produce a speculative winner or partial aggregate. The seven scanner-reported cleanup/close findings are handled directly rather than suppressed, and adjacent explicit cleanup discards in Pebble initialization / production construction are also joined with their primary failures for consistent semantics.  
**Task:** T-043.

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
**Qualified commit:** `d611198a92f17011e487f5dba942bd2933da4a7a`.

#### T-042 — Enable G101 globally with a fail-closed false-positive contract
**Status:** DONE  
**Priority:** P1  
**Findings:** F-016, F-017  
**Activation commit:** `f98e049bc323c780cf4c780a9d4b2c94ab6cb3d5` — exposed F-017 and correctly failed security qualification.  
**Recovery/qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

Acceptance satisfied:
- G101 absent from global `-exclude`;
- global G101/G404 restoration is sentinel-blocked;
- G404 deterministic-jitter exception remains constrained;
- the G101 public-protocol false positive is path-scoped and independently counter-scanned;
- any second G101 in `adgo` fails the targeted contract;
- production Go code and public API were unchanged by the recovery commit;
- security, full CI/race, quality-loop and module-checksum all PASS.

#### T-043 — Enable G104 globally and close ignored-error findings
**Status:** VERIFYING  
**Priority:** P1  
**Finding:** F-018  
**Characterization commit:** `aa823e1613a44445fd552a634f8abc399116f39d` — exact v2.28.0 scan, fully qualified.  
**Implementation:** `<this commit>`.

Acceptance under verification:
- G104 removed from global `-exclude` and added to the anti-regression policy sentinel;
- no G104 path or inline suppressions introduced;
- speculative budget aggregation fails closed on `addBudget` validation errors;
- all seven scanner-reported cleanup/close findings are handled directly; adjacent explicit Pebble/production cleanup discards are also eliminated;
- temporary informational characterization step is removed after evidence capture;
- focused regression test catches the former ignored-budget error;
- security, full CI/race, quality-loop and module checksum must all pass.

#### T-044 — Select and remove the next global gosec exclusion
**Status:** BLOCKED on T-043 qualification  
**Priority:** P1  
**Evidence available:** `G115=22`, `G301=20`, `G302=6`, `G304=18`, `G306=4`, `G703=14`. Permission rules require an explicit public-artifact vs private-state policy; G703 requires source/sink path-boundary analysis; G115 requires integer-domain proofs rather than blanket casts or suppressions.

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
- REJECTED: restore G101 global suppression because one public protocol-version constant is misclassified.
- REJECTED: path-exclude a G101-bearing credential boundary without an independent counter-scan.
- REJECTED: suppress G104 cleanup errors or the speculative budget validation error instead of handling them.
- REJECTED: enable permission/path/integer rules in bulk without classifying their distinct contracts.
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
G404 enabled globally; deterministic retry-jitter exception constrained by executable sentinel; test-of-tests killed global-G404 and added-rand-API mutants; commit `d611198a92f17011e487f5dba942bd2933da4a7a`; all gates PASS.

### Iteration 6 — T-042 activation
- Pre-flight search found no obvious production credential literals.
- Commit `f98e049bc323c780cf4c780a9d4b2c94ab6cb3d5` removed G101 from global exclusion and strengthened the policy sentinel.
- Post-push security correctly FAILED: G101 exposed `HTTPWorkerProtocolVersion = "adgo-worker-v1"` as a LOW-confidence false positive.
- Learning: code search cannot substitute for executing the exact scanner; false-positive characterization belongs in the failure-recovery loop.

### Iteration 6A — T-042 failure recovery
- Finding F-017 recorded conceptually before remediation: public protocol version misclassified as credential.
- Rejected global rollback and unguarded file-level blindness.
- Added exact path-scoped G101 exclusion plus `verify_g101_false_positive.sh` counter-scan.
- Test-of-tests: normal fixture PASS; second-finding mutant KILLED; wrong-path mutant KILLED; global-G101 mutant KILLED; missing-guard mutant KILLED.
- Commit `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.
- Qualification: G101 main scan PASS, targeted false-positive contract PASS, govulncheck PASS, Gitleaks PASS, Linux/macOS/Windows tests PASS, race PASS, lint/fuzz/examples/codegen/downstream/release/benchmark gates PASS, quality-loop PASS, module checksum PASS.
- Process learning: a necessary path exclusion is acceptable only when a narrower executable counter-scan closes the blind spot.

### Iteration 7 — T-043 characterization
- Commit `aa823e1613a44445fd552a634f8abc399116f39d` added a temporary informational exact gosec v2.28.0 scan without weakening the enforced SAST scan.
- Measured 92 findings: G104=8, G115=22, G301=20, G302=6, G304=18, G306=4, G703=14.
- Full CI/race, security, quality-loop and module checksum PASS.
- Selected G104 because one finding exposed F-018; G306, although smaller, is dominated by intentional public artifact permissions and needs a separate policy.

### Iteration 7A — T-043 implementation
- G104 removed from global suppression; policy sentinel now prevents G104 from returning to the global exclude list.
- F-018 fixed by propagating `addBudget` validation failure from speculative result selection.
- Regression test asserts invalid speculative budget cannot produce a winner or partial budget result.
- Pebble iterator/closer errors are propagated; Pebble/production startup cleanup errors are joined with primary initialization failures.
- Temporary characterization step removed after evidence capture.
- Commit: `<this commit>`.
- Status: VERIFYING — post-push qualification required before T-044 starts.

---

## 6. Continuation checkpoint

CURRENT QUALIFIED HEAD: `aa823e1613a44445fd552a634f8abc399116f39d`  
CURRENT QUALIFIED MILESTONE: release correctness closed; deterministic quality gate restored; G404/G101 suppression debt closed; remaining gosec debt characterized exactly with v2.28.0.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external blocker;
- F-005 Core/ADGO durable duplication;
- F-006 Flow crash-boundary proof gap;
- F-007 remaining global gosec exclusions after pending T-043: `G115,G301,G302,G304,G306,G703`;
- F-008 no mechanical API compatibility gate;
- F-018 speculative budget validation propagation — VERIFYING.

CURRENT TASK:
- T-043 — post-push qualification of G104 enablement and ignored-error fixes.

NEXT TASK AFTER GREEN:
- T-044 — select next gosec rule from already measured evidence, with preference for correctness/security value over raw finding count.

VERIFICATION FOR T-043:
- focused speculative invalid-budget regression test;
- G104-enabled exact gosec v2.28.0 scan with no G104 suppressions;
- security workflow;
- full `ci`, `quality-loop`, `module-checksum` qualification.
