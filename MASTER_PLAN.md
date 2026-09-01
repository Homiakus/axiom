# Axiom Living Master Plan

Status: **ACTIVE — authoritative execution source of truth**

Repository: `Homiakus/axiom`  
Target branch: `main`  
Last qualified implementation HEAD: `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`  
Last reconciliation: 2026-09-01

> This file is the only execution roadmap. Historical plans, audits and topic-specific documents are evidence inputs, not parallel roadmaps. Observable behavior, reproducible tests, security/correctness invariants and code outrank stale prose.

---

## 0. Operating protocol

`SYNCHRONIZE -> OBSERVE -> UNDERSTAND -> SELECT -> CHARACTERIZE -> IMPLEMENT -> VERIFY -> ATTACK -> LEARN -> RECONCILE -> COMMIT -> PUSH MAIN -> CHECKPOINT -> REPEAT`

1. Reconstruct state from remote `main`, this file, repository instructions and current CI before substantial work.
2. Substantial work uses `T-XXX`; substantial unexpected information uses `F-XXX` before architecture/API/security/persistence behavior is changed.
3. Red `main` blocks unrelated work. Flaky `FAIL -> retry -> PASS` is not qualification.
4. One logical iteration should remain atomic and reversible; implementation, executable evidence and relevant documentation belong together whenever practical.
5. Never force-push.
6. Prefer root-cause fixes, executable invariants and fail-closed behavior over scanner/test suppression.
7. Security exceptions must be narrow, mechanically constrained and justified. Any path exception that can hide future findings of the same rule requires an independent repo-wide counter-scan.
8. Use deterministic clocks/schedulers plus race/shuffle for timing and concurrency contracts; use mutation testing when it distinguishes semantics.
9. Performance work requires measurement first.
10. An implementation iteration is qualified only after its pushed SHA has green `ci`, `security`, `quality-loop`, and `module-checksum` gates.
11. A document-only reconciliation checkpoint is also qualified before the next implementation task starts.

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
- Private file-backed runtime state and coordination files are owner-only (`0600`) unless an explicit sharing contract states otherwise.
- Private file-backed runtime state and coordination directories are owner-only (`0700`) unless an explicit sharing contract states otherwise.
- Generated source/publication artifacts are a separate permission domain: `axiomgen` output directories intentionally remain `0755`; generated source and benchmark publication files intentionally remain `0644`.
- File-backed durable-state enumeration must not follow symlinked commit/inbox records into external filesystem content.
- Generic owned-lock filenames must be exactly one path component; execution IDs are encoded and derived lock paths remain below the private lock root.
- Content-addressed artifact references are canonical lowercase `sha256:` plus 64 hexadecimal digits before path derivation; artifact reads are rooted and must not follow symlink traversal outside the CAS root.
- Caller-selected arbitrary filesystem paths in public `Load(path)` APIs and explicit CLI source/output options remain intentional contracts; SAST must not silently narrow them.
- Cross-principal shared-filesystem deployments require explicit external ACL/permission configuration; private defaults are not a substitute for a multi-principal ACL design.
- Parsed syntax is not a runtime guarantee until executable behavior proves it.
- Cross-platform/race/security/quality gates are never weakened merely to regain green.

### Repository/process state

- `MASTER_PLAN.md` is authoritative; `AGENTS.md` is absent; active repository instructions are primarily `CONTRIBUTING.md` and `DEVELOPMENT.md`.
- GitHub reports `main` protection disabled; T-010 remains an external blocker.
- `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd` is fully push-qualified: security, CI Completion Gate/full race and OS matrix, quality-loop, and module checksum PASS.
- The security workflow has **no global gosec `-exclude=<rule>` suppression remaining**.
- G404 is enabled repository-wide with one path-scoped deterministic-jitter exception guarded by source sentinels.
- G101 is enabled repository-wide; the public HTTP worker protocol-version false positive is path-scoped and independently counter-scanned.
- G104 is enabled repository-wide with no suppression.
- G115 is enabled repository-wide; 16 structurally bounded internal-ID conversions remain across four reviewed paths and are exact-counter-scanned as `5+4+3+4`.
- G302 is enabled repository-wide; ADGO lock files use `0600`.
- G301 is enabled repository-wide; 19 ADGO private-state directories use `0700`; the sole intentional `axiomgen` `0755` output-directory finding is independently constrained.
- G306 is enabled repository-wide; four intentional public-artifact findings are exact-counter-scanned as `cmd/axiombench/main.go=2 + cmd/axiomgen/internal/generate/generate.go=2`.
- G703 is enabled repository-wide; reviewed ADGO path-provenance findings are exact-counter-scanned as `adgo/catalog.go=1 + adgo/file_lock.go=5 + adgo/file_lock_heartbeat.go=1 + adgo/store.go=7`.
- G304 is enabled repository-wide. Exact T-049 characterization measured 18 findings: 8 intentional caller-selected arbitrary-path API/CLI sinks, 9 internally confined durable-path sinks, and 1 real artifact traversal defect. The defect is fixed; an independent raw G304 scan now requires exactly the remaining 17 reviewed findings and structural source/sink sentinels.
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

### F-007 — Broad global gosec exclusions reduced security signal
**Status:** RESOLVED by T-040/T-042/T-043/T-044/T-045/T-046/T-047/T-048/T-049  
**Category:** Security / signal quality  
**Severity:** High  
**Resolution:** every previously global rule family is enabled; intentional exceptions are path-scoped and independently constrained. T-049 removed the final global `G304` exclusion.  
**Qualified implementation:** `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.

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
**Qualified commit:** `d611198a92f17011e487f5dba942bd2933da4a7a`.

### F-016 — G101 hardcoded-credential detection was globally disabled
**Status:** RESOLVED by T-042  
**Qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

### F-017 — G101 misclassified the public HTTP worker protocol version as a credential
**Status:** RESOLVED by T-042 failure recovery  
**Resolution:** one path-scoped false-positive exception plus exact independent counter-scan.  
**Qualified commit:** `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.

### F-018 — Speculative budget aggregation ignored validation failure
**Status:** RESOLVED by T-043  
**Category:** Correctness / budget safety  
**Severity:** High  
**Qualified commit:** `8560aca04db9dde1777d079404ec69d1ce080044`.

### F-019 — Unchecked numeric narrowing and round-robin counter rollover under G115
**Status:** RESOLVED by T-044  
**Category:** Correctness / numeric safety  
**Severity:** High  
**Qualified commit:** `685a2b8f478ac57fd90af613f30a684c12c99f0a`.

### F-020 — ADGO coordination lock files were created world-readable
**Status:** RESOLVED by T-045  
**Category:** Security / filesystem confidentiality  
**Severity:** Medium  
**Qualified commit:** `80c78cdb611cac857ce2dc1a674e952118a945ea`.

### F-021 — ADGO private runtime/durable directories were group/world traversable
**Status:** RESOLVED by T-046  
**Category:** Security / filesystem confidentiality  
**Severity:** Medium  
**Qualified commit:** `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`.

### F-022 — G306 permission findings mixed public generated artifacts with private-state policy
**Status:** RESOLVED by T-047  
**Category:** Security / filesystem permission semantics  
**Resolution:** public/generated outputs remain intentionally `0644`; exact four-finding counter-scan prevents expansion.  
**Qualified commit:** `66fba0d43fd440d45dfb7d64f106acb53cdf2b69`.

### F-023 — G703 path-taint findings obscured durable filesystem trust boundaries
**Status:** RESOLVED by T-048  
**Category:** Security / filesystem traversal / durable state  
**Severity:** High  
**Resolution:** exact 14-finding counter-scan, symlink record rejection, lock leaf validation and execution-lock containment.  
**Qualified commit:** `2daa369b937ce09265f4cb4809e36497aea9308b`.

### F-024 — Content-addressed artifact digest validation was not canonical
**Status:** RESOLVED by T-049  
**Category:** Security / path traversal / artifact integrity  
**Severity:** High  
**Root cause:** externally supplied `ArtifactRef.Digest` was validated only by length after stripping `sha256:` and could therefore carry non-hex/path-separator data.  
**Resolution:** require canonical lowercase `sha256:` + 64 hex digits, decode to exactly 32 bytes, derive only validated relative paths, and use `os.OpenInRoot`/`os.OpenRoot` for retrieval/existence checks. Adversarial tests cover invalid prefix/length/case/hex, `/`, `\\`, normal round-trip and symlink escape.  
**Qualified commit:** `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.

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
**Status:** READY  
**Priority:** P1  
**Goal:** make every durable Flow crash boundary deterministically interruptible and replayable without production-only timing tricks.  
**Required work:**
1. Inventory state/intent/effect/ack persistence boundaries in Flow and its durable store adapters.
2. Introduce a test-only/internal failpoint interface with named deterministic stages; no global mutable hooks in production semantics.
3. Prove failpoints are inert when disabled and composable with Memory/Pebble/File-backed test substrates as applicable.
4. Add deterministic crash/reopen harness utilities that can stop exactly before/after a durable boundary.
5. Keep failure injection below public API unless a stable testing API is explicitly justified.
6. Verify race/shuffle and existing replay semantics remain unchanged.

- **T-031 — Flow intent/effect/ack crash matrix:** TODO, P1, depends T-030.
- **T-032 — no-resurrection/backend/crash equivalence properties:** TODO, P1/P2, depends T-030.
- **T-033 — Flow backlog/backpressure/observability contracts:** TODO, P2, depends T-031.

### M3 — Shared durable primitives without merging engines

- **T-020 — inventory Core vs ADGO durable primitive contracts:** TODO, P1.
- **T-021 — define acyclic shared durable boundary:** TODO, P1/P2, depends T-020.
- **T-022 — extract behavior-identical pure primitives:** TODO, P2, depends T-021.
- **T-023 — architecture anti-drift tests:** TODO, P2, depends T-021.

### M4 — Security boundary reduction

- **T-040 — G404 globally + deterministic-jitter exception contract: DONE**, `d611198a92f17011e487f5dba942bd2933da4a7a`.
- **T-042 — G101 globally + protocol-version false-positive contract: DONE**, `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.
- **T-043 — G104 globally + ignored-error closure: DONE**, `8560aca04db9dde1777d079404ec69d1ce080044`.
- **T-044 — G115 globally + checked numeric boundaries: DONE**, `685a2b8f478ac57fd90af613f30a684c12c99f0a`.
- **T-045 — G302 globally + private lock-file permissions: DONE**, `80c78cdb611cac857ce2dc1a674e952118a945ea`.
- **T-046 — G301 globally + private state-directory permissions: DONE**, `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`.
- **T-047 — G306 globally + public artifact permission contract: DONE**, `66fba0d43fd440d45dfb7d64f106acb53cdf2b69`.
- **T-048 — G703 globally + durable path-provenance hardening: DONE**, `2daa369b937ce09265f4cb4809e36497aea9308b`.
- **T-049 — G304 globally + canonical artifact path boundary: DONE**, `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`.
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
- REJECTED: restore any global gosec rule exclusion after its contract has been characterized.
- REJECTED: path-exclude a security boundary without an independent raw counter-scan.
- REJECTED: make generated/public artifacts private merely to satisfy permission scanners.
- REJECTED: follow symlinked durable records as ordinary state files.
- REJECTED: accept arbitrary path components in private lock filenames.
- REJECTED: treat all G304 findings as false positives; T-049 proved a real artifact traversal defect existed.
- REJECTED: restrict caller-selected `Load(path)`/CLI source-output semantics solely to silence G304.
- REJECTED: tag-push as a second release publisher.
- DEFERRED: generated typed-activity specialization until profiling proves value.

---

## 5. Iteration log

1. **T-001** — authoritative plan established; `414f01c84ec215b29784cbfa7e5987cb35cdea41`; gates PASS.
2. **T-002** — frozen release metadata; `094de51e4e42d72d4bdb4f813f342cee71f9ac87`; gates PASS.
3. **T-003/T-004** — fail-closed single release path plus deterministic hedge-timer race fix; `8e1b11560305d56010049c992968c11f3197ca9e` / `3a0ba3034f9202a38108fe412286f4e337a90f21`.
4. **T-040** — G404 global enablement with deterministic-jitter sentinel; `d611198a92f17011e487f5dba942bd2933da4a7a`.
5. **T-042** — G101 activation exposed a scanner false positive; exact counter-scan used instead of global rollback; `668c8f77f0619aa8b88cc7dc0002d31651deedcf`.
6. **T-043** — G104 closure including fail-closed speculative budget accounting; `8560aca04db9dde1777d079404ec69d1ce080044`.
7. **T-044** — G115 closure: checked typed numeric boundaries, Host rollover fix, exact internal-ID counter-scan; `685a2b8f478ac57fd90af613f30a684c12c99f0a`.
8. **T-045** — G302 closure: private lock files `0600`; `80c78cdb611cac857ce2dc1a674e952118a945ea`.
9. **T-046** — G301 closure: 19 ADGO private directories moved to `0700`; one public codegen directory exact-guarded; `abb08b7381ad7ff5eaa750ccf1e6fc2081c4454e`. Malformed local candidate `669832323de3605f19b127b06d745565cf792420` was caught before push.
10. **T-047 characterization** — `85146808ff742c6c600b3d1d9eaa2a539a6fe067` measured `G304=18`, `G306=4`, `G703=14`.
11. **T-047 implementation** — G306 globally enabled; exact four-finding public-artifact guard; `66fba0d43fd440d45dfb7d64f106acb53cdf2b69`.
12. **T-048** — G703 globally enabled with exact 14-finding guard; symlinked durable records rejected, generic lock leaf validation and execution-lock containment added. Premature staging commit `f8d83d2120e031f3217dc2566b6f2f831ff6af27` containing a placeholder reached `main`; history was not rewritten. It was immediately superseded by forward fix `2daa369b937ce09265f4cb4809e36497aea9308b`, which fully qualified.
13. **T-049 characterization** — temporary branch/PR #28 enabled raw G304 against qualified baseline without touching `main`; exact scan found 18 sinks: 8 caller-selected arbitrary-path contracts, 9 internally confined durable paths, 1 real defect in `ContentAddressedStore`.
14. **T-049 implementation preflight** — branch/PR #29 added canonical artifact digest validation, rooted artifact reads, adversarial tests, global G304 enablement and exact 17-finding counter-scan. First candidate `616373e81c0b1031551fad25be70a3fdd6d885bb` failed lint/unit compile because an internal permission test still used `ContentAddressedStore.path`; it never moved to `main`.
15. **T-049 corrected implementation** — forward fix `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd` restored `path(ref)` only as a validated inspection helper while production reads remain rooted. PR exact-SHA security/CI/quality including changed-code mutation testing passed. The exact SHA was then fast-forwarded to `main` without force; GitHub subsequently marks PR #29 merged because its head became identical to `main`, with no separate merge commit. Push security, full CI/race/OS matrix, quality-loop and module checksum all PASS.

---

## 6. Continuation checkpoint

CURRENT QUALIFIED IMPLEMENTATION HEAD: `ebdb71db29d74effa3ea5c8bd21bb7ff50d3dfbd`  
CURRENT QUALIFIED MILESTONE: release correctness closed; deterministic quality gate restored; **all previously broad global gosec rule exclusions are closed**; security exceptions are now narrow and independently audited.

OPEN CRITICAL/HIGH:
- F-002 unprotected `main` — external GitHub configuration blocker;
- F-005 Core/ADGO durable primitive duplication;
- F-006 Flow crash-boundary proof gap;
- F-008 no mechanical API compatibility gate.

NEXT TASK AFTER THIS DOCUMENT CHECKPOINT QUALIFIES:
- **T-030 — deterministic durable failpoint framework.**

T-030 SELECTION RATIONALE:
- F-006 is the highest remaining product-correctness risk that can be advanced entirely in code;
- durable external effects are at-least-once, so deterministic proof around persist/effect/ack crash windows has more safety value than adding new features;
- T-030 unlocks T-031 crash matrix and T-032 no-resurrection/backend equivalence properties;
- T-010 remains important but is external repository configuration and must not block code-correctness progress.

VERIFICATION FOR T-030:
- exact checkpoint HEAD synchronization before code changes;
- named deterministic failpoint stages around durable state/intent/effect/ack boundaries;
- no production semantic change when failpoints are disabled;
- deterministic crash/reopen tests rather than sleeps/timing dependence;
- Memory/durable backend behavior characterization where applicable;
- race and boundary shuffle remain green;
- `security`, full `ci`, `quality-loop`, and `module-checksum` qualification.
