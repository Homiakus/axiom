// Package adgo implements Adaptive Durable Graph Orchestration (ADGO): a
// deterministic control plane for durable, adaptive workflows.
//
// ADGO models work as an immutable graph rather than a cursor through a list of
// stages. Activities return facts; deterministic decisions, gates and policies
// choose control flow. The package provides static plan validation, durable
// execution snapshots, crash recovery, leases, bounded retry/backoff, parallel
// super-steps, risk-based human approval, compensation, targeted dependency
// repair, capability routing, child executions, content-addressed artifacts,
// plan-delta validation and audit replay.
//
// External side effects are deliberately at-least-once. Activity handlers must
// honor the supplied idempotency key or reconcile ambiguous outcomes. Large
// domain objects should live in an ArtifactStore while execution state keeps
// only compact facts and ArtifactRef values.
package adgo
