# Versioning and compatibility

Axiom follows Semantic Versioning for published tags and releases.

## Current maturity

Axiom is a **pre-v1** library. The first versioned public baseline is `v0.1.0`.

Before `v1.0.0`, minor releases may intentionally evolve public API, but breaking changes must be deliberate, documented in `CHANGELOG.md`, and should include a migration path whenever practical.

Recommended progression:

- `v0.1.x` — compatible fixes and hardening of the initial baseline;
- `v0.x.0` — new capabilities and, when explicitly documented, intentional pre-v1 API refinement;
- `v1.0.0` — long-term public API/runtime contract declared stable;
- `v1.x.y` — backward-compatible features and fixes;
- `v2.0.0` — incompatible changes that cannot be introduced safely under v1.

## What counts as public API

Public compatibility includes:

- exported identifiers in `github.com/Homiakus/axiom`;
- exported identifiers in `model`, `axm`, `table`, `diagram` and `store/pebble`;
- behavior documented as a runtime guarantee;
- diagnostic codes that documentation encourages callers to act on;
- persisted data formats explicitly documented as stable;
- generated-code contracts explicitly documented as stable.

The following are not compatibility promises:

- `internal/*` implementation details;
- parsed-but-not-implemented forward-compatible syntax;
- undocumented runtime internals;
- benchmark values as SLA;
- behavior explicitly listed as a limitation.

## Release requirements

Before creating a release tag:

1. `go mod tidy` leaves `go.mod`/`go.sum` unchanged;
2. `go test ./...` passes;
3. critical packages pass under `go test -race`;
4. `go vet ./...` passes;
5. `govulncheck` passes or every accepted finding is documented;
6. parser/TRIZ fuzz smoke tests pass;
7. the external-consumer CI test passes;
8. runnable examples and documented generator commands pass;
9. public API/runtime changes are described in `CHANGELOG.md` and release notes;
10. benchmark regressions are reviewed rather than silently accepted.

Each version must have release notes at `docs/releases/<version>.md`.

## Publishing through GitHub Actions

The repository provides `.github/workflows/release.yml` with a manual `workflow_dispatch` entry point.

A release is published from a **frozen release branch** so later commits to `main` cannot silently change the contents of an already-reviewed release candidate.

To publish a version:

1. merge the release candidate into `main` and confirm its normal CI is green;
2. create `release/<version>` at the exact reviewed commit, for example `release/v0.1.0`;
3. keep `docs/releases/<version>.md` on `main`;
4. open **Actions -> release -> Run workflow** on `main`;
5. enter a pre-v1 SemVer tag such as `v0.1.0` and choose prerelease status;
6. the workflow resolves `release/<version>`, verifies that commit is an ancestor of `main`, then checks out the frozen SHA;
7. it re-runs module consistency, `go test ./...`, and `go vet ./...` on that frozen candidate;
8. on success, `gh release create` creates the tag at the frozen SHA and publishes the GitHub Release using `docs/releases/<version>.md` from release tooling on `main`.

The workflow refuses malformed versions, duplicate tags, missing release notes, missing frozen branches, candidates that are not ancestors of `main`, and dispatches from a branch other than `main`.

## v0.1.0 release checklist

- [x] Canonical onboarding path documented.
- [x] Typed activity boundary fails fast on invalid shapes/handlers.
- [x] Common model-builder mistakes return diagnostics rather than panic.
- [x] Invalid model literals retain their original encoding error.
- [x] Retry/timeout/once policy semantics for the frozen baseline are documented.
- [x] AXM arithmetic is consistent across parser, regular evaluator and fast VM.
- [x] AXM `Int` has an explicit signed 64-bit range contract.
- [x] Stable `runtime.*` execution metadata projections are implemented.
- [x] Fuzz-discovered parser panic has regression coverage.
- [x] Current usability audit exists and the July report is marked superseded.
- [x] `CHANGELOG.md` and `docs/releases/v0.1.0.md` are prepared.
- [x] Release-readiness PR is merged into `main` with green CI.
- [x] Release tooling supports a frozen `release/v0.1.0` candidate.
- [ ] `release` workflow is executed for `v0.1.0`.

Changes merged after the frozen candidate, including durable task-level retry, belong to `Unreleased` and do not retroactively change the `v0.1.0` contract.

## Deprecation policy

When practical, a public symbol should be deprecated for at least one minor pre-v1 release before removal. Deprecated APIs must point to the preferred replacement in Go documentation.

Patch releases must not intentionally remove a public API. Larger pre-v1 cleanup belongs in a minor release with a migration note.

For v1 and later, removals and incompatible signature changes require a new major version.

## Runtime guarantees versus syntax

A field being accepted by a model or parser does not automatically make it a runtime guarantee. `docs/runtime-semantics.md`, the AXM reference, and release notes are the source of truth for enforced behavior such as retries, timeouts, concurrency, durability and cross-process coordination.

## Post-v0.1.0 reliability roadmap

1. atomic task supersession for `concurrency: latest/first`;
2. compile-time validation of `runtime.*` projection names;
3. runtime policy `catch:` dispatch;
4. wall-clock timer scheduler contract;
5. AXM multi-file import resolver/linker;
6. distributed execution ownership/coordination contract;
7. root public API classification and cleanup before `v1.0.0`.
