# Mathematical and cybernetic audit of compiled runtime correctness — 2026-09-08

Status: **engineering evidence input; not a parallel roadmap**  
Audited branch: `main`  
Audited baseline: `1c3271721c897054a111a02458be3506f44ac688`  
Primary scope: CRFG description, compiler, compiled/fast runtime, rule processing, claims, writes/transactions, history replay, selected tests.  
Validation note: this is a **static audit of the central mathematical/runtime core**. Tests were not executed during this audit. Conclusions must not be generalized automatically to all ADGO paths, every Store backend, or every production composition.

## 1. Executive conclusion

Axiom has a strong architectural idea: make domain state, dependencies, invariants and external effects explicit enough that they can become executable contracts rather than implicit application conventions.

The dominant correctness risk in the audited baseline is not lack of features. It is incomplete equivalence between three representations of the same intended semantics:

1. the declarative/model semantics;
2. the compiled/optimized runtime semantics;
3. the history/replay semantics.

The highest-value next objective is therefore to make the following relationship an explicit, testable and eventually provable contract:

```text
Model semantics
      |
      v
Canonical semantic IR
   /           \
  v             v
Reference     Fast/compiled
runtime       runtime
   \             /
    \           /
     v         v
   same observable state
          |
          v
    canonical history
          |
          v
 replay / verification
```

The project should not claim semantic identity between these layers merely because they share source data structures or because individual expression tests pass. Identity must be established over values, missingness, errors, rule scheduling, writes, terminal state and replay-visible history.

The immediate priorities are:

- **P0:** semantic model digest;
- **P0:** fast/reference equivalence;
- **P0:** complete typed dependency graph;
- **P1:** replay completeness and verification mode;
- **P1:** explicit write semantics and write-conflict policy;
- **P1:** precise fixpoint/termination/cancellation semantics;
- **P2:** formally restricted classes with convergence guarantees.

---

## 2. Formal state-transition model

A useful abstraction for the audited runtime is an event-driven transition system:

\[
S=(C,D,F,Q,T,\sigma,H),
\]

where:

- \(C\) — primary context;
- \(D\) — computed/derived values;
- \(F\) — facts and their exposed values;
- \(Q\) — rule work queue;
- \(T\) — activity tasks;
- \(\sigma\) — execution status;
- \(H\) — durable history.

An input event \(e_k\) induces a model-parameterized transition:

\[
S_{k+1}=\mathcal T_M(S_k,e_k).
\]

Inside one logical transition the runtime may:

1. apply a patch/signal/activity result;
2. recompute dependent values;
3. evaluate rule conditions;
4. schedule/evaluate rule work;
5. evaluate writes;
6. validate claims;
7. create or update activity tasks;
8. append history;
9. update execution status.

This representation is useful because it separates state, derived knowledge and external effects. It does **not** remove combinatorial state complexity. With \(n\) independent Boolean variables the reachable state space can still contain up to \(2^n\) valuations. A dependency graph improves locality of evaluation; by itself it does not prove safety over all reachable combinations.

### 2.1 Required semantic contract

Define a reference evaluator \(\mathcal R\), optimized evaluator \(\mathcal F\), and replay projection \(\mathcal P\). For supported models and event sequences, the target contract should be stated explicitly:

\[
\operatorname{Obs}(\mathcal R(M,E))
=
\operatorname{Obs}(\mathcal F(M,E)),
\]

and, for histories produced by a conforming evaluator,

\[
\operatorname{Projection}(\mathcal P(M,H))
=
\operatorname{Projection}(\mathcal R(M,E)).
\]

`Obs` must include not only raw values but also:

- presence vs absence;
- type;
- diagnostic/error class;
- execution status;
- scheduled/terminal tasks;
- rule-visible state;
- history-significant outcomes.

---

## 3. P0 — compiled model hash does not fully identify executable semantics

### Observation

In the audited implementation, `compiledHash()` hashes versions and lists of entity names, but does not include the complete executable meaning of the model. Expressions, defaults, rule bodies, claims and relevant policy parameters are not all represented in the digest input.

Therefore two models can differ semantically while retaining the same `CompiledHash`.

Example:

```text
Order.amount > 100
```

versus:

```text
Order.amount > 100000
```

If the names and currently hashed metadata are unchanged, the digest can remain equal.

This is not a SHA-256 collision. The semantic difference was discarded before hashing.

### Why this is critical

Replay uses the model hash as a compatibility guard. If the digest does not identify executable semantics, replay may accept a history under a meaningfully different model.

Defaults are especially dangerous: replay reconstruction may use defaults from the model supplied at replay time. A changed default under the same incomplete hash can therefore alter reconstructed state without an incompatibility error.

### Required design

Introduce a canonical semantic representation and hash that representation:

\[
h(M)=SHA256(\operatorname{CanonicalIR}(M)).
\]

`CanonicalIR(M)` must include every semantic input that can change observable execution, including at minimum:

- type definitions and relevant type metadata;
- defaults;
- normalized expression ASTs;
- computed definitions;
- fact `when` expressions;
- fact `expose` expressions;
- rule triggers/conditions;
- ordered write bodies when order is semantically significant;
- claims/invariants;
- activity declarations and observable policy semantics;
- retry/backoff/timeout/concurrency/idempotency policy data where runtime semantics depend on them;
- compiler/semantic version identifiers;
- plan/runtime semantic version identifiers.

### Canonicalization rules

Canonicalization must be deterministic and explicitly versioned.

- Sort collections only where order is semantically irrelevant.
- Preserve order where execution depends on order.
- Normalize AST representation rather than hashing source formatting.
- Encode types and scalar values unambiguously.
- Include a `semantic_digest_version` so future canonicalization changes cannot silently reinterpret old histories.

### Acceptance gate

The digest test suite must prove that any semantic mutation changes the digest, while formatting-only or order-insensitive mutations do not.

Required mutation families:

```text
change default
change expression constant
change operator
change fact expose
change trigger
change write expression
swap ordered writes (if sequential semantics remain supported)
change claim
change retry/backoff/timeout/concurrency semantics
change semantic version
```

---

## 4. P0 — fast runtime is not yet equivalent to the general evaluator for computed values

### Observation

In the audited fast-plan compilation path, named `computed` values are registered only when their declared type is Boolean. `NewEngine()` builds the fast plan, and the fast recomputation path then becomes authoritative rather than falling back to the general evaluator.

Consequently, support for integer/string computed expressions in a general evaluator does not imply that the main optimized path computes them correctly.

Example model:

```text
computed total: Int = Order.price * Order.count
```

must have the same semantics in reference and optimized execution. In the audited path that equivalence is not guaranteed by construction.

### False is not missing

A separate Boolean issue exists when synchronization removes a computed value when its bit is false. That collapses two distinct semantic states:

\[
\text{present(false)} \neq \text{missing}.
\]

If the language exposes `exists`/`missing`, then these expressions must be distinguishable:

```text
ready == false
missing(ready)
```

### Required representation

Do not encode semantic presence only through a Boolean bit/value projection. A derived slot needs at least:

```text
DerivedSlot = {
    Present: bool,
    Type:    TypeID,
    Value:   TypedValue
}
```

The optimized Boolean atom representation may remain as an acceleration structure, but it must not be the sole source of semantic truth when the language distinguishes missingness from `false`.

### Central optimization contract

The fast path must satisfy:

\[
\operatorname{EvalFast}(M,C)=\operatorname{EvalReference}(M,C)
\]

for:

- typed values;
- missing/present state;
- errors/diagnostics;
- dependency invalidation;
- rule-visible projections;
- fact exposures.

### Acceptance gate

Create differential tests over generated models and generated patch sequences. A fast result that differs from the reference result must fail CI, even if both paths are individually deterministic.

---

## 5. P0 — dependency graph is not closed over the full expression language

### Observation

The compiler currently performs cycle checks in separate dependency subgraphs such as computed→computed and fact→fact. However, the expression language permits cross-category dependencies.

Separate acyclicity does not prove acyclicity of the union graph.

Counterexample:

\[
computed\ a=\neg F,
\qquad
fact\ F=a.
\]

The combined graph has a cycle although each category viewed in isolation may appear acyclic.

### Expose dependency gap

Fact dependencies must include both:

- the `when` condition that determines fact presence;
- the `expose` expression that determines its exposed value.

If invalidation indexes only `when`, a context field referenced only by `expose` can change while the exposed value remains stale.

### Required graph model

Build one typed dependency graph for all derived semantic nodes:

```text
Context field
Computed
Fact presence
Fact exposed value
Claim expression dependency
Rule condition dependency
Write expression dependency
Activity input / idempotency-key dependency (where applicable)
```

A practical representation is a directed typed multigraph:

\[
G=(V,E,\tau_V,\tau_E),
\]

where node and edge types distinguish semantic roles without fragmenting cycle analysis.

Run strongly connected component (SCC) analysis over the complete derivation subgraph.

### Cycle policy

Every SCC containing a semantic cycle must fall into one explicit class:

1. **Rejected** — arbitrary cyclic dependency is invalid.
2. **Admitted monotone fixpoint** — all transitions are proven monotone over a finite-height lattice.
3. **Admitted iterative semantics** — iteration order, convergence test and failure budget are part of the language contract.

There must be no implicit fourth class in which cycles are accepted merely because two partial graph checks miss them.

### Acceptance gate

Tests must include mixed cycles such as:

```text
computed -> fact presence -> computed
computed -> fact expose -> computed
fact expose -> computed -> fact presence
```

and non-cyclic cross-category graphs that must remain accepted.

---

## 6. P1 — `ExecutionReachedFixpoint` currently proves local quiescence, not a global mathematical fixpoint

### Observation

The runtime records `ExecutionReachedFixpoint` after the internal rule queue drains. At that moment pending activities may still exist and can later change context.

The property actually established is closer to:

\[
Q=\varnothing,
\]

not necessarily:

\[
\Phi(S^*)=S^*.
\]

### Required terminology

Separate at least three guarantees:

| Guarantee | Meaning |
|---|---|
| **Local quiescence** | current internal rule queue is empty |
| **Termination** | internal transition processing cannot continue forever under the stated budget/class |
| **Order independence / confluence** | all allowed execution orders produce the same observable result |

A fourth useful concept is **external quiescence**: no immediately runnable internal work remains and no pending external result is required before a stronger completion state can be claimed.

### Convergence

A step limit prevents unbounded execution; it does not prove mathematical convergence.

For a formally admitted monotone mode, Axiom can use classical finite-lattice conditions:

- state domain is a finite-height partially ordered set;
- transition operator is monotone;
- updates are inflationary (or otherwise have a proved convergence argument).

Then iteration reaches a least fixpoint in finitely many ascending steps.

For arbitrary overwrites, convergence requires a different argument, such as:

- a well-founded decreasing rank;
- a syntactically restricted terminating fragment;
- an explicit operational budget with semantics that say `bounded execution`, not `proved fixpoint`.

### Recommendation

Until stronger conditions are implemented, prefer naming/history semantics that say **quiescent** or **rule queue drained** instead of overloading `fixpoint` with a stronger mathematical claim.

---

## 7. P1 — writes have order-dependent imperative semantics

### Observation

The audited `applyWrites()` behavior evaluates a write and mutates context before evaluating the next write.

With initial state:

\[
x=1,\quad y=2,
\]

and writes:

```text
x := y
y := x
```

the sequential result is:

\[
x=2,\quad y=2,
\]

not a swap.

This is a valid imperative language design, but it must be explicit. In a declarative-looking DSL users may reasonably assume that writes in one rule are simultaneous.

### Recommended declarative semantics

For one write block, evaluate all right-hand sides against the same pre-state:

\[
v_i=\operatorname{Eval}(expr_i,C_{old}),
\]

construct the candidate atomically:

\[
C_{candidate}=C_{old}[targets\leftarrow v],
\]

then:

1. recompute required derived state;
2. validate claims;
3. commit or reject the candidate as one semantic transition.

### Alternative

If sequential writes are intentionally retained, the language specification must state that order is semantic and the model digest must preserve that order.

### Cross-rule conflict policy

Deterministic scheduling order is not the same as order-independent semantics.

The compiler/runtime should detect multiple potentially active writes to the same target and apply an explicit policy, for example:

- reject ambiguous conflicts;
- require declared priority;
- require a commutative merge function;
- allow sequential semantics only in an explicitly imperative block.

### Acceptance gate

Add metamorphic tests where write order is permuted. For declarative/snapshot blocks the result must be invariant. For imperative blocks the compiler/hash/spec must make order dependence explicit.

---

## 8. P1 — replay reconstructs a history projection but is not yet a full semantic replay proof

### Observation

In the audited `ReplayFromHistory()` path:

- `ExecutionCanceled` is not fully handled as a reconstruction event although `Cancel()` records it;
- explicit uniqueness/continuity validation for sequence numbers is incomplete;
- repeated `ExecutionStarted` can recreate/reset state;
- an absent model hash can be substituted from the current model;
- state is reconstructed primarily from recorded writes rather than re-proving that the model should have emitted those writes.

Therefore replay is useful, but two distinct concepts must be separated.

### Mode A — state reconstruction

Apply authoritative recorded events to reconstruct a declared projection of runtime state.

Required properties:

- strict event sequence validation;
- no duplicate sequence identifiers;
- no gaps unless the history format explicitly supports sparse sequence ranges;
- legal lifecycle transitions;
- exact terminal status reconstruction;
- explicit treatment of cancel/failure/supersession/retry events;
- exact model/semantic digest policy;
- explicit rule for incomplete/truncated histories.

### Mode B — semantic verification replay

Re-evaluate model decisions and verify that recorded transitions were permitted by the model.

External activities must not be re-executed. Their recorded outcomes become replay inputs/oracles.

Conceptually:

```text
recorded external outcome
        |
        v
reference evaluator
        |
        v
expected rule/write/task decision
        |
        +---- compare ---- recorded history
```

This mode can detect a history that is structurally valid but semantically inconsistent with the model.

### Acceptance gate

Replay conformance tests must include:

- canceled executions;
- duplicate `Seq`;
- missing `Seq`;
- reordered events;
- duplicate start;
- missing/legacy model digest;
- changed default/expression under same entity names;
- histories ending with pending/running tasks;
- verification replay using recorded activity results.

---

## 9. P1 — claims protect internal candidate state, not the entire cyber-physical process

### Observation

Claims are checked around local writes and can reject/rollback a local candidate. However, an external activity may have already changed a device or external API before its result is interpreted and before a subsequent claim failure rejects local state.

Possible sequence:

```text
1. external device/API performs effect
2. handler returns observation/result
3. result drives local write
4. claim fails
5. local candidate is rejected
6. external world remains changed
```

### Cybernetic model

Separate the controller's internal estimate from the controlled object:

\[
C_t=\text{internal estimated/logical state},
\qquad
X_t=\text{physical or external state}.
\]

A claim

\[
I(C_t)
\]

does not by itself prove

\[
I(X_t).
\]

### Required external-effect contract

For safety-relevant activities, promote the following into a first-class adapter/command contract:

- command precondition/admission predicate;
- observation timestamp and freshness bound;
- command ID / idempotency key;
- expected state/version;
- acknowledgement semantics;
- completion/verification semantics;
- reconciliation/read-back procedure;
- compensation path where compensation is actually safe;
- device-side safety envelope for hazardous physical actions.

For physical systems, safety invariants that must hold despite controller/network/runtime failure belong at the actuator/device/interlock boundary as well as in Axiom claims.

---

## 10. P1 — cancellation and terminal-state semantics need a precise state machine

### Observation

`Cancel()` uses execution locking that can also be held while synchronous dispatch drains work and waits for retries. Cancellation through that API may therefore wait behind ongoing execution work; cancellation through a parent `context.Context` is a different mechanism with different persistence semantics.

### Required lifecycle model

Define the execution lifecycle as an explicit transition system rather than a collection of status assignments.

Example shape:

```text
Created/Started
    |
    v
Running <------> Quiescent
  |  \
  |   \ external/pending work
  |    v
  |   Waiting
  |
  +--> Failed
  +--> Canceled
  +--> Completed   (only if completion semantics are explicitly defined)
```

For every transition specify:

- initiating event;
- lock/transaction boundary;
- whether outstanding tasks remain valid;
- whether running handlers are merely abandoned, cooperatively canceled, or fenced;
- what history must be emitted;
- what replay reconstructs;
- whether the transition is terminal.

### Acceptance gate

Model-check or exhaustively test the finite status transition graph. Illegal terminal-to-running transitions must be rejected mechanically.

---

## 11. Priority and definition of done

| Priority | Work | Definition of done |
|---|---|---|
| **P0** | Semantic digest | changes to defaults, executable expressions, writes, claims and policy semantics are detected; formatting-only changes are stable |
| **P0** | Fast/reference equivalence | values, types, missingness, errors, invalidation and rule-visible results match over generated event sequences |
| **P0** | Complete dependency graph | mixed computed/fact/expose dependencies are represented; SCCs are classified by explicit cycle policy |
| **P1** | Replay completeness | every supported history event and legal lifecycle transition reconstructs correctly; malformed sequences are rejected |
| **P1** | Verification replay | recorded external outcomes can be used to re-check model decisions without re-running external effects |
| **P1** | Write semantics | snapshot vs sequential semantics are explicit; same-target conflicts have a compiler/runtime policy |
| **P1** | Quiescence/fixpoint terminology | history/API distinguish queue-drained state, termination and confluence |
| **P1** | Cancellation/terminal state machine | allowed transitions and outstanding-task behavior are explicit and mechanically tested |
| **P2** | Provable convergence classes | each admitted formal mode states assumptions and the theorem/argument that yields termination/fixpoint guarantees |

---

## 12. Verification strategy: independent executors, not only unit tests

`TestExpressionVMMatchesEvaluator` is a useful local differential test, but expression equivalence is weaker than runtime equivalence. The verification target must cover **sequences of state transitions**.

### 12.1 Reference interpreter

Maintain a deliberately simple, obviously-correct reference implementation with minimal optimization:

```text
ReferenceEngine
  full dependency scan / clear semantics
  explicit typed values
  explicit missingness
  snapshot candidate state
  explicit rule queue
  explicit lifecycle checks
```

Do not share too much optimization machinery with the fast engine; excessive implementation sharing reduces the independence of the oracle.

### 12.2 Generated small models

Generate bounded models containing combinations of:

- Bool/Int/String computed values;
- missing/default values;
- facts with `when` and `expose`;
- mixed cross-category dependencies;
- claims;
- one and multiple rules;
- overlapping writes;
- activities with recorded outputs;
- cancellation and retries.

Small bounded models are especially valuable because they permit exhaustive or near-exhaustive enumeration of input states and short event traces.

### 12.3 Differential trace testing

For each generated model and event sequence:

```text
initial context
  -> patch/signal
  -> recompute
  -> rules
  -> writes
  -> task decisions
  -> recorded external outcome
  -> recompute
  -> ...
```

compare after every semantic boundary:

- context;
- derived values;
- fact presence/exposure;
- queue contents or normalized rule decisions;
- task state;
- execution status;
- error class;
- normalized history projection.

### 12.4 Metamorphic properties

Useful properties include:

- formatting/source rendering does not change semantic digest;
- reordering order-insensitive declarations does not change digest/result;
- changing any executable expression changes digest;
- `present(false)` remains distinguishable from missing;
- unrelated context patches do not change unrelated derived values;
- fast/reference results remain equal after arbitrary short patch sequences;
- snapshot writes are invariant under permutation of write statements;
- replay(reconstruct(history(run))) equals declared runtime projection;
- verification replay accepts genuine histories and rejects semantically tampered histories.

### 12.5 Mutation testing

Deliberately introduce faults such as:

- skip `expose` invalidation;
- drop false computed value;
- omit one AST field from digest;
- ignore one replay event type;
- swap write evaluation order;
- remove one lifecycle guard.

The verification suite should kill these mutants. If it does not, the corresponding semantic property is not yet protected.

---

## 13. Formal-method opportunities

Axiom is unusually well-suited to selective formal verification because much of its intended semantics is already state-machine and graph based.

### 13.1 Model digest theorem

Desired property:

> if two canonical IRs differ in an executable semantic component, their serialized canonical forms differ.

SHA-256 then provides the cryptographic digest assumption on top of a deterministic semantic serialization.

### 13.2 Dependency correctness

For each derived node \(v\), the compiler should compute a dependency set \(Dep(v)\) such that changing any input outside the transitive dependency closure cannot change \(v\), while every referenced input has a path to \(v\).

This can be tested structurally against AST reference extraction.

### 13.3 Monotone fragment

For a restricted monotone fragment over a finite-height lattice \((L,\sqsubseteq)\), if \(\Phi:L\to L\) is monotone and inflationary, repeated application reaches a fixpoint:

\[
x_{n+1}=\Phi(x_n),
\]

with finite convergence bounded by lattice height.

This would justify the word `fixpoint` for that fragment rather than using it only as an operational label.

### 13.4 Lifecycle model checking

The execution/task status machines are finite and can be exhaustively explored. A small TLA+, Alloy, PlusCal, or custom enumerative model can check invariants such as:

- terminal execution never returns to running;
- completed task is never leased again;
- superseded task is never dispatched;
- canceled execution cannot schedule new external effects unless explicitly allowed;
- replay lifecycle accepts exactly the histories generated by legal runtime transitions.

---

## 14. Documentation contract changes implied by this audit

Until the P0/P1 items are closed, documentation should avoid stronger claims than the implementation supports.

### Use precise terms

Prefer:

- `local quiescence` / `rule queue drained` instead of mathematically global `fixpoint` unless the formal preconditions are satisfied;
- `history reconstruction` instead of `semantic verification replay` when recorded decisions are not re-proved;
- `process/store-local idempotency` instead of external exactly-once;
- `internal claim enforcement` instead of cyber-physical safety guarantee.

### Every optimization needs an equivalence obligation

Any new compiled/fast representation should document:

```text
reference semantics
optimized representation
abstraction function
equivalence property
differential tests
known unsupported cases
```

### Every durable format needs a semantic compatibility identifier

History compatibility should be tied to the canonical semantic digest and semantic version, not only entity names or parser/compiler version strings.

---

## 15. Relationship to existing architecture work

This audit complements, rather than replaces:

- `docs/high-leverage-architecture-audit-2026-09-06.md` — broad architecture leverage and fragility;
- `docs/runtime-semantics.md` — current runtime behavior and declared guarantees;
- `docs/axiom-crfg.md` — conceptual CRFG model;
- `docs/architecture-fmea.md` and `docs/architecture-risk-register.json` — risk lifecycle;
- `docs/PRODUCTION_STABILIZATION_PLAN.md` / `MASTER_PLAN.md` — execution planning.

The new evidence should be folded into those planning/risk systems under one principle:

> **No optimized or durable representation is production-qualified until its semantic equivalence to the canonical model is executable as a conformance test.**

---

## 16. Final assessment

Axiom's most promising property is the possibility of turning executable state contracts into something that is not only ergonomic, but **auditable, replayable and mathematically constrained**.

The current audit found gaps exactly at the semantic boundaries that determine whether that promise is trustworthy: model identity, optimized evaluation, dependency closure, write semantics, fixpoint terminology, replay and external-effect reconciliation.

Closing these gaps has more architectural leverage than adding new language features on top of the current core.

The target state is clear:

\[
\boxed{
\text{Language}
\equiv
\text{Canonical IR}
\equiv
\text{Reference runtime}
\equiv
\text{Fast runtime}
\equiv
\text{Verified history projection}
}
\]

where `\equiv` means equivalence of the explicitly documented observable semantics, not implementation identity.
