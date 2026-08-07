# Versioning and compatibility

Axiom follows Semantic Versioning for published tags and releases.

## Current maturity

Until the first `v1.0.0` release, Axiom should be treated as a pre-v1 library. Minor releases may intentionally evolve the public API, but breaking changes must be documented in the release notes and should include a migration path whenever practical.

Recommended release progression:

- `v0.x.y` — active API refinement and compatibility hardening;
- `v1.0.0` — public API and runtime contract declared stable;
- `v1.x.y` — backward-compatible features and fixes;
- `v2.0.0` — breaking public API or persisted-format changes that cannot be introduced compatibly.

## What counts as public API

Public compatibility includes:

- exported identifiers in `github.com/Homiakus/axiom`;
- exported identifiers in `model`, `axm`, `table`, `diagram` and `store/pebble`;
- behavior documented as a runtime guarantee;
- persisted data formats explicitly documented as stable;
- generated-code contracts documented as stable.

Examples, benchmark output and files explicitly marked experimental are not compatibility promises.

## Release requirements

Before creating a release tag:

1. `go test ./...` passes;
2. critical packages pass under `go test -race`;
3. `go vet ./...` passes;
4. `govulncheck` passes or every accepted finding is documented;
5. the external-consumer CI test passes;
6. runnable documentation examples pass;
7. public API changes are described in release notes;
8. persisted/runtime semantic changes are called out separately;
9. benchmark regressions are reviewed rather than silently accepted.

## Deprecation policy

When practical, a public symbol should be deprecated for at least one minor pre-v1 release before removal. Deprecated APIs must point to the preferred replacement in Go documentation.

For v1 and later, removals and incompatible signature changes require a new major version.

## Runtime guarantees versus syntax

A field being accepted by a model or parser does not automatically make it a runtime guarantee. Release notes and `docs/runtime-semantics.md` are the source of truth for enforced behavior such as retries, timeouts, concurrency, durability and cross-process coordination.

This distinction is especially important before v1 while the runtime contract is still being tightened.
