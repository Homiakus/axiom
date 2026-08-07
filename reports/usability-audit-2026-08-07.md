# Axiom usability audit — 2026-08-07

## Summary

This report supersedes the July 28 usability audit. It evaluates the repository after the DX/runtime hardening merged through the `v0.1.0` release candidate.

**Current usability/DX assessment: 9.0 / 10.**

The largest problems from the previous audit are no longer current: typed activity registration exists and fails fast, arithmetic is supported end-to-end, the runtime has a preferred `Run` handle, model builder panics were replaced with diagnostics, examples were modernized, and retry/timeout/once policy semantics are now enforced.

## Current strengths

| Area | Status | Notes |
|---|---|---|
| First-run path | Strong | `model -> Plan/Open -> Engine -> Run` is documented as the default Go path. |
| Typed Go DX | Strong | Typed fields, strict literal helpers and `ActTyped` reduce map/string plumbing. |
| Error quality | Strong | `AX507`, `AX509`, `AX510` move common configuration/model failures to initialization or compile time. |
| Runtime ergonomics | Strong | `Run.Dispatch`, `State`, `Status`, `History`, `Explain`, `PendingActivities`, `Cancel`. |
| Arithmetic contract | Strong | Parser, regular evaluator and fast VM share `+ - * / %` and unary minus behavior. |
| Integer correctness | Strong | Exact signed `int64` arithmetic/comparison with overflow detection and explicit unsigned input range. |
| Runtime policy clarity | Good | Retry, timeout and process-local once/parallel behavior are implemented and documented. |
| Query metadata | Strong | Stable `runtime.*` execution projections are available. |
| Documentation | Strong | API guide, runtime semantics, AXM reference, versioning policy, changelog and release notes. |
| CI / consumer safety | Excellent | tests, race, vet, vulnscan, fuzz, external-consumer build, examples and performance checks. |

## Remaining usability gaps

### P1 — Durable retry is still incomplete

Current `retry` is immediate and in-process. The next reliability milestone is task-level retry using `NextAttemptAt`, backoff, durable attempt state and explicit retry history entries. This matters for process crashes and operational recovery.

### P1 — `latest` / `first` concurrency needs task supersession

These modes remain production-blocked with `AX508`. Correct behavior needs atomic replacement/cancellation semantics in the task store, not merely another mutex mode.

### P1 — Unknown `runtime.*` query fields should fail at compile time

The supported projection list is now stable, but an unknown projection still evaluates to `nil`. The compiler should reject unknown names with a diagnostic and list the valid fields.

### P2 — Policy `catch:` dispatch

Catch mappings are parsed and target signals are validated, but the verified runtime does not dispatch them after activity failure.

### P2 — Timer scheduler contract

Timer triggers are represented/indexed but there is no complete wall-clock scheduling API and operational contract.

### P2 — AXM imports

Import syntax exists, but the public compiler does not resolve/link multiple AXM modules.

### P2 — Root package surface before v1

Axiom still exports several low-level aliases. Before `v1.0.0`, classify the root surface into stable application API versus advanced/runtime tooling API and deprecate redundant entry points where useful.

## Release recommendation

The codebase is suitable for a **`v0.1.0` pre-v1 baseline** after the release-candidate CI is green. The remaining issues are important but are explicit limitations rather than hidden contradictions in the primary onboarding path.

Recommended post-`v0.1.0` sequence:

1. durable retry/backoff and retry history;
2. `latest/first` task supersession;
3. strict runtime projection validation;
4. policy catch dispatch;
5. timer scheduler contract;
6. multi-file import resolution;
7. public API surface classification before `v1.0.0`.
