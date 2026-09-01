# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`  
Target branch: `main`  
Last qualified implementation HEAD: `dfa7e583b66c9e0a58268798303ac7ae259b066b`  
Last reconciliation: 2026-09-01

> This file is the only execution roadmap. Historical audits and topic-specific plans are evidence inputs, not parallel roadmaps. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Reconstruct state from remote `main`, this file, repository instructions and current CI before substantial work.
2. Substantial work uses `T-XXX`; substantial unexpected information uses `F-XXX` before architecture/API/security/persistence behavior is changed.
3. Red `main` blocks unrelated work. `FAIL -> rerun -> PASS` is not qualification; fix the root cause with a forward commit.
4. One logical iteration should remain atomic and reversible; implementation, executable evidence and relevant documentation belong together whenever practical.
5. Never force-push.
6. Prefer root-cause fixes, executable invariants and fail-closed behavior over scanner/test suppression.
7. Security exceptions must be narrow, mechanically constrained and independently counter-scanned when an exclusion could hide future findings.
8. Use deterministic clocks/schedulers/failpoints plus race/shuffle for timing, persistence and concurrency contracts.
9. Use mutation testing when it distinguishes semantics rather than merely increasing test count.
10. Performance work requires measurement first.
11. An implementation iteration is qualified only after its pushed SHA has green `ci`, `security`, `quality-loop`, and `module-checksum` gates.
12. A document-only reconciliation checkpoint is also qualified before the next implementation task starts.

Task states: `TODO`, `READY`, `IN_PROGRESS`, `VERIFYING`, `BLOCKED`, `DONE`, `DEFERRED`, `REJECTED`.  
Finding states: `OPEN`, `INVESTIGATING`, `VERIFYING`, `RESOLVED`, `ACCEPTED_RISK`, `REJECTED`.

---

## 1. Architecture and critical invariants

- Declarative Go `model`, AXM and TOML converge on the canonical compiled Axiom runtime.
- Typed `Flow` and `adgo` remain separate orchestration surfaces; do not merge them into a mega-runtime.
- Share only behavior-identical low-level durable primitives after executable characterization.
- External effects are **at-least-once**, never falsely exactly-once; idempotency and reconciliation are explicit.
- Durable intents/tasks are persisted before external execution where required.
- Durable Flow state + `EventHandled` + `EffectPending[]` are atomically and synchronously committed before effect delivery.
- Durable Flow crash windows are deterministically injectable through internal, execution-scoped context failpoints; no global mutable failpoint state and no exported testing API were introduced.
- The six canonical Flow failpoint stages are: before/after state+intent commit, before/after effect delivery, before/after acknowledgement commit.
- Stable durable effect IDs are the downstream idempotency key; duplicate delivery attempts after an ambiguous crash are valid, duplicate business application is the downstream concern.
- Real-Pebble crash qualification proves pending intents survive reopen, reducer state is not re-applied during effect recovery, acknowledgement failures redeliver the same stable ID, and acknowledged intents do not resurrect after reopen.
- Stale workers cannot commit through fencing.
- Execution/plan/schema identity is explicit; incompatible persisted formats fail closed.
- Same-execution mutation is serialized/validated; independent executions may progress concurrently.
- Semantic durable time uses explicit clock abstractions; governing timers are armed before an operation becomes observably started.
- Retry jitter is deterministic by execution/node identity and attempt and is never security entropy.
- Security-sensitive random identifiers use cryptographic randomness.
- Production credentials are external inputs, never intentional source literals.
- Budget aggregation is fail-closed: invalid, negative, non-finite or overflowing usage invalidates the aggregate.
- Cleanup errors from durable-store initialization and explicit close paths are not silently discarded when they can be returned or joined.
- Typed numeric boundaries reject sign changes, narrowing overflow and invalid float-to-integer conversion rather than accepting wraparound semantics.
- Private file-backed runtime state/coordination files are owner-only (`0600`) and private directories are owner-only (`0700`) unless an explicit sharing contract says otherwise.
- Generated/public artifacts are a separate permission domain: `axiomgen` output directories remain `0755`; generated source and benchmark publication files remain `0644`.
- Durable-state enumeration must not follow symlinked commit/inbox records into external filesystem content.
- Generic owned-lock filenames are exactly one component; execution IDs are encoded and derived paths remain below the private lock root.
- Content-addressed artifact references are canonical lowercase `sha256:` + 64 hex digits before path derivation; reads are rooted and cannot escape through symlinks.
- Caller-selected arbitrary paths in public `Load(path)` APIs and explicit CLI source/output options remain intentional contracts.
- Cross-platform/race/security/quality gates are never weakened merely to regain green.

### Repository/process state

- `MASTER_PLAN.md` is authoritative; active repository instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` protection disabled; T-010 remains an external blocker.
- `dfa7e583b66c9e0a58268798303ac7ae259b066b` is fully push-qualified: security, CI Completion Gate/full race and OS matrix, quality-loop and module checksum PASS; its qualification PR also passed changed-code mutation testing.
- Security workflow has **zero global gosec `-exclude=<rule>` suppressions**.
- G404/G101/G104/G115/G302/G301/G306/G703/G304 are enabled repository-wide; reviewed exceptions/findings are mechanically constrained by exact counter-scans/source sentinels where required.
- Current intentional scanner contracts include: G115 `5+4+3+4` internal-ID conversions; one G301 public codegen directory; four G306 public artifacts; 14 reviewed G703 path-provenance findings; 17 reviewed G304 arbitrary/confined path findings after removal of the real artifact traversal defect.
- Push/PR fuzz smoke is iteration-bounded (`10000x`) to avoid runner scheduling turning healthy fuzz targets into wall-clock deadline failures; nightly retains 60-second time-based deep fuzz campaigns.
- No GitHub Release had been published at the audited baseline.

---

## 2. Findings

### F-001 — Missing authoritative living execution plan
**Status:** RESOLVED by T-001.

### F-002 — `main` is not protected
**Status:** OPEN / external configuration  
**Category:** Governance / CI integrity  
**Severity:** Critical process risk  
**Task:** T-010.

### F-003 — Manual release selected caller HEAD instead of frozen candidate
**Status:** RESOLVED by T-002.

### F-004 — Release verification was weaker than normal `main`
**Status:** RESOLVED by T-003/T-004.

### F-005 — Core and ADGO duplicate durable primitives without executable anti-drift boundary
**Status:** OPEN  
**Category:** Architecture  
**Severity:** High  
**Tasks:** T-020..T-023.

### F-006 — Durable Flow crash boundaries lack comprehensive equivalence proof
**Status:** OPEN / substantially reduced by T-030/T-031  
**Category:** Persistence / reliability  
**Severity:** High  
**Evidence now closed:** T-030 provides six deterministic failpoints and exact boundary-state/reopen assertions. T-031 proves the full real-Pebble intent/effect/ack crash matrix, stable effect IDs, acknowledgement-failure recovery, recovery interruption, and no post-ack redelivery.  
**Remaining work:** T-032 generalized no-resurrection/backend/crash equivalence properties; T-033 operational outbox contracts.

### F-007 — Broad global gosec exclusions reduced security signal
**Status:** RESOLVED by T-040/T-042/T-043/T-044/T-045/T-046/T-047/T-048/T-049.  
**Qualified security implementation:** `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.

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
**Status:** RESOLVED by T-004.  
**Qualified commit:** `3a0ba3034f9202a38108fe412286f4e337a90f21`.

### F-015 — Deterministic `math/rand` use caused repository-wide G404 blindness
**Status:** RESOLVED by T-040.  
**Qualified commit:** `d611198a92f17011e487f5dba942bd2933da4a7a`.

### F-016 — G101 credential detection was globally disabled
**Status:** RESOLVED by T-042.  
**Qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

### F-017 — G101 misclassified public worker protocol version as a credential
**Status:** RESOLVED by T-042 failure recovery with one path-scoped exception and exact counter-scan.

### F-018 — Speculative budget aggregation ignored validation failure
**Status:** RESOLVED by T-043.  
**Qualified commit:** `8560aca04db9dde1777d079404ec69d1ce080044`.

### F-019 — Unchecked numeric narrowing and round-robin rollover under G115
**Status:** RESOLVED by T-044.  
**Qualified commit:** `685a2b8f478ac57fd90af613f30a684c12c99f0a`.

### F-020 — ADGO coordination lock files were world-readable
**Status:** RESOLVED by T-045.  
**Qualified commit:** `80c78cdb611cac857ce2dc1a674e952118a945ea`.

### F-021 — ADGO private runtime/durable directories were group/world traversable
**Status:** RESOLVED by T-046.  
**Qualified commit:** `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`.

### F-022 — G306 mixed public generated artifacts with private-state permission policy
**Status:** RESOLVED by T-047.  
**Qualified commit:** `66fba0d43fd440d45dfb7d64f106acb53cdf2b69`.

### F-023 — G703 findings obscured durable filesystem trust boundaries
**Status:** RESOLVED by T-048.  
**Qualified commit:** `2daa369b937ce09265f4cb4809e36497aea9308b`.

### F-024 — Content-addressed artifact digest validation was not canonical
**Status:** RESOLVED by T-049  
**Root cause:** external `ArtifactRef.Digest` was effectively length-checked before path derivation and could carry non-hex/path-separator data.  
**Resolution:** canonical lowercase SHA-256 validation, rooted reads, adversarial separator/case/hex/symlink tests.  
**Qualified commit:** `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.

### F-025 — Wall-clock fuzz smoke could fail healthy targets at the budget boundary
**Status:** RESOLVED by forward recovery `d5747471db6260d240943ca93da4b1eadb02ae97`  
**Category:** CI reliability / test determinism  
**Severity:** High process risk  
**Evidence:** first push qualification of T-030 (`5759426697e5a95208ab055d24a604e32e2ad26c`) ran `FuzzNormalize` for 163,201 executions with no discovered crash, then the Go fuzz runner returned `context deadline exceeded` at the external `-fuzztime=5s` boundary. The same exact SHA had passed the PR run.  
**Resolution:** PR/push fuzz smoke uses fixed `-fuzztime=10000x`; nightly keeps 60-second time-based parser/TRIZ/compiler fuzzing. No failed run was rerun to manufacture green.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process

- **T-001 — authoritative `MASTER_PLAN.md`: DONE**, `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- **T-004 — deterministic hedge timing: DONE**, `3a0ba3034f9202a38108fe412286f4e337a90f21`.
- **T-010 — protect `main`: BLOCKED**, external GitHub repository setting.

### M1 — Release correctness and provenance

- **T-002 — frozen release metadata resolution: DONE**, `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.
- **T-003 — single fail-closed publication/verification contract: DONE**, `8e1b11560305d56010049c992968c11f3197ca9e`.

### M2 — Durable correctness closure

#### T-030 — Deterministic durable failpoint framework
**Status:** DONE  
**Priority:** P1  
**Implementation SHA:** `5759426697e5a95208ab055d24a604e32e2ad26c`  
**Qualified recovery HEAD:** `d5747471db6260d240943ca93da4b1eadb02ae97`  
**Delivered:**
1. Internal context-scoped failpoint seam; no exported API and no global mutable test hook.
2. Six exact durable stages: before/after state+intent commit, before/after effect delivery, before/after acknowledgement commit.
3. Failpoint event metadata for flow/execution/history sequence/effect identity.
4. Exact stage-order and boundary-state tests.
5. Real Pebble close/reopen proof after synchronized state+intent commit, followed by `DrainEffects` with stable effect ID.
6. PR qualification passed CI/security/race/shuffle/mutation. First push then exposed independent F-025; forward recovery fixed the CI harness and fully push-qualified the combined HEAD.

#### T-031 — Flow intent/effect/ack crash matrix
**Status:** DONE  
**Priority:** P1  
**Qualified SHA:** `dfa7e583b66c9e0a58268798303ac7ae259b066b`  
**Delivered:**
1. Test-only real-Pebble crash/recovery matrix; no production API/code change.
2. Covered pre-commit crash, post-commit/pre-effect crash, ambiguous external application plus handler failure, post-effect/pre-ack crash, acknowledgement commit failure, post-ack crash, and recovery interrupted before delivery.
3. Exact state/history/pending/completion assertions across close/reopen cycles.
4. Stable effect ID assertions across every redelivery path.
5. Separate delivery-attempt and unique idempotent-business-application counters, proving at-least-once delivery without a false exactly-once claim.
6. Qualification PR #32 passed CI/security/race/Boundary Shuffle and changed-code mutation testing; exact SHA then fast-forwarded to `main` without force and all push gates passed.

#### T-032 — No-resurrection/backend/crash equivalence properties
**Status:** READY  
**Priority:** P1/P2  
**Depends:** T-030/T-031  
**Goal:** generalize T-031 examples into reusable properties proving that durable Flow recovery cannot resurrect completed effects or diverge semantically across supported store paths.  
**Required work:**
1. Characterize current Memory vs Pebble durable-store capabilities; do not invent durability guarantees for memory-only stores.
2. Build table/property-style traces over reducer events and 0..N effects with deterministic interruption at each T-030 failpoint stage.
3. Compare uninterrupted execution with crash/reopen execution by committed state, ordered business history, pending/completed effect set and stable effect identities.
4. Prove monotonic completion: once `EffectCompleted(id)` is durably visible, future drain/reopen cycles never invoke that ID again.
5. Prove no pending resurrection after repeated close/reopen/drain cycles.
6. Prove no reducer re-application during recovery of already committed pending effects.
7. Where a second synchronous durable backend is unavailable, factor backend-independent history/outbox properties separately from Pebble crash persistence rather than faking backend equivalence.
8. Add randomized/property traces only with deterministic seeds/reproducible counterexamples; no wall-clock dependence.
9. Keep production behavior/API unchanged unless a characterization uncovers a real defect.

- **T-033 — Flow backlog/backpressure/observability contracts:** TODO, P2, depends T-031.

### M3 — Shared durable primitives without merging engines

- **T-020 — inventory Core vs ADGO durable primitive contracts:** TODO, P1.
- **T-021 — define acyclic shared durable boundary:** TODO, P1/P2, depends T-020.
- **T-022 — extract behavior-identical pure primitives:** TODO, P2, depends T-021.
- **T-023 — architecture anti-drift tests:** TODO, P2, depends T-021.

### M4 — Security boundary reduction

- **T-040 — G404 closure: DONE**, `d611198a92f17011e487f5dba942bd2933da4a7a`.
- **T-042 — G101 closure: DONE**, `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.
- **T-043 — G104 closure: DONE**, `8560aca04db9dde1777d079404ec69d1ce080044`.
- **T-044 — G115 closure: DONE**, `685a2b8f478ac57fd90af613f30a684c12c99f0a`.
- **T-045 — G302 closure: DONE**, `80c78cdb611cac857ce2dc1a674e952118a945ea`.
- **T-046 — G301 closure: DONE**, `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`.
- **T-047 — G306 closure: DONE**, `66fba0d43fd440d45dfb7d64f106acb53cdf2b69`.
- **T-048 — G703 closure: DONE**, `2daa369b937ce09265f4cb4809e36497aea9308b`.
- **T-049 — G304 closure: DONE**, `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.
- **T-041 — supply-chain provenance, remaining permission minimization, container digest pinning:** TODO, P2.

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
- REJECTED: retry-away/redrive failed qualification to regain green without a root-cause fix.
- REJECTED: weaken tests/scanners to regain green.
- REJECTED: global mutable failpoint state for durable Flow tests.
- REJECTED: expose a public failpoint API before a stable external testing contract is justified.
- REJECTED: replace deterministic retry jitter with cryptographic randomness merely to silence SAST.
- REJECTED: restore any global gosec rule exclusion after characterization.
- REJECTED: path-exclude a security boundary without an independent raw counter-scan.
- REJECTED: make generated/public artifacts private merely to satisfy permission scanners.
- REJECTED: follow symlinked durable records as ordinary state files.
- REJECTED: accept arbitrary path components in private lock filenames.
- REJECTED: restrict caller-selected `Load(path)`/CLI paths solely to silence G304.
- REJECTED: remove time-based deep fuzzing; it remains in nightly while push/PR smoke is iteration-bounded.
- REJECTED: tag-push as a second release publisher.
- DEFERRED: generated typed-activity specialization until profiling proves value.

---

## 5. Iteration log

1. **T-001** — authoritative plan established; `414f01c84ec215b29784cbfa7e5987cb35cdea41`; gates PASS.
2. **T-002** — frozen release metadata; `094de51e4e42d72d4bdb4f813f342cee71f9ac87`; gates PASS.
3. **T-003/T-004** — fail-closed release path + hedge timer race fix; `8e1b11560305d56010049c992968c11f3197ca9e` / `3a0ba3034f9202a38108fe412286f4e337a90f21`.
4. **T-040..T-046** — progressive SAST closure for G404/G101/G104/G115/G302/G301 with real defects fixed and intentional cases bounded.
5. **T-047 characterization** — `85146808ff742c6c600b3d1d9eaa2a539a6fe067`: `G304=18`, `G306=4`, `G703=14`.
6. **T-047** — G306 closure; `66fba0d43fd440d45dfb7d64f106acb53cdf2b69`.
7. **T-048** — G703 closure; forward fix `2daa369b937ce09265f4cb4809e36497aea9308b` after a premature placeholder checkpoint; no history rewrite/force push.
8. **T-049** — raw characterization PR found 18 G304 sinks; real CAS artifact traversal fixed, global G304 exclusion removed, remaining 17 findings exact-guarded. Corrected implementation `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd` fully qualified.
9. **T-030** — deterministic Flow durable failpoints: commits `492f3cc955e0d26283fdaaba027eda581ea13315`, `c992c3eb704de891063b614b52cc354afe3d5986`, `5759426697e5a95208ab055d24a604e32e2ad26c`; PR #30 passed CI/security/shuffle/mutation and exact SHA was fast-forwarded to `main`.
10. **F-025 discovered during T-030 push qualification** — first push CI failed only TRIZ fuzz smoke after 163,201 successful executions with `context deadline exceeded` at the 5s budget. Failure was not rerun.
11. **F-025 forward recovery** — `d5747471db6260d240943ca93da4b1eadb02ae97` changed PR/push fuzz smoke to `10000x`, retained nightly 60s deep fuzzing, passed PR preflight then was fast-forwarded without force. Push security, full CI including Linux/macOS/Windows/race/fuzz, quality-loop and module checksum all PASS.
12. **T-031** — test-only real-Pebble crash/recovery matrix; `dfa7e583b66c9e0a58268798303ac7ae259b066b`. PR #32 passed CI/security/race/Boundary Shuffle and changed-code mutation testing. Exact SHA was fast-forwarded to `main`; push module-checksum, security, CI Completion Gate/full OS/race/fuzz and quality-loop all PASS.

---

## 6. Continuation checkpoint

CURRENT QUALIFIED IMPLEMENTATION HEAD: `dfa7e583b66c9e0a58268798303ac7ae259b066b`  
CURRENT QUALIFIED MILESTONE: release correctness closed; all broad global gosec exclusions closed; T-030 deterministic durable failpoints and T-031 full real-Pebble crash matrix closed; F-025 CI nondeterminism closed.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external GitHub configuration blocker;
- F-005 Core/ADGO durable primitive duplication;
- F-006 generalized Flow no-resurrection/backend equivalence proof gap;
- F-008 no mechanical API compatibility gate.

NEXT TASK AFTER THIS DOCUMENT CHECKPOINT QUALIFIES:
- **T-032 — no-resurrection/backend/crash equivalence properties.**

T-032 SELECTION RATIONALE:
- T-031 proves every named crash window with concrete real-Pebble scenarios; the next reliability step is converting those examples into reusable monotonic/no-resurrection properties;
- generalized equivalence catches interaction bugs across multiple events/effects that a fixed seven-case matrix may miss;
- the task can remain test-only unless characterization reveals a real defect;
- T-032 finishes the correctness proof layer needed before T-033 operational backlog/backpressure work.

VERIFICATION FOR T-032:
- deterministic property/table traces over multiple events/effects;
- uninterrupted vs interrupted committed-state/history equivalence;
- monotonic completion/no post-completion delivery;
- repeated reopen/drain no-resurrection;
- no reducer re-application during pending-effect recovery;
- backend-independent history/outbox properties separated from Pebble crash persistence if no second synchronous durable backend exists;
- reproducible seeds/counterexamples only; no sleeps or wall-clock dependence;
- changed-code mutation testing where applicable;
- pushed SHA green on `security`, full `ci`, `quality-loop`, and `module-checksum`.