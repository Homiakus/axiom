# Architecture FMEA and risk tracking

Status: **canonical engineering risk register companion to `MASTER_PLAN.md`**  
Machine-readable source: [`architecture-risk-register.json`](architecture-risk-register.json)  
Updated: 2026-09-04

## 1. Role in the planning system

`MASTER_PLAN.md` remains the only execution roadmap. This document does **not** create a second task list.

The planning model is deliberately split into three identities:

- `F-XXX` — observed fact, defect, limitation or evidence;
- `R-XXX` — prospective failure mode whose probability/detectability must be controlled;
- `T-XXX` — executable mitigation, verification or productization work in `MASTER_PLAN.md`.

A risk is therefore useful only when it is connected to evidence and executable work:

```text
observed evidence (F-XXX)
          |
          v
failure mode (R-XXX) --FMEA--> current priority / residual target
          |
          v
mitigation task(s) (T-XXX)
          |
          v
executable evidence -> residual FMEA -> CLOSED / ACCEPTED
```

The machine-readable register is mechanically validated by `architecture_risk_register_test.go`. The test recalculates RPN, checks the lifecycle fields, and fails if a referenced finding/task is absent from `MASTER_PLAN.md`.

## 2. FMEA scoring

Each risk is scored on the classic 1–10 dimensions:

| Dimension | Meaning | 1 | 10 |
|---|---|---|---|
| Severity (`S`) | consequence if the failure mode occurs | negligible/local | catastrophic correctness, durability, safety or control-boundary impact |
| Occurrence (`O`) | likelihood/frequency without further mitigation | exceptional | expected/frequent |
| Detectability (`D`) | difficulty of detecting the failure before impact | almost certain early detection | difficult to detect before impact |

`RPN = S × O × D`.

Priority is intentionally not pure RPN because low occurrence must not erase catastrophic severity:

- **critical** — `RPN >= 300`;
- **high** — `RPN >= 180` **or** `S >= 9`;
- **medium** — `RPN >= 80`;
- **low** — otherwise.

RPN is a prioritization aid, not a proof of safety. A risk with severity 9–10 remains high even when good controls make occurrence small.

Mitigation normally reduces `O` and/or `D`. Severity should only be reduced when the actual consequence of the failure mode has changed, not merely because more tests were added.

## 3. Risk lifecycle

Allowed states:

- `OPEN` — known failure mode; mitigation is not yet complete;
- `MITIGATING` — active controls exist and linked tasks continue reducing occurrence/detectability;
- `VERIFYING` — implementation is believed to address the risk but exact acceptance evidence is not yet complete;
- `ACCEPTED` — residual risk is explicitly accepted; rationale and a review trigger remain mandatory;
- `CLOSED` — target residual risk and exit evidence are satisfied.

Planning rules:

1. Every **critical/high** open risk must have at least one linked `T-XXX` mitigation in `MASTER_PLAN.md`.
2. A task that mitigates a risk is not fully reconciled as `DONE` until the linked `R-XXX` entry is reviewed and its residual `O/D/RPN` are updated from evidence.
3. A risk is not `CLOSED` merely because code merged; its `exit_evidence` must be satisfied.
4. A new architecture/API/security/persistence finding with a plausible future recurrence must either create a new `R-XXX` or explicitly update an existing one.
5. Architecture changes should record `Risk impact: R-...` (or `none` with rationale) during planning/reconciliation so an existing control is not silently weakened.
6. `ACCEPTED` risks remain visible and must have a `review_trigger`; acceptance is not deletion.
7. Changes to a control listed under `current_controls` trigger re-review even if the linked risk was previously closed.
8. The register is version-controlled evidence. Runtime `RiskLevel` or provider/node risk policy is a different concept and must not be conflated with engineering FMEA.

## 4. Initial architecture FMEA

| Risk | Failure mode | S | O | D | RPN | Priority | State | Linked plan work | Target RPN |
|---|---|---:|---:|---:|---:|---|---|---|---:|
| `R-001` | Duplicate irreversible external effect after ambiguous crash when consumer idempotency/reconciliation is incomplete | 10 | 4 | 7 | 280 | high | OPEN | `T-083` | 90 |
| `R-002` | Multi-host Store violates CAS/lease/fencing/inbox/transaction semantics | 10 | 5 | 6 | 300 | **critical** | OPEN | `T-084`, `T-086` | 60 |
| `R-003` | Core/Flow/ADGO semantic drift or false equivalence | 8 | 5 | 5 | 200 | high | MITIGATING | `T-081`, `T-086` | 72 |
| `R-004` | Production miscomposition bypasses intended durability/hard controls | 9 | 5 | 6 | 270 | high | OPEN | `T-082`, `T-083`, `T-085`, `T-086` | 54 |
| `R-005` | Clock-domain / multi-snapshot TOCTOU around retry, lease or schedule boundaries | 9 | 4 | 6 | 216 | high | MITIGATING | `T-081`, `T-086` | 54 |
| `R-006` | Persisted-format or migration incompatibility strands durable executions | 9 | 3 | 6 | 162 | high (severity floor) | OPEN | `T-084`, `T-086` | 54 |
| `R-007` | Broad exported API freezes low-level contracts or increases accidental breakage | 7 | 5 | 4 | 140 | medium | OPEN | `T-085`, `T-086` | 63 |
| `R-008` | Public API compatibility gate is platform-dependent | 8 | 6 | 3 | 144 | medium | VERIFYING | `T-080` | 32 |
| `R-009` | Unprotected `main` permits unqualified changes to bypass intended gates | 9 | 3 | 3 | 81 | high (severity floor) | OPEN | `T-010` | 18 |

The two most important system-level conclusions are:

1. **Distributed persistence is the largest current architecture risk.** Axiom has strong single-owner/local durable mechanics, but the absence of a first-party networked transactional reference backend leaves too much semantic responsibility to custom Store implementations. This is why `R-002` is the only initial critical RPN and why `T-084` should remain ahead of unrelated feature growth.
2. **The second risk cluster is not missing algorithms but composition/semantic boundaries.** Axiom already contains many powerful mechanisms; the failure mode is that a consumer or future refactor composes them under the wrong durability/time/runtime assumptions. `T-081/T-082/T-083/T-085/T-086` are therefore risk-reduction work, not cosmetic productization.

## 5. Risk-by-risk rationale

### R-001 — external side-effect ambiguity

Axiom correctly claims at-least-once execution and persists work before execution. That removes a large class of lost-work failures, but it cannot make a remote side effect transactional with the local durable commit. The dangerous crash window is:

```text
persist task -> provider accepts effect -> process dies -> local result not committed -> redelivery
```

The runtime is correct to redeliver. The remaining risk belongs at the effect adapter: stable idempotency keys and, for ambiguous providers, reconciliation. `T-083` should prove this end-to-end rather than only describing it.

### R-002 — distributed Store semantic violation

The Store contract contains coupled invariants: CAS, task claim identity, lease expiry, fencing, inbox/event deduplication, history ordering and capability-specific transaction boundaries. A custom SQL/KV implementation can look correct in unit tests while failing under independent processes or weak isolation. The first-party PostgreSQL backend in `T-084` is therefore an architecture reference implementation and a conformance oracle, not merely another storage option.

### R-003 — runtime semantic drift

Core, Flow and ADGO are intentionally different surfaces. Existing work already proved that retry/backoff and lease/fencing are not safely shareable just because the concepts look alike. The residual risk is future semantic drift or misleading documentation. The integration matrix must encode ownership and non-equivalence explicitly.

### R-004 — production miscomposition

The present architecture exposes many valid building blocks. This is powerful for expert users but creates a failure mode where a consumer chooses an embedded/ephemeral path while assuming production durability, or omits an optional capability that a higher-level guarantee implicitly needs. Supported profiles reduce the reachable configuration space without collapsing the runtimes into a mega-abstraction.

### R-005 — time-domain boundary errors

F-026 demonstrated that a single coordinator step could cross a retry deadline between readiness and deadlock classification. That defect is fixed, but it demonstrates a general hazard: clocks are part of the durable protocol. Every new timer/retry/lease/schedule feature must declare which clock governs it and prove equality/boundary behavior deterministically.

### R-006 — persisted-format and migration incompatibility

Fail-closed versioning is the correct default, but it converts a compatibility mistake into an availability event rather than silent corruption. Upgrade qualification must therefore include migration/reopen scenarios using real durable state, especially once a multi-host backend exists.

### R-007 — public API breadth

The current mechanical compatibility gate is a strong control. The remaining risk is strategic: low-level exported symbols can accidentally become de facto stable contracts. `T-085` should tier rather than merely delete APIs so advanced capability remains available without forcing common consumers onto infrastructure-level types.

### R-008 — platform-sensitive compatibility gate

This is primarily a process/release risk rather than a runtime-correctness risk, hence medium FMEA priority despite being a P0 release blocker in the execution plan. Recent changes on `main` target Windows traversal and line-ending normalization, but the risk remains `VERIFYING` until the authoritative cross-platform acceptance evidence is reconciled.

### R-009 — unprotected main

The repository has strong qualification workflows, but disabled branch protection leaves the last enforcement boundary procedural. Severity is deliberately held at 9: a rare bypass can invalidate the assumption that `main` represents qualified behavior. This cannot be completely fixed in source; `T-010` remains an external governance task.

## 6. Review cadence

Review the register at these checkpoints:

- before selecting the next P0/P1 architecture task;
- after any linked `T-XXX` reaches `VERIFYING` or `DONE`;
- after a new high-severity `F-XXX` is opened;
- after any durable schema, Store SPI, lease/fencing, time, external-effect, public API or production-profile change;
- before a release candidate is considered qualified.

The desired trend is not “zero risk IDs”. The desired trend is that high-consequence failure modes stay explicit, have executable controls, and move from high detectability/occurrence scores toward evidence-backed residual risk without weakening the architecture invariants that made Axiom reliable in the first place.
