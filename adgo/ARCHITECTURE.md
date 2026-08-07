# ADGO architecture and algorithm contract

This document maps the ADGO design contract to the implementation in this directory.

## 1. Principles

1. Graph, not cursor.
2. State, not call stack.
3. Events, not hidden mutation.
4. Activities produce facts; policies choose routes.
5. Deterministic control; probabilistic workers.
6. Hard invariants before utility optimization.
7. Typed outcomes and node kinds instead of arbitrary stage strings.
8. Idempotency and reconciliation instead of exactly-once claims.
9. Minimal dependency-directed repair instead of blind restart.
10. Every cycle and repair path is bounded.
11. Detect non-convergence and oscillation.
12. Parallelize from dependencies, subject to resource limits.
13. Use child executions for bounded dynamic fan-out.
14. Keep large data in artifact storage and compact references in execution state.
15. Pin every execution to an immutable plan digest.
16. Human intervention is risk-driven and durable.
17. A probabilistic planner may propose; deterministic validation authorizes.
18. Information-plane input cannot mutate the control plane.
19. Every state can be explained from committed evidence.
20. Recovery is a normal execution path.

## 2. Compiler contract

`Compile(Definition)` validates and canonicalizes the graph, then computes a deterministic
SHA-256 digest. An execution records the plan ID, version and digest and refuses to run under
a different plan.

Static analysis checks:

- IDs, node kinds and transition outcomes;
- dependencies and transition targets;
- data requirements and producers;
- permissions;
- external-effect timeout, idempotency and bounded retry;
- compensation for high-risk reversible effects;
- join semantics and fan-out limits;
- potential conflicting writers;
- reachability and terminal existence;
- strongly-connected components and complete termination bounds;
- deterministic repair roots and complete loop bounds.

## 3. Runtime super-step

For execution `E` under pinned plan `P`:

```text
1  LOAD E + verify P.digest
2  INGEST inbox events and commit their state effects
3  RECOVER expired activity leases
4  APPLY cancellation / budget hard constraints
5  EXECUTE ready deterministic nodes (decision, gate, fork, join, wait, human)
6  DERIVE ready external work from dependencies/data/guards/not-before
7  FILTER permission, approval, budget, throttle and provider constraints
8  SCHEDULE by safe utility under concurrency/resource limits
9  COMMIT task records + leases before invoking handlers
10 RUN selected activities in parallel
11 CLASSIFY each result
12 COMMIT facts/artifact refs/quality/budget/history or retry/repair/wait/failure
13 COMPLETE, WAIT, HUMAN, COMPENSATE or continue the next super-step
```

A worker crash after step 9 is recovered from the expired lease. A crash after an external
side effect but before step 12 may cause redelivery; the committed idempotency key is the
protection boundary.

## 4. Ready-set semantics

A node is ready only when all relevant conditions hold:

```text
activated
AND pending
AND not-before <= now
AND strategy is not banned
AND dependencies satisfy node/join semantics
AND required data/artifacts exist
```

Readiness does not imply scheduling. Scheduler filters and limits are a separate layer.

## 5. Scheduler contract

Hard constraints are evaluated before scores. Candidate selection observes:

- global concurrency;
- per-activity concurrency;
- per-capability concurrency;
- resource keys;
- budget headroom;
- provider availability and permissions;
- risk policy and human approval;
- persistent rate-limit throttle windows.

Only valid candidates are scored using critical-path value, blocked successors, quality
gain, cost, latency and risk.

## 6. Durable commit boundary

Runtime state transitions are written through `Store.Commit(id, expectedVersion, mutate)`.
The `FileStore` implements this with cross-process lock files and immutable atomic snapshots.
A database-backed store can implement the same interface with a transaction/CAS primitive.

The external side effect itself is outside the local transaction. This is why idempotency and
reconciliation are part of the node contract.

## 7. Repair algorithm

When a gate reports violations:

1. collect explicit `Violation.RepairFrom` roots;
2. otherwise map missing data to producing nodes;
3. otherwise use gate-level `RepairFrom` roots;
4. compute descendants of each root that can reach the failed gate;
5. preserve completed nodes outside that set;
6. invalidate only outputs produced/written by affected nodes;
7. enforce each repair root's iteration, cost and duration bounds;
8. compare repeated evaluations of the same gate for convergence;
9. reactivate only the affected subgraph.

This is dependency-directed incremental recomputation, not `goto previous stage`.

## 8. Convergence and oscillation

Convergence uses gate-to-gate quality snapshots. It intentionally ignores intermediate nodes
that do not evaluate the repair objective, preventing false stagnation from no-op validation
steps.

Oscillation uses a bounded history of semantic signatures emitted by activities. Repeated
period-2 or period-3 patterns ban the responsible strategy so the system cannot endlessly
alternate between equivalent states.

## 9. Human and external waits

Timers and events are persisted in execution state. `Run` returns when no immediate progress
is possible; a later invocation resumes from storage.

High-risk external side effects are converted into an approval wait before the activity is
scheduled. Approval is a durable inbox event, so the wait survives process restarts.

## 10. Adaptive planning

Adaptive proposals are data, not executable authority.

`ValidatePlanDelta` enforces:

- allowlisted activities/capabilities;
- allowlisted permissions;
- max risk;
- remaining budget;
- max added nodes;
- side-effect timeout/idempotency requirements;
- valid attachment/rejoin references.

`CompileValidatedPlanDelta` then creates a new immutable child plan carrying the parent and
delta digests in metadata. The running parent graph is never edited in place.

## 11. Child workflows

`RunFanout` derives deterministic child IDs from parent/node/item. Re-running the same fan-out
loads completed children instead of duplicating them. Join policies support all/any/N/quorum
and fan-out/concurrency are explicitly bounded.

## 12. Cancellation and compensation

Cancellation is a persisted request:

```text
CancelRequested
→ stop scheduling new work
→ enter compensating state
→ execute registered compensation handlers LIFO
→ Canceled
```

The same compensation path runs after permanent downstream failure.

## 13. Replay

The deterministic audit boundary is the committed state, not the output of an LLM. FileStore
keeps immutable versions; `VerifyReplay` verifies plan pinning and monotonic state/history
properties across those versions.

## 14. Deployment model

For one host or processes sharing a durable filesystem, `FileStore` is a complete reference
backend. For a cluster, implement `Store` using a transactional shared database and preserve
the same CAS/inbox/lease semantics. Activity handlers remain idempotent in both modes.
