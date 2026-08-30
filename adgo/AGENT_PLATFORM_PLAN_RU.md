# ADGO AGENT PLATFORM — upstream-план reusable 24/7 durable agent kernel

> Репозиторий: `Homiakus/axiom`  
> Подсистема: `axiom/adgo`  
> Статус: **authoritative upstream architecture/implementation roadmap**  
> Главный downstream consumer: `Homiakus/Antigravity-Progressive-Bootstrap` (`agctl`)  
> Стратегическая цель: развивать durable orchestration и generic long-running agent capabilities **один раз в ADGO**, а приложения оставлять тонкими domain/product слоями.

---

## 0. Стратегическое решение

ADGO должен стать reusable kernel для долгоживущих вычислительных и агентных workflow. `agctl` не должен параллельно развивать второй универсальный Harness.

Целевая иерархия:

```text
Axiom
  model / claims / rules / compiler / table
        |
        v
ADGO
  durable execution / DAG / workers / leases / retry / effects
  repair / budgets / artifacts / history / migration / storage
        |
        v
ADGO Agent
  agent runs / generations / checkpoints / provider capacity
  quota / demand / reservations / sessions / context / runtime ABI
        |
        +-----------------------+-----------------------+
        |                       |                       |
        v                       v                       v
      agctl                 BusinessOS              other apps
  coding product layer      domain layer            domain layer
```

Правило ownership:

```text
generic orchestration/agent mechanism -> Axiom/ADGO
application/domain mechanism          -> downstream repository
```

---

# 1. Что ADGO уже умеет и должно остаться foundation

Не переписывать без доказанной необходимости:

- immutable Plan + deterministic digest;
- durable Execution/Node/Task model;
- coordinator/worker split;
- TaskPending -> TaskRunning -> fenced commit protocol;
- leases, heartbeat и zombie-worker protection;
- crash recovery;
- bounded retry/failure taxonomy;
- generic budgets/admission/resource controls;
- durable inbox/signals/waits/human decisions;
- external effect idempotency boundary;
- ambiguous-side-effect reconciliation semantics;
- targeted dependency repair;
- revision counters;
- stagnation/oscillation detection;
- adaptive provider routing foundation;
- provider health/circuit concepts;
- result cache/single-flight;
- pure hedged/ensemble execution;
- immutable versions/time travel/forks;
- explicit plan migration;
- child workflows/Host;
- diagnostics/watch/audit;
- Memory/File/Pebble stores and optional capability-interface philosophy.

Новые функции должны усиливать эти contracts, а не создавать параллельный runtime внутри `adgo/agent`.

---

# 2. Новая подсистема `adgo/agent`

`adgo/agent` — не новый workflow engine. Это reusable domain extension для LLM/agent workloads поверх ADGO tasks.

Целевые package boundaries:

```text
adgo/
├── ... existing durable core ...
│
└── agent/
    ├── runtime.go        portable runtime ABI
    ├── run.go            logical AgentRun
    ├── generation.go     context-generation lifecycle
    ├── checkpoint.go     durable handoff contract
    ├── provider.go       provider/account/model identities
    ├── capacity.go       observations + native metrics
    ├── quota.go          quota windows/headroom semantics
    ├── demand.go         conservative demand estimation
    ├── reservation.go    atomic claim/settlement contracts
    ├── usage.go          classified usage samples
    ├── session.go        reusable session observations
    ├── context.go        context pressure/headroom
    ├── broker.go         REUSE/NEW/CHECKPOINT_AND_NEW
    ├── router.go         hard filters + explainable utility
    ├── failure.go        agent/provider-specific failure info
    └── telemetry.go      agent-level explainability
```

Физические Codex/Claude/Antigravity implementations **не входят** в Axiom. Они реализуют public `agent.AgentRuntime` downstream.

---

# 3. Agent domain model

## 3.1 AgentRun

`AgentRun` — логическая работа агента, которая переживает смену provider session/context/process.

```text
ADGO Task
   |
   v
AgentRun
   |
   +--> Generation 1 -> checkpoint
   +--> Generation 2 -> checkpoint
   +--> Generation 3 -> terminal result
```

Инвариант:

```text
provider conversation != AgentRun
```

Provider thread/session является replaceable physical resource.

## 3.2 Generation

Предлагаемый lifecycle:

```text
NEW
 -> STARTING
 -> RUNNING
 -> CHECKPOINTING
 -> ROTATED

RUNNING -> SUCCEEDED
RUNNING -> FAILED
RUNNING -> CANCELED
CHECKPOINTING -> FAILED
```

Generation identity включает:

- AgentRun ID;
- generation number/epoch;
- ADGO task/attempt identity;
- selected provider/account/model;
- optional provider session ID;
- workspace fingerprint supplied downstream;
- checkpoint input/output refs;
- lease/fencing identity;
- start/end timestamps;
- usage and failure classification.

Stale generation не может commit после rotation/failover.

## 3.3 Checkpoint

Checkpoint должен быть small-control-state + artifact references, а не transcript dump.

Обязательные классы:

- objective/state summary;
- completed subgoals;
- unresolved blockers;
- important decisions;
- rejected approaches;
- external effect status;
- artifact refs;
- next recommended action;
- provider/session metadata только как non-authoritative hint;
- schema version + digest.

Большие данные живут в Artifact store/CAS downstream или через ADGO artifact capability.

---

# 4. Provider/account/model abstraction

Нужны устойчивые идентификаторы и явное разделение:

```text
Provider
  -> Account
      -> Model
          -> Session
```

Запрещено выводить account/model/session affinity из opaque ID, UI label или transcript heuristics.

Минимальные contracts:

```go
type ProviderID string
type AccountID string
type ModelID string
type SessionID string
```

Provider observation может быть partial/unknown. UNKNOWN не превращается в optimistic availability.

---

# 5. Native-unit capacity/quota model

Generic ADGO Admission недостаточен для сложных AI provider quotas. `adgo/agent` должен поддержать native metrics:

```text
TOKENS
REQUESTS
COST
FRACTION
OPAQUE
```

Для каждого окна сохраняются:

- provider/account/model scope;
- metric kind;
- unit;
- limit, если authoritative;
- consumed/remaining, если authoritative;
- reset time/window;
- observation timestamp/freshness;
- source/confidence;
- opaque provider bucket identity where applicable.

Инварианты:

1. несовместимые units не суммируются;
2. отсутствие limit не означает infinity;
3. zero effective headroom не автоматически означает authoritative provider exhaustion;
4. stale observation не используется после известного reset boundary;
5. OPAQUE metric не конвертируется в token/request equivalents без provider contract;
6. FRACTION валидируется в явных bounds.

---

# 6. Demand estimation

Demand estimation должен быть conservative и scoped.

Предлагаемая модель:

```text
TaskClass
RepositoryClass
ContextClass
Provider
Model
Metric
        -> empirical samples
        -> conservative percentile (default p80)
```

Fallback разрешён только по явно совместимой hierarchy. Нельзя угадывать classification из opaque IDs или transcript names.

Cold start:

- configuration/provided estimate;
- provider/model-specific default только если документирован;
- иначе UNKNOWN/needs explicit budget.

Каждый estimate хранит rationale/source/sample count/confidence.

---

# 7. Atomic reservation + settlement

До запуска дорогостоящей agent generation:

```text
observe capacity
   -> estimate demand
   -> check complete active claims
   -> atomically reserve assignment + quota claims
   -> dispatch
   -> record actual usage
   -> settle/release reservation
```

Reservation должен поддерживать несколько одновременно применимых quota windows без partial commit.

Требования:

- exact replay idempotent;
- expired claims excluded;
- complete active population considered;
- one transaction/CAS authority;
- rollback on any incompatible/over-capacity window;
- actual usage stored separately from reserved estimate;
- provider ambiguous billing/usage status remains explicit.

Эта модель должна дополнять, а не заменять ADGO generic AdmissionController.

---

# 8. Session/context broker

Canonical decisions:

```text
REUSE
NEW
CHECKPOINT_AND_NEW
UNAVAILABLE
```

Broker учитывает:

- account state;
- model availability/capability;
- provider health;
- authoritative session affinity;
- workspace fingerprint if required;
- absolute remaining context;
- context fraction/headroom;
- acquire/retain hysteresis;
- draining semantics;
- task requirements.

Rules:

- unknown context != unlimited;
- draining account may reuse existing safe session but не обязан иметь право acquire replacement;
- disabled/exhausted/unavailable fail closed;
- session affinity never inferred;
- candidate ordering deterministic;
- threshold policy supplied caller/config, не hard-coded universal constants.

---

# 9. Portable AgentRuntime ABI

ADGO defines lifecycle, downstream provides implementation.

```go
type AgentRuntime interface {
    Capabilities(context.Context) (RuntimeCapabilities, error)
    Start(context.Context, AgentStartRequest) (RuntimeHandle, error)
    Resume(context.Context, AgentResumeRequest) (RuntimeHandle, error)
    Observe(context.Context, RuntimeID) (RuntimeObservation, error)
    Checkpoint(context.Context, RuntimeID) (RuntimeCheckpoint, error)
    Cancel(context.Context, RuntimeID, CancelMode) error
}
```

`RuntimeObservation` должен позволять отличить:

- starting/running/waiting/finished;
- provider/session loss;
- context pressure;
- retryable transport failure;
- rate limit;
- terminal error;
- ambiguous side effect;
- human/approval wait where applicable.

Никакой runtime adapter не имеет права напрямую мутировать ADGO execution state; он возвращает observations/results, а deterministic coordinator делает transition.

---

# 10. Storage evolution

Текущие Memory/File/Pebble полезны, но downstream `agctl` имеет зрелый SQLite durable store и operational patterns, которые надо поднять upstream.

## 10.1 Не расширять base Store до мегаиинтерфейса

Сохранить capability model:

```text
Store
VersionedStore
ExecutionCatalog
ExecutionDeletionStore
VersionPruner
ProviderHealthStore
...
```

Добавить при необходимости:

```text
LeaseQueryStore
ArtifactMetadataStore
AgentProviderStore
AgentReservationStore
AgentUsageStore
AgentSessionStore
```

Только если capability действительно требует backend-level atomicity/queryability.

## 10.2 SQLite backend

Цель: `adgo/store/sqlite` или эквивалентный public package.

Требования:

- WAL where safe;
- migrations versioned/checksummed/immutable;
- transactional CAS;
- crash/reopen tests;
- foreign-key/check constraints where useful;
- bounded busy/retry policy;
- consistent snapshot reads;
- benchmark gates;
- Windows/Linux support;
- no dependency on agctl domain types.

Не копировать schema `agctl` буквально: сначала вывести generic contracts.

---

# 11. Continue-as-new

Долгоживущий logical process не должен бесконечно раздувать одну execution history.

ADGO должен получить explicit protocol:

```text
Execution A
  -> quiescent point
  -> select carry-forward facts/artifacts/budget lineage
  -> create Execution B
  -> durable link A -> B
  -> close A as continued
```

Требования:

- no active tasks;
- deterministic successor ID/idempotency;
- selected state only;
- cumulative mission accounting optional but explicit;
- old execution immutable audit trail;
- callbacks tied to old revision become stale;
- compensation/reconciliation obligations cannot disappear silently.

AgentRun может использовать continue-as-new для 24h/72h mission segmentation.

---

# 12. Plan migration

ADGO already has explicit migration semantics; их надо довести до agent/platform requirements.

Migration allowed only at quiescent points with compatibility analysis.

Нужно валидировать:

- plan ID/version/digest source;
- active task absence;
- node mapping;
- completed semantic compatibility;
- agent generation state;
- pending waits/callbacks;
- reserved quota claims;
- artifact/fact ownership;
- changed risk/permissions;
- changed external-effect semantics.

LLM proposal не может сам repin live execution.

---

# 13. Repair/convergence

Уже существующие ADGO mechanisms становятся canonical для downstream applications:

```text
failed gate
 -> violations
 -> deterministic repair roots
 -> minimal affected subgraph
 -> preserve unaffected completed nodes
 -> invalidate affected outputs
 -> revision epoch
 -> bounded rerun
```

Расширить tests и public explanation, а не создавать второй repair planner в `agctl`.

Обязательные loop bounds:

- max iterations;
- max cost;
- max duration;
- epsilon improvement;
- strategy signature repetition;
- oscillation/stagnation detection.

---

# 14. Artifacts and provenance

ADGO core хранит small metadata/reference semantics. Large payload storage остаётся replaceable.

Нужно определить generic:

```go
type ArtifactRef struct {
    Digest      string
    MediaType   string
    Size        int64
    Namespace   string
    Provenance  ProvenanceRef
}
```

Agent checkpoints/results/test reports в downstream системах используют refs.

Не превращать execution state в blob store.

---

# 15. Security boundary

ADGO/Agent гарантирует control-plane safety, но не является secret manager.

Rules:

- raw secrets never persisted in execution/checkpoint;
- runtime gets secret references/environment downstream;
- provider/account identity != credential material;
- permissions are hard filters before adaptive scoring;
- unknown provider/session affinity fails closed;
- operator patches cannot mutate reserved/internal fields;
- stale generation/worker fencing enforced;
- external side effect needs idempotency/reconciliation contract;
- agent output never directly mutates live control state.

---

# 16. Public API/versioning strategy

Axiom pre-v1, поэтому migration consumer требует disciplined releases.

До первого широкого `agctl` adoption:

1. определить public `adgo/agent` API surface;
2. добавить external consumer compile test;
3. зафиксировать persisted format policy;
4. documented deprecation/migration rules;
5. выпускать tagged pre-v1 versions/frozen release branches;
6. downstream pin только на reviewed tag/commit;
7. compatibility matrix в release notes.

`agctl` не должен зависеть от floating `main` в production.

---

# 17. Go toolchain policy

Сейчас Axiom использует Go 1.26, `agctl` исторически — 1.24.2.

Цель: единый поддерживаемый baseline, предпочтительно Go 1.26, если downstream platform matrix проходит.

Перед объявлением requirement permanent:

- Linux CI;
- Windows CI;
- race support;
- external consumer build;
- static analysis;
- deployment toolchain availability.

---

# 18. Upstream extraction matrix из `agctl`

| `agctl` capability | ADGO target | Action |
|---|---|---|
| provider domain primitives | `adgo/agent/provider` | generalize upstream |
| provider observations | `adgo/agent/capacity` | generalize |
| normalization/headroom | `adgo/agent/quota` | port tests/invariants |
| conservative p80 demand | `adgo/agent/demand` | port |
| atomic reservations | `adgo/agent/reservation` | port transaction semantics |
| usage classification | `adgo/agent/usage` | port without coding-specific heuristics |
| session broker | `adgo/agent/session` | port |
| agent in-memory checkpoints | `adgo/agent/generation/checkpoint` | redesign durable |
| worker/session recovery ideas | ADGO core + agent | merge |
| generic SQLite patterns | `adgo/store/sqlite` | extract generic backend |
| worktree manager | none | stays agctl |
| Codex/Antigravity adapters | none | stays agctl |
| MASTER_PLAN semantics | none | stays agctl |
| publication/main policy | none | stays agctl |

---

# 19. Atomic upstream tasks

## U-01 — Freeze ADGO core/agent ownership model
**P0.** Document core vs `adgo/agent` vs downstream boundary. Add architecture tests/lints where practical.

## U-02 — Capability parity audit against agctl Harness
**P0.** Compare semantics/tests/benchmarks for engine, scheduler, leases, retries, effects, artifacts, store, provider subsystem.

## U-03 — Storage capability gaps and SQLite design
**P0.** Define minimal optional interfaces and migration approach before code.

## U-04 — Provider/account/model/session primitives
**P0.** Stable IDs, validation, no-inference rules.

## U-05 — Capacity/quota observations
**P0.** Native metrics, freshness/reset/source semantics.

## U-06 — Demand estimator
**P0.** Scoped empirical percentile + cold-start contracts.

## U-07 — Atomic reservations
**P0.** Multi-window complete-claim accounting, replay, rollback.

## U-08 — Usage settlement/classification
**P0.** Actual usage and reservation reconciliation.

## U-09 — Session/context broker
**P0.** REUSE/NEW/CHECKPOINT_AND_NEW/UNAVAILABLE with deterministic hysteresis.

## U-10 — AgentRun/Generation model
**P0.** Durable logical run, generation epoch and stale-generation fencing.

## U-11 — Durable checkpoint contract
**P0.** Versioned summary + artifact refs + validation/digest.

## U-12 — AgentRuntime ABI
**P0.** Start/resume/observe/checkpoint/cancel; no provider-specific APIs.

## U-13 — SQLite backend
**P0.** Generic durable backend, migrations, crash tests, benchmarks.

## U-14 — Continue-as-new
**P1.** Quiescent rollover with lineage and obligation preservation.

## U-15 — Agent-aware plan migration
**P1.** Generations/reservations/waits/callback compatibility.

## U-16 — Differential/model-based test kit
**P0.** Reusable deterministic traces for comparing legacy/consumer state machines where needed.

## U-17 — Mutation/property/fuzz expansion
**P0.** Kill safety guards: fencing, reservation bounds, unknown context, stale plan, repair convergence.

## U-18 — Fault-injection suite
**P0.** Coordinator death, worker death, ambiguous effect, store reopen, stale callbacks, quota reset, generation loss.

## U-19 — 24h/72h endurance fixtures
**P1.** Reusable soak harness for downstream consumers; no network secrets required in core CI.

## U-20 — Release/consumer gate
**P0.** Tagged release/frozen candidate + `agctl` external consumer compilation/integration before default migration.

---

# 20. Dependency DAG

```text
U-01 -> U-02 -> U-03
        |
        +-> U-04 -> U-05 -> U-06 -> U-07 -> U-08
                              |
                              +-> U-09 -> U-10 -> U-11 -> U-12

U-03 -> U-13
U-10,U-13 -> U-14
U-09..U-14 -> U-15
U-04..U-15 -> U-16 -> U-17 -> U-18 -> U-19
U-13,U-18 -> U-20
```

Downstream `agctl` migration should not implement blocked generic tasks locally merely to bypass this DAG.

---

# 21. Verification policy

Каждая P0 capability требует:

- table/characterization tests;
- exact boundary tests;
- deterministic ordering tests;
- concurrent race tests;
- property/fuzz where state-space large;
- mutation sentinel for critical guard;
- crash/reopen test for durable state;
- benchmark + allocation evidence;
- Windows/Linux;
- external-consumer compilation for public API;
- documentation of ambiguity/unknown semantics.

Provider subsystem additionally требует multidimensional matrix:

```text
provider
x account
x model
x metric/window
x freshness/reset
x active reservations
x task class
x session context
x draining/disabled state
x failure timing
x replay
```

---

# 22. Fault/endurance qualification

Reusable fault scenarios:

```text
kill coordinator before schedule commit
kill worker after claim
kill worker after provider effect but before completion commit
expire lease and return zombie worker
reopen durable store
corrupt/incomplete non-authoritative observation
quota reset during active claims
provider becomes unavailable after reservation
session disappears
context drops below threshold
checkpoint fails
duplicate callback/signal
plan migration attempted with active task
repair stagnates
repair oscillates
continue-as-new crash window
```

Success criteria:

- no false completion;
- no stale commit;
- no lost durable obligation;
- no unbounded retry/repair;
- no invented provider capacity/session affinity;
- no duplicate logical execution from replay;
- explainable terminal/wait state.

---

# 23. Non-goals

ADGO Agent **не должен** превращаться в coding framework.

Не добавлять upstream:

- Git worktree management;
- GitHub main publication;
- `MASTER_PLAN.md` parser/process;
- Go test/mutation commands;
- Codex-specific process invocation;
- Antigravity IDE automation;
- coding role prompts;
- repository-specific acceptance policy;
- UI tailored only to `agctl`.

Generic contract — upstream; concrete application adapter — downstream.

---

# 24. Definition of Done

ADGO Agent Platform считается готовым для `agctl` default adoption, когда:

- ADGO остаётся единственным orchestration authority;
- `adgo/agent` не содержит второго scheduler/store/workflow engine;
- Provider/Account/Model/Session имеют stable validated identities;
- quota metrics сохраняются в native units;
- conservative demand + atomic reservations проходят race/fault tests;
- session broker детерминирован и fail-closed на unknown affinity/context;
- AgentRun переживает множество Generation и process restarts;
- stale generation cannot commit;
- checkpoint rotation durable и bounded;
- AgentRuntime ABI доказан минимум двумя downstream adapters;
- SQLite backend проходит crash/reopen/migration tests;
- continue-as-new ограничивает бесконечный history growth;
- plan migration учитывает active agent obligations;
- repair/oscillation semantics являются canonical и downstream не дублирует их;
- full race/vet/property/mutation/fault suite зелёная;
- external consumer (`agctl`) проходит pinned integration tests;
- tagged release/frozen commit опубликован для migration wave.

---

# 25. Первый практический milestone

Первый milestone должен быть небольшим и доказательным:

```text
U-01 ownership model
  + U-02 parity audit
  + U-04 provider primitives
  + U-05 capacity model
  + U-09 session broker contract
```

Затем `agctl` подключает эти types через один `internal/adgobridge` в read-only shadow path. Только после совпадения semantics переносить reservations, AgentGeneration и execution ownership.

Это позволит использовать уже написанный Harness как ценный R&D/test oracle, но прекратит долгосрочное дублирование общей orchestration платформы.