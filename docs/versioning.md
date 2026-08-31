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
2. `go test ./...` passes on the normal supported CI matrix;
3. `go test -race ./...` passes in the normal CI verification DAG;
4. `go vet ./...` and linting pass;
5. `govulncheck`, Gitleaks and gosec pass under the current security workflow or every explicitly accepted finding is documented;
6. parser/TRIZ fuzz smoke tests pass;
7. the external-consumer CI test passes;
8. runnable examples and generated-wrapper verification pass;
9. public API/runtime changes are described in `CHANGELOG.md` and release notes;
10. benchmark regression smoke passes and material benchmark changes are reviewed.

Each version must have non-empty release notes at `docs/releases/<version>.md` **inside the frozen release candidate commit**.

## Publishing through GitHub Actions

`.github/workflows/release.yml` intentionally has one publication entry point: manual `workflow_dispatch` from `main`.

A release is published from a **frozen release branch** so later commits to `main` cannot silently change the reviewed candidate, while release tooling itself always comes from the current protected/hardened `main` workflow definition. Tag-push is deliberately not a publication trigger because GitHub runs the workflow version stored at the event ref; an old frozen tag must not be able to select obsolete release tooling.

To publish a version:

1. merge the release candidate into `main` and confirm normal CI/security are green;
2. create `release/<version>` at the exact reviewed commit, for example `release/v0.1.0`;
3. ensure `docs/releases/<version>.md` is non-empty in that frozen commit;
4. open **Actions -> release -> Run workflow** on `main`;
5. enter the SemVer tag and desired GitHub prerelease flag;
6. the workflow validates the version before using it in refs, fetches the exact remote `main` and `release/<version>` refs, rejects an existing remote tag, and requires the candidate to be an ancestor of remote `main`;
7. it rejects missing/empty candidate release notes and an already existing GitHub Release;
8. the current reusable `ci` and `security` workflows are called with the frozen candidate SHA so release verification cannot silently become weaker than the normal verification DAG;
9. binaries, SBOM and checksums are built from that exact SHA;
10. `gh release create --target <frozen-sha>` creates the release exactly once using the candidate release notes; there is no generated-notes or upload/clobber fallback;
11. after publication the workflow fetches the created tag, resolves it to a commit, and fails unless it equals the frozen candidate SHA.

A failed release is not implicitly repaired by overwriting an existing release or assets. Recovery requires an explicit future recovery procedure/task so the exceptional path is reviewable instead of hidden inside the normal publisher.

The workflow refuses malformed versions, duplicate tags, existing releases, missing/empty release notes, missing frozen branches, candidates that are not ancestors of `main`, and dispatches from a ref other than `main`.

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
- [x] Frozen `release/v0.1.0` branch exists at the reviewed baseline and contains its release notes.
- [ ] `release` workflow is executed for `v0.1.0` after current release-hardening gates are green.

Changes merged after the frozen candidate, including durable retry, runtime-query validation, and task supersession, belong to `Unreleased` and do not retroactively change the `v0.1.0` contract.

## Deprecation policy

When practical, a public symbol should be deprecated for at least one minor pre-v1 release before removal. Deprecated APIs must point to the preferred replacement in Go documentation.

Patch releases must not intentionally remove a public API. Larger pre-v1 cleanup belongs in a minor release with a migration note.

For v1 and later, removals and incompatible signature changes require a new major version.

## Runtime guarantees versus syntax

A field being accepted by a model or parser does not automatically make it a runtime guarantee. `docs/runtime-semantics.md`, the AXM reference, and release notes are the source of truth for enforced behavior such as retries, timeouts, concurrency, durability and cross-process coordination.

## Post-v0.1.0 reliability roadmap

Completed after the frozen v0.1.0 baseline:

- durable task-level retry/backoff with persisted `NextAttemptAt` and retry history;
- stable runtime-query namespace validation on canonical Plan/Open/New paths;
- transactional pending-task supersession for `concurrency: first/latest`.

The authoritative implementation backlog now lives only in [`../MASTER_PLAN.md`](../MASTER_PLAN.md). Historical priority lists in older documents are reference material, not a second execution roadmap.
