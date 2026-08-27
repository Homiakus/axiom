# Axiom Plan-Driven Quality Loop

Status: **active control plane**  
Canonical implementation roadmap: [`PRODUCTION_STABILIZATION_PLAN.md`](PRODUCTION_STABILIZATION_PLAN.md)  
Edge-space contract: [`../quality/edge-space.json`](../quality/edge-space.json)  
Mutation engine: Gremlins **v0.6.0**, pinned in CI.

This document defines the cyclic test/repair contour used while executing Axiom's canonical roadmap. It is intentionally not a second roadmap. `docs/PRODUCTION_STABILIZATION_PLAN.md` remains the single source of implementation order; the quality loop reads that file, selects the first actionable `P0 BLOCKER`, `TODO`, or `PARTIAL` task (skipping external-only tasks), and validates every implementation cycle against the same invariant stack.

## 1. Control loop

Each implementation cycle is:

`select -> reproduce -> model boundaries -> implement -> narrow test -> sentinels -> race/shuffle -> mutation -> review delta -> commit -> observe CI -> update roadmap`

The loop is fail-closed. A failed gate stops progression to the next roadmap item. A later green gate never overrides an earlier red gate.

### Step A — select exactly one roadmap task

```bash
go run ./cmd/qualityloop -mode next
```

Selection order is the order in the canonical plan. An agent must not silently skip an earlier actionable item because a later task is easier. `EXTERNAL` items are reported in the plan but are not selected as source-code work.

### Step B — create a minimal reproducer before the repair

For a defect or fragile invariant, first add the smallest deterministic test that fails on the current implementation. Prefer an exact boundary, fake clock, failpoint, temporary store, subprocess boundary, or explicit competing writer over sleeps and timing luck.

A repair without a reproducer is allowed only for mechanical changes where behavior cannot change (for example formatting), or when the task is documentation-only.

### Step C — project the change into the edge space

Every changed invariant is considered across these independent dimensions:

1. identity / namespace;
2. numeric domain;
3. semantic time;
4. concurrency / ownership;
5. persistence backend and reopen state;
6. failure/crash boundary;
7. ordering / tie-breaking;
8. lifecycle (retry, replay, restart, compensation, duplicate delivery).

The exact values and critical subsets are machine-readable in `quality/edge-space.json`.

A full Cartesian product is intentionally not run for every commit. With eight dimensions it quickly becomes wasteful and obscures the dangerous combinations. Instead:

- **PR/push:** interaction strength 2 plus exhaustive high-risk sentinels;
- **nightly:** interaction strength 3 plus repeated shuffle/race campaigns;
- **weekly/manual:** mutation testing of the full mutable core;
- **always exhaustive:** explicitly listed sentinel intersections, because those represent known Axiom failure surfaces rather than statistically sampled combinations.

The machine contract rejects an edge model if a critical dimension is not connected to an executable sentinel.

### Step D — implement the minimum invariant-preserving patch

The implementation should change the smallest surface that closes the reproduced defect and all directly implied boundary cases. Do not combine unrelated refactors with a correctness repair. Persisted formats, public APIs, clocks, stores, ownership/fencing, and Flow effects retain the stricter rules from the canonical production plan.

### Step E — narrow-to-broad validation ladder

1. the new reproducer;
2. package tests for the modified package;
3. related sentinel campaign;
4. shuffled/repeated critical packages;
5. race detector for runtime/store/concurrency changes;
6. repository CI;
7. mutation testing for changed Go code;
8. nightly/weekly campaigns for broader interaction coverage.

Local entry points:

```bash
bash scripts/quality_loop.sh plan
bash scripts/quality_loop.sh sentinels
bash scripts/quality_loop.sh fast
bash scripts/quality_loop.sh deep
bash scripts/quality_loop.sh mutation-diff
```

## 2. Multidimensional boundary model

The edge model is not a flat list of "special values". A failure is treated as a point in a multidimensional space:

`E = Identity × Numeric × Time × Concurrency × Persistence × Failure × Ordering × Lifecycle`

The important property is interaction. Examples:

- `encoded-slash × Pebble reopen × prefix-related ordering` checks durable namespace separation;
- `NaN × commit × replay` checks that an invalid numeric state is rejected before publication and stays rejected across backends;
- `exact-deadline × competing writers × retry` checks deterministic ownership at the temporal boundary;
- `after-state-before-effect × Pebble × restart` checks outbox/idempotency crash semantics;
- `owner replacement × reverse order × dot/dotdot identity` checks that ownership and path identity never alias.

When a new bug class is discovered, add either a new value to an existing dimension or a new dimension if the variable is genuinely independent. Then add a sentinel if the value is critical. This makes the test model grow from evidence rather than from an unstructured pile of regressions.

## 3. Test-of-tests: mutation testing

Coverage answers "was this line executed?" Mutation testing asks "would the tests notice if the behavior were wrong?"

Axiom uses the pinned Gremlins release in two modes:

- **diff mutation gate:** changed Go code on pull requests; minimum test efficacy **80%**, minimum mutant coverage **70%**;
- **full mutation baseline:** weekly/manual mutable-core campaign; bootstrap floors **60% efficacy / 50% mutant coverage**.

The full baseline is intentionally lower at introduction because an unmeasured legacy module must first expose survivors rather than have the gate disabled. The floor is a ratchet: after a stable baseline is observed, raise it; never lower it to make a change pass without an explicit roadmap item explaining the surviving mutants.

For a `LIVED` mutant, the default action is to strengthen the test, not to rewrite production code. A surviving mutant often means the asserted invariant is too weak. `NOT COVERED` means the corresponding reachable behavior lacks test execution. `NOT VIABLE` is excluded from the efficacy calculation but should still be inspected if unexpectedly dominant.

## 4. Controlled auto-repair policy

There are two repair classes.

### Class M — mechanical auto-fix

May run automatically because it is semantics-preserving and deterministic. The initial allowlist contains only `gofmt` on changed Go files:

```bash
bash scripts/quality_loop.sh autofix
```

The command then reruns tests. It does not commit, push, modify tests, change dependencies, regenerate durable fixtures, suppress diagnostics, add skips, or increase timeouts.

### Class L — logical repair by an agent

An agent may modify logic only under this transaction:

1. capture the exact failing command and smallest reproducer;
2. state the violated invariant and affected edge-space dimensions;
3. restrict the patch to the selected roadmap task and its required tests/docs;
4. run the reproducer until green;
5. run the relevant sentinel(s);
6. run package/race/shuffle gates required by the touched surface;
7. run diff mutation when Go behavior changed;
8. reject the repair if it reduces test strength, weakens an invariant, introduces an unconditional skip, hides an error, or lowers a mutation threshold;
9. allow at most **3 logical repair attempts** for the same failure signature before stopping automatic iteration and recording a blocker/root-cause note;
10. only then commit and push the atomic task.

Tests may be changed when the test is demonstrably inconsistent with the documented contract, but the same commit must include the contract evidence and replacement test. Deleting a failing assertion is not a repair.

## 5. Forbidden self-healing actions

The contour must never autonomously:

- remove, skip, quarantine, or weaken a failing test to obtain green CI;
- increase sleeps/timeouts as a substitute for deterministic synchronization;
- catch and discard an error that was previously observable;
- change a public or persisted contract without the roadmap's compatibility procedure;
- lower mutation, race, security, benchmark, or compatibility gates;
- use `--force` to overwrite `main` history;
- retry nondeterministic failures until one run happens to pass and call that a fix;
- bundle several roadmap tasks into one repair transaction.

## 6. CI topology

`.github/workflows/quality-loop.yml` complements, rather than replaces, the existing CI and nightly workflows.

| Layer | Trigger | Purpose |
|---|---|---|
| Plan & edge contract | push / PR / schedule / manual | canonical task selection and machine validation of the boundary model |
| Boundary shuffle & sentinels | push / PR | detect order dependence and known high-risk interactions quickly |
| Deep boundary campaign | daily / manual | repeated full shuffle + race over critical surfaces |
| Diff mutation | PR / manual | test-of-tests for changed Go behavior |
| Full mutation | weekly / manual | discover weak assertions and uncovered mutable behavior across the module |

The existing `ci.yml` remains authoritative for lint, module hygiene, cross-platform unit tests, full race, examples, codegen, consumer isolation, and benchmark smoke. The existing `nightly.yml` remains authoritative for long fuzz campaigns. The quality loop adds orchestration, interaction testing, and mutation analysis rather than duplicating those responsibilities.

## 7. Evidence recorded per completed roadmap task

A task is only moved to `DONE` when its plan entry or progress log records enough evidence to reconstruct the decision:

- reproducer/test name;
- dimensions/sentinel IDs exercised;
- narrow test command;
- race/shuffle result when required;
- mutation result for changed Go behavior;
- compatibility/performance evidence when applicable;
- implementing commit SHA;
- CI result for that SHA.

This makes the roadmap self-auditing: the next cycle can distinguish "implemented" from "believed implemented" without repeating the whole investigation.
