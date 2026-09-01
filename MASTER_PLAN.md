# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`  
Target branch: `main`  
Last qualified implementation HEAD: `33be44e7ccd947f03782d73c430b413fffec4a41`  
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
- Stable durable effect IDs are downstream idempotency keys; duplicate delivery attempts after an ambiguous crash are valid, duplicate business application is the downstream concern.
- Real-Pebble crash qualification proves pending intents survive reopen, reducer state is not re-applied during post-commit recovery, acknowledgement failures redeliver the same stable ID, and acknowledged intents do not resurrect.
- `MemoryFlowStore` is intentionally ephemeral and must not be treated as a crash-durable backend; `PebbleFlowStore` is the built-in synchronous durable Flow backend.
- Uninterrupted and interrupted/reopened durable Flow traces must converge to equivalent committed state, normalized ordered history and idempotent business applications.
- Once `EffectCompleted(id)` is durably visible, repeated drain/reopen cycles must never deliver `id` again.
- Operational outbox controls must remain a Flow concern only: do not build a second scheduler/worker runtime inside Flow.
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
- `33be44e7ccd947f03782d73c430b413fffec4a41` is fully push-qualified: security, CI Completion Gate/full race and OS matrix, quality-loop and module checksum PASS; its qualification PR also passed changed-code mutation testing.
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

### F-006 — Durable Flow crash boundaries lacked comprehensive equivalence proof
**Status:** RESOLVED by T-030/T-031/T-032  
**Category:** Persistence / reliability  
**Severity:** High  
**Resolution:** six deterministic crash boundaries, full real-Pebble intent/effect/ack recovery matrix, stable effect-ID/redelivery assertions, generalized uninterrupted-vs-crash/reopen equivalence traces, monotonic completion and repeated no-resurrection cycles. Memory is explicitly classified ephemeral rather than presented as a second durable backend.  
**Qualified implementation:** `33be44e7ccd947f03782d73c430b413fffec4a41`.  
**Operational follow-up:** T-033 backlog/backpressure/observability is a separate P2 production-operability task, not an unresolved crash-correctness proof.

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
**Resolution:** canonical lowercase SHA-256 validation, rooted reads, adversarial separator/case/hex/symlink tests.  
**Qualified commit:** `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.

### F-025 — Wall-clock fuzz smoke could fail healthy targets at the budget boundary
**Status:** RESOLVED by forward recovery `d5747471db6260d240943ca93da4b1eadb02ae97`  
**Category:** CI reliability / test determinism  
**Severity:** High process risk  
**Resolution:** PR/push fuzz smoke uses fixed `-fuzztime=10000x`; nightly keeps 60-second time-based parser/TRIZ/compiler fuzzing. The failed push was not rerun to manufacture green.

---

## 3. Prioritized task DAG

### M0 — Trustworthy execution process

- **T-001 — authoritative `MASTER_PLAN.md`: DONE**, `414f01c84ec215b29784cbfa7e5987cb35cdea41`.
- **T-004 — deterministic hedge timing: DONE**, `3a0ba3034f9202a38108fe412286f4e337a90f21`.
- **T-010 — protect `main`: BLOCKED**, external GitHub repository setting.

### M1 — Release correctness and provenance

- **T-002 — frozen release metadata resolution: DONE**, `094de51e4e42d72d4bdb4f813f342cee71f9ac87`.
- **T-003 — single fail-closed publication/verification contract: DONE**, `8e1b11560305d56010049c992968c11f3197ca9e`.

### M2 — Durable Flow correctness and operations

#### T-030 — Deterministic durable failpoint framework
**Status:** DONE  
**Qualified recovery HEAD:** `d5747471db6260d240943ca93da4b1eadb02ae97`.

#### T-031 — Flow intent/effect/ack crash matrix
**Status:** DONE  
**Qualified SHA:** `dfa7e583b66c9e0a58268798303ac7ae259b066b`.

#### T-032 — No-resurrection/backend/crash equivalence properties
**Status:** DONE  
**Priority:** P1/P2  
**Qualified SHA:** `33be44e7ccd947f03782d73c430b413fffec4a41`  
**Delivered:**
1. Explicit capability classification: `MemoryFlowStore` is ephemeral/non-durable; `PebbleFlowStore` is synchronous and implements `DurableFlowStore`.
2. Backend-independent synchronous outbox model proving completion monotonicity and repeated-drain no-resurrection.
3. Real-Pebble multi-event/multi-effect reference trace compared with 10 deterministic crash/reopen scenarios covering all six failpoint stages and first/second-effect occurrences.
4. Final committed state, normalized ordered history, stable effect IDs and idempotent business applications converge with uninterrupted execution.
5. Post-commit crash recovery never re-applies the reducer; pre-commit interruption is explicitly distinguished as non-durable speculative reducer work requiring business-event redispatch.
6. Three additional close/reopen/drain cycles after completion prove no effect/history/state resurrection.
7. Qualification PR #33 passed CI/security/race/Boundary Shuffle and changed-code mutation testing; exact SHA was fast-forwarded to `main` without force and all push gates passed.

#### T-033 — Flow outbox backlog/backpressure/observability contract
**Status:** READY  
**Priority:** P2  
**Depends:** T-031/T-032  
**Characterization:** current `drainDurableEffectsLocked` loads complete history, reconstructs the completed set, then walks every pending entry until all recoverable work is delivered. No explicit backlog diagnostic or bounded-drain API exists. Historical FLOW-002/FLOW-003 tasks remain TODO.  
**Goal:** make durable Flow outbox state operationally inspectable and recovery work explicitly bounded without building a second scheduler/worker runtime or weakening delivery order.  
**Required work:**
1. Define a bounded-cardinality diagnostic snapshot derived from durable history: pending count, completed count relevant to the snapshot, oldest pending sequence/time, and whether recovery work remains. Do not label metrics with execution/effect IDs in aggregate telemetry contracts.
2. Define a bounded drain primitive with an explicit maximum number of effect deliveries/acknowledgements per call; zero/negative/overflowing limits must fail clearly or use an explicitly documented semantic, never silently mean unlimited.
3. Preserve strict pending-effect order and stable effect IDs under bounded draining.
4. A bounded drain that reaches its limit must return enough state for the caller to know more work remains without treating the partial successful drain as an error.
5. Existing `DrainEffects(ctx)` compatibility must remain intact; it may delegate to the bounded primitive with the legacy drain-all behavior if that does not create ambiguous semantics.
6. `Dispatch` must continue draining older pending effects before reducing a new event. Do not silently change dispatch ordering or allow new business events to leapfrog backlog.
7. Add tests for 0, 1, N and >N pending effects, partial drain/reopen/resume, failure on an item before the limit, completion monotonicity, and no duplicate delivery after the final page.
8. Add a large-history characterization/benchmark before optimizing history scans. Do not add indexes/compaction/persisted-format changes in the same iteration unless measurement proves they are necessary.
9. Prefer a minimal stable API surface. If an exported diagnostic/bounded-drain API is required, document its compatibility status and avoid embedding worker/scheduler policy.
10. Keep persisted format unchanged unless a separately planned compatibility task is created.

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
- REJECTED: build a second scheduler/worker runtime inside Flow to manage the outbox.
- REJECTED: exactly-once claims for external effects.
- REJECTED: retry-away/redrive failed qualification to regain green without a root-cause fix.
- REJECTED: weaken tests/scanners to regain green.
- REJECTED: global mutable failpoint state for durable Flow tests.
- REJECTED: expose a public failpoint API before a stable external testing contract is justified.
- REJECTED: pretend `MemoryFlowStore` provides crash durability for backend-equivalence tests.
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
4. **T-040..T-049** — progressive SAST closure; all broad global gosec exclusions eliminated and real security defects fixed. Final security baseline `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.
5. **T-030** — deterministic Flow durable failpoints; implementation `5759426697e5a95208ab055d24a604e32e2ad26c`; PR #30 passed CI/security/shuffle/mutation.
6. **F-025/T-030 forward recovery** — push fuzz deadline flake was fixed, not rerun; qualified combined HEAD `d5747471db6260d240943ca93da4b1eadb02ae97`.
7. **T-031** — real-Pebble seven-case crash/recovery matrix; `dfa7e583b66c9e0a58268798303ac7ae259b066b`; PR #32 and all push gates PASS.
8. **T-032** — capability classification + synchronous model monotonicity + 10 real-Pebble uninterrupted/crash equivalence scenarios; `33be44e7ccd947f03782d73c430b413fffec4a41`; PR #33 mutation/security/CI/shuffle PASS, exact SHA fast-forwarded to `main`, all push gates PASS.

---

## 6. Continuation checkpoint

CURRENT QUALIFIED IMPLEMENTATION HEAD: `33be44e7ccd947f03782d73c430b413fffec4a41`  
CURRENT QUALIFIED MILESTONE: release correctness closed; all broad global gosec exclusions closed; durable Flow crash correctness is closed through deterministic failpoints, concrete recovery matrix and generalized no-resurrection/equivalence properties.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external GitHub configuration blocker;
- F-005 Core/ADGO durable primitive duplication;
- F-008 no mechanical API compatibility gate.

NEXT TASK AFTER THIS DOCUMENT CHECKPOINT QUALIFIES:
- **T-033 — Flow outbox backlog/backpressure/observability contract.**

T-033 SELECTION RATIONALE:
- crash correctness is now proven; the remaining Flow gap is operational inspectability and explicitly bounded recovery work;
- current drain loads/scans complete history and drains all pending effects in one call, with no explicit backlog status or caller-controlled work bound;
- closing this task completes the Flow-specific stabilization chain before returning to broader architecture/API work;
- the task must remain a thin reducer-outbox capability and must not evolve Flow into a worker engine.

VERIFICATION FOR T-033:
- capability/API characterization before code changes;
- pending/backlog snapshot with bounded-cardinality semantics;
- bounded drain preserving order, stable IDs and acknowledgement monotonicity;
- partial drain + reopen + resume tests;
- failure before/at limit tests;
- legacy `DrainEffects` compatibility;
- large-history benchmark/characterization before any indexing or persisted-format optimization;
- changed-code mutation testing where applicable;
- pushed SHA green on `security`, full `ci`, `quality-loop`, and `module-checksum`.