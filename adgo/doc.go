// Package adgo implements Axiom Adaptive Durable Graph Orchestration: a
// production durable execution engine for long-running graphs, agents, LLM/tool
// workflows and human-in-the-loop processes.
//
// The package has three execution layers:
//
//   - Runtime: deterministic embedded super-step kernel for single-process use.
//   - Engine: production coordinator/worker protocol with durable task queues,
//     leases, heartbeats, fencing, recovery, adaptive routing and operator APIs.
//   - Host: multi-plan container that serves many immutable plan versions and
//     production child workflows from one durable execution store.
//
// The central invariant is that committed execution state, not goroutine stack
// state, is the source of truth. External work is durably enqueued before a
// worker may run it. Workers claim tasks with expiring leases; heartbeats extend
// those leases; stale workers are fenced and cannot commit late results.
// Therefore external effects are intentionally at-least-once and must use the
// provided idempotency key or explicit reconciliation.
//
// Compile converts a declarative Definition into an immutable digest-pinned
// Plan. The compiler rejects unreachable nodes, unsafe external effects,
// conflicting writers, invalid joins, missing data and unbounded cycles before
// execution begins.
//
// OpenProduction is the batteries-included entrypoint. It wires an Engine with
// durable execution storage, adaptive provider health, global admission control,
// an activity result cache and schedules. Pebble is the default high-throughput
// local backend; FileStore remains useful for shared-filesystem multi-process
// deployments; MemoryStore is intended for tests and ephemeral processes.
//
// Activities report facts, artifacts, quality and resource usage. Deterministic
// gates and policies decide control flow. Probabilistic code does not mutate the
// durable graph directly. Targeted repair invalidates only the affected
// dependency subgraph and is bounded by iteration, cost, duration and quality
// convergence constraints.
//
// Long-running workflows can wait durably for timers, signals, callback tokens
// and human decisions; fork historical snapshots; migrate between compatible
// immutable plans; continue-as-new to bound history growth; run deterministic
// child workflows; and recover compensation after process failure.
//
// Production helpers also provide cross-process admission/rate limiting,
// provider circuit breaking, content-addressed result caching, opt-in hedged or
// ensemble execution for pure activities, graceful worker drain, execution
// diagnostics, fleet audits, retention and version pruning.
//
// See README.md in this directory and ../../ADGO.md for the complete production
// guide and operational invariants.
package adgo
