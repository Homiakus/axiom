# Go-first architecture

Axiom exposes three frontends over one execution model:

1. Typed Go reducers for the smallest file-free API.
2. The declarative `model` package for statically analyzable Go definitions.
3. Optional AXM and TOML frontends for serialized and visual models.

All declarative frontends compile to `axiom.Plan`. The runtime consumes the compiled module contained by the Plan and does not depend on the source format.

The typed reducer API is intentionally marked `AnalysisOpaque`, because arbitrary Go handlers cannot be converted into a complete static dependency graph. The `model`, `axm`, and `table` frontends use `AnalysisStatic`.
