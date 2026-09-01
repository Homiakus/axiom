# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`  
Target branch: `main`  
Last qualified HEAD: `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`  
Last reconciliation: 2026-09-01

> This file is the only execution roadmap. Historical plans, audits and topic-specific documents are evidence inputs, not parallel execution plans. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Reconstruct state from remote `main`, this file, repository instructions and current CI before substantial work.
2. Substantial work uses `T-XXX`; substantial unexpected information uses `F-XXX` before architecture/API/security/persistence behavior is changed.
3. Red `main` blocks unrelated work. Flaky `FAIL -> retry -> PASS` is not qualification.
4. One logical iteration should remain atomic and reversible; implementation, executable evidence and relevant documentation belong together whenever practical.
5. Never force-push.
6. Prefer root-cause fixes, executable invariants and fail-closed behavior over scanner/test suppression.
7. Use deterministic clocks/schedulers plus race/shuffle for timing and concurrency contracts; use mutation testing when it distinguishes semantics.
8. Security exceptions must be narrow, mechanically constrained and justified. A path exception that can hide future findings of the same rule requires an independent counter-scan.
9. Performance work requires measurement first.
10. An iteration is qualified only after the pushed SHA has green `ci`, `security`, `quality-loop`, and `module-checksum` gates.

Task states: `TODO`, `READY`, `IN_PROGRESS`, `VERIFYING`, `BLOCKED`, `DONE`, `DEFERRED`, `REJECTED`.  
Finding states: `OPEN`, `INVESTIGATING`, `VERIFYING`, `RESOLVED`, `ACCEPTED_RISK`, `REJECTED`.

---

## 1. Architecture and critical invariants

- Declarative Go `model`, AXM and TOML converge on the canonical compiled Axiom runtime.
- Typed `Flow` and `adgo` remain separate orchestration surfaces; do not merge them into a mega-runtime.
- Share only behavior-identical low-level durable primitives after executable characterization.
- External effects are at-least-once, never falsely exactly-once; idempotency and reconciliation are explicit.
- Durable intents/tasks are persisted before external execution where required.
- Stale workers cannot commit through fencing.
- Execution/plan/schema identity is explicit; incompatible persisted formats fail closed.
- Same-execution mutation is serialized/validated; independent executions may progress concurrently.
- Semantic durable time uses explicit clock abstractions; governing timers are armed before an operation becomes observably started.
- Retry jitter is deterministic by execution/node identity and attempt and is never security entropy.
- Security-sensitive random identifiers use cryptographic randomness.
- Production credentials are external inputs, never intentional source literals.
- Public protocol/version identifiers are not credentials; scanner exceptions for them must not create credential blind spots.
- Budget aggregation is fail-closed: invalid, negative, non-finite or overflowing usage from any executed speculative variant invalidates the aggregate.
- Cleanup errors from durable-store initialization and explicit close paths are not silently discarded when they can be returned or joined with a primary failure.
- Typed numeric boundaries reject sign changes, narrowing overflow and non-finite/out-of-domain float-to-integer conversion rather than accepting Go wraparound semantics.
- Internal `uint32` runtime/compiler IDs may use reviewed narrowing only where structurally derived from in-memory slice indexes; the exact G115 finding multiset is independently counter-scanned.
- Private file-backed runtime state and coordination **files** are owner-only (`0600`) unless an explicit sharing contract states otherwise.
- Private file-backed runtime state and coordination **directories** are owner-only/traversable only by the owner (`0700`) unless an explicit sharing contract states otherwise.
- Generated source/publication artifacts are a separate permission domain. The axiomgen output directory intentionally remains `0755`; its sole G301 exception is independently counter-scanned repo-wide and cannot silently expand.
- Cross-principal shared-filesystem deployments require explicit external ACL/permission configuration; private defaults are not a substitute for a multi-principal ACL design.
- Parsed syntax is not a runtime guarantee until executable behavior proves it.
- Cross-platform/race/security/quality gates are never weakened merely to regain green.

Repository/process state:

- `MASTER_PLAN.md` is authoritative; `AGENTS.md` is absent; active repository instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` protection disabled; T-010 remains an external blocker.
- `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e` is fully qualified: security, full CI/race, quality-loop and module checksum PASS.
- G404 is enabled repository-wide with one path-scoped, sentinel-constrained deterministic-jitter exception.
- G101 is enabled repository-wide; the one public HTTP worker protocol-version false positive is path-scoped and independently counter-scanned.
- G104 is enabled repository-wide with no path or inline suppression.
- G115 is enabled repository-wide; 16 structurally bounded internal-ID conversions remain only across four reviewed paths and are counter-scanned as the exact `5+4+3+4` multiset.
- G302 is enabled repository-wide with no G302 suppression; ADGO lock files use `0600`.
- G301 is enabled repository-wide; 19 ADGO private-state directory findings were eliminated with `0700`. The one intentional `axiomgen` `0755` output-directory finding is path-scoped and independently constrained to exactly one repo-wide G301 finding.
- Exact gosec v2.28.0 characterization at `aa823e1613a44445fd552a634f8abc399116f39d` measured: `G104=8`, `G115=22`, `G301=20`, `G302=6`, `G304=18`, `G306=4`, `G703=14`.
- Current remaining global gosec exclusions: **`G304,G306,G703`**.
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
**Remaining global exclusions:** `G304,G306,G703`.  
**Direction:** remove one rule family per atomic iteration after exact finding/provenance classification; never bulk-enable noisy rules.

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
**Evidence:** quality-loop shuffle seed `1788202523607722540`; fixed by arming the first timer before primary launch.  
**Qualified commit:** `3a0ba3034f9202a38108fe412286f4e337a90f21`.

### F-015 — One deterministic `math/rand` use caused repository-wide G404 blindness
**Status:** RESOLVED by T-040  
**Severity:** High  
**Qualified commit:** `d611198a92f17011e487f5dba942bd2933da4a7a`.

### F-016 — G101 hardcoded-credential detection was globally disabled
**Status:** RESOLVED by T-042  
**Category:** Security / credential leakage / signal quality  
**Severity:** High  
**Qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

### F-017 — G101 misclassifies the public HTTP worker protocol version as a credential
**Status:** RESOLVED by T-042 failure recovery  
**Category:** Security / scanner signal quality  
**Severity:** Medium process risk; not a product vulnerability  
**Resolution:** one path-scoped false-positive exception plus exact independent G101 counter-scan; any second credential-like G101 in `adgo` fails CI.  
**Qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

### F-018 — Speculative budget aggregation ignored validation failure
**Status:** RESOLVED by T-043  
**Category:** Correctness / budget safety  
**Severity:** High  
**Resolution:** speculative result selection now propagates `addBudget` validation failure; cleanup/close findings were handled directly rather than suppressed.  
**Qualified commit:** `8560aca04db9dde1777d079404ec69d1ce080044`.

### F-019 — Unchecked numeric narrowing and round-robin counter rollover under G115
**Status:** RESOLVED by T-044  
**Category:** Correctness / numeric safety / long-running runtime safety  
**Severity:** High  
**Resolution:** typed numeric conversion is checked; Host round-robin stays unsigned through modulo; 16 structural internal-ID conversions are exact-counter-scanned.  
**Qualified commit:** `685a2b8f478ac57fd90af613f30a684c12c99f0a`.

### F-020 — ADGO coordination lock files were created world-readable
**Status:** RESOLVED by T-045  
**Category:** Security / filesystem confidentiality  
**Severity:** Medium  
**Resolution:** all five ADGO lock creation sites use `privateLockFileMode = 0600`; axiomgen append uses mode `0` because it does not create files.  
**Qualified commit:** `80c78cdb611cac857ce2dc1a674e952118a945ea`.

### F-021 — ADGO private runtime/durable directories were group/world traversable
**Status:** RESOLVED by T-046  
**Category:** Security / filesystem confidentiality / deployment boundary  
**Severity:** Medium  
**Evidence:** exact G301 characterization contained 20 findings: 19 ADGO runtime/durable state or coordination directories created `0755`, plus one intentionally shareable `axiomgen` output directory.  
**Root cause:** the same generic directory mode was used for two distinct permission domains: private runtime state and generated source output.  
**Resolution:** introduced `privateStateDirMode = 0700` and applied it to all 19 ADGO findings, including execution state, inbox/commits, locks, cache, schedules, provider health, admission state, artifact content-addressed storage and production roots. POSIX regression tests verify actual directory modes. `axiomgen` remains `0755` under one path-scoped exception; `verify_g301_codegen_directory.sh` re-runs exact G301 repo-wide and permits exactly that one finding.  
**Qualified commit:** `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process

- **T-001 — authoritative `MASTER_PLAN.md`: DONE**, commit `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- **T-004 — deterministic hedge timing: DONE**, commit `3a0ba3034f9202a38108fe412286f4e337a90f21`.
- **T-010 — protect `main`: BLOCKED**, external GitHub repository setting.

### M1 — Release correctness and provenance

- **T-002 — frozen release metadata resolution: DONE**, commit `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.
- **T-003 — single fail-closed publication/verification contract: DONE**, commit `8e1b11560305d56010049c992968c11f3197ca9e`.

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

- **T-040 — G404 globally + deterministic-jitter exception contract: DONE**, qualified `d611198a92f17011e487f5dba942bd2933da4a7a`.
- **T-042 — G101 globally + fail-closed protocol-version false-positive contract: DONE**, qualified `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.
- **T-043 — G104 globally + ignored-error closure: DONE**, qualified `8560aca04db9dde1777d079404ec69d1ce080044`.
- **T-044 — G115 globally + checked numeric boundaries: DONE**, qualified `685a2b8f478ac57fd90af613f30a684c12c99f0a`.
- **T-045 — G302 globally + private lock-file permissions: DONE**, qualified `80c78cdb611cac857ce2dc1a674e952118a945ea`.
- **T-046 — G301 globally + private state-directory permissions: DONE**, qualified `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`.

#### T-047 — Select and remove the next global gosec exclusion
**Status:** READY  
**Priority:** P1  
**Candidates:** `G304=18`, `G306=4`, `G703=14` from the exact characterization baseline.  
**Selection rule:** characterize actual current findings first. G306 is the leading low-complexity candidate only if its four findings are truly intentional generated/public outputs that can be captured by a narrow executable permission contract. G304/G703 may have higher security value but require path provenance/root-containment analysis and must not be path-excluded wholesale.

- **T-041 — supply-chain provenance, remaining workflow permission minimization, container digest pinning:** TODO, P2.

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
- REJECTED: retry-away, disable or weaken tests/scanners to regain green.
- REJECTED: replace deterministic retry jitter with cryptographic randomness merely to silence SAST.
- REJECTED: leave G404 globally disabled for one deterministic false positive.
- REJECTED: restore G101 global suppression because one public protocol-version constant is misclassified.
- REJECTED: path-exclude a credential boundary without an independent counter-scan.
- REJECTED: suppress G104 cleanup/budget validation errors instead of handling them.
- REJECTED: restore G115 globally or accept a G115 path exception without an exact counter-scan.
- REJECTED: restore G302/G301 globally or make generated source private merely to satisfy permission scanners.
- REJECTED: treat an entire generator/source subtree as trusted for G301; only the exact one-finding contract is accepted.
- REJECTED: enable permission/path rules in bulk without classifying their distinct contracts.
- REJECTED: tag-push as a second release publisher.
- DEFERRED: generated typed-activity specialization until profiling proves value.

---

## 5. Iteration log

1. **T-001** — authoritative plan established; `414f01c84ec215b29784cbfa7e5987cb35cdea41`; all gates PASS.
2. **T-002** — frozen release metadata; `094de51e4e42d72d4bdb4f813f342cee71f9ac87`; all gates PASS.
3. **T-003/T-004** — fail-closed single release path plus deterministic hedge-timer race fix; `8e1b11560305d56010049c992968c11f3197ca9e` / `3a0ba3034f9202a38108fe412286f4e337a90f21`.
4. **T-040** — G404 global enablement with deterministic-jitter sentinel; `d611198a92f17011e487f5dba942bd2933da4a7a`.
5. **T-042** — G101 activation exposed a real scanner false positive; recovery added exact counter-scan rather than global rollback; qualified `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.
6. **T-043** — exact SAST characterization (`aa823e1613a44445fd552a634f8abc399116f39d`) then G104 closure, including fail-closed speculative budget accounting; qualified `8560aca04db9dde1777d079404ec69d1ce080044`.
7. **T-044** — G115 closure: checked typed numeric boundaries, Host rollover fix, exact internal-ID counter-scan; qualified `685a2b8f478ac57fd90af613f30a684c12c99f0a`.
8. **T-045** — G302 closure: private lock files `0600`, meaningless generator append mode removed; qualified `80c78cdb611cac857ce2dc1a674e952118a945ea`.
9. **T-046 characterization/implementation** — historical exact scan split 20 G301 findings into 19 private ADGO directories and one intentionally shareable axiomgen output directory. A malformed local Git-tree candidate (`669832323de3605f19b127b06d745565cf792420`) was caught by pre-push diff inspection and never moved to `main`. Corrected implementation `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e` introduced `privateStateDirMode = 0700`, POSIX mode regressions and exact repo-wide G301 one-finding counter-scan. Security, Linux/macOS/Windows, race, lint/fuzz/examples/codegen/downstream/release/benchmark, Plan & Edge-Space, Boundary Shuffle & Sentinels, module checksum and CI Completion Gate all PASS.

---

## 6. Continuation checkpoint

CURRENT QUALIFIED HEAD: `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`  
CURRENT QUALIFIED MILESTONE: release correctness closed; deterministic quality gate restored; broad global suppression debt for G404/G101/G104/G115/G302/G301 closed; **three** gosec rule families remain globally excluded.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external blocker;
- F-005 Core/ADGO durable duplication;
- F-006 Flow crash-boundary proof gap;
- F-007 remaining global gosec exclusions `G304,G306,G703`;
- F-008 no mechanical API compatibility gate.

NEXT TASK:
- **T-047 — characterize and qualify the next global gosec exclusion.**

SELECTION GUIDANCE FOR T-047:
- run exact current G306/G304/G703 evidence before changing policy;
- G306 may be the cleanest next step if all four findings belong to intentionally shareable generated/benchmark artifacts; encode an explicit executable public-artifact contract rather than changing them to private permissions;
- G304/G703 require source/sink provenance and root-containment analysis and are higher-risk if addressed with broad path suppressions;
- choose engineering/security value, not lowest finding count alone.

VERIFICATION FOR NEXT ITERATION:
- exact-rule evidence against the qualified checkpoint HEAD;
- executable regression/counter-scan evidence for every retained exception;
- no broad replacement suppression;
- `security`, full `ci`, `quality-loop`, and `module-checksum` qualification.
