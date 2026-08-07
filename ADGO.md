# Axiom ADGO — production durable orchestration engine

ADGO (**Adaptive Durable Graph Orchestration**) — production-движок Axiom для долгоживущих графов, LLM/tool/agent workflows, human-in-the-loop, quality gates, targeted repair, внешних эффектов и процессов, которые обязаны переживать падение worker/coordinator без потери логики выполнения.

> **Главный принцип: durable committed state — источник истины. Go call stack, goroutine и конкретный процесс — расходный материал.**

Полная документация:

- [`adgo/README.md`](adgo/README.md) — production guide и API;
- [`adgo/ARCHITECTURE.md`](adgo/ARCHITECTURE.md) — архитектурные инварианты;
- [`adgo/OPERATIONS.md`](adgo/OPERATIONS.md) — эксплуатационный runbook;
- [`adgo/examples/production`](adgo/examples/production) — runnable production topology;
- [`adgo/examples/iris`](adgo/examples/iris) — сложный quality/repair workflow.

## Когда использовать обычный Axiom, а когда ADGO

| Задача | Runtime |
|---|---|
| Lifecycle одного бизнес-объекта: заказ, заявка, оборудование | `axiom.Engine` + `model` |
| Таблица решений | `table` |
| Компактный typed reducer | `axiom.Flow` |
| Долгий dependency graph с параллелизмом | `adgo.Engine` |
| LLM/search/browser/tool pipeline | `adgo.Engine` |
| Human approval, callbacks, timers, repair | `adgo.Engine` |
| Несколько версий workflow и child workflows | `adgo.Host` |
| Готовая production-сборка storage/router/cache/schedules | `adgo.OpenProduction` |

Обычный Axiom моделирует прежде всего **жизненный цикл доменного объекта**. ADGO моделирует **durable execution вычислительного/агентного графа**.

## Четыре уровня API

```text
Runtime
  embedded deterministic kernel

Engine
  production coordinator + durable workers

Host
  many immutable Plan versions + child workflows

OpenProduction
  Engine + durable store + provider health + admission + cache + schedules
```

Для нового production-сервиса начинайте с `OpenProduction` или `Host`.

## Production quick start

```go
plan, err := adgo.Compile(definition)
if err != nil {
    return err
}

registry := adgo.NewRegistry()
registry.Activity("Generate", generate)
registry.Activity("Publish", publish)
registry.Compensation("Unpublish", unpublish)

production, err := adgo.OpenProduction(
    plan,
    registry,
    adgo.DefaultProductionConfig("./var/adgo"),
)
if err != nil {
    return err
}
defer production.Close()

_, err = production.Engine.StartOrLoad(
    ctx,
    "article-42", // workflow-level idempotency key
    initialFacts,
    adgo.BudgetLimit{MaxCost: 10},
)
if err != nil {
    return err
}

go production.Engine.RunResilientCoordinator(ctx)
go production.Engine.RunWorker(ctx, adgo.WorkerSpec{
    ID:          "worker-a",
    Concurrency: 8,
})
```

## Что теперь умеет production Engine

### Durable coordinator/worker

Coordinator только принимает deterministic control decisions и **сначала сохраняет `TaskPending`**.

Worker отдельно:

1. `Poll`;
2. CAS claim;
3. `TaskRunning + WorkerID + LeaseUntil`;
4. activity call;
5. heartbeat;
6. `Complete` / `Fail`.

Late/zombie worker не может перезаписать более новый результат: commit требует совпадения execution/task/worker/attempt и живого lease.

### Crash recovery

- worker умер → lease истёк → новая попытка;
- старый worker вернулся → `ErrStaleTask`;
- coordinator умер → следующий продолжает из committed state;
- coordinator умер во время compensation → resilient coordinator продолжает оставшийся stack;
- повторные worker losses → durable operator quarantine вместо бесконечного churn.

### Human-in-the-loop

Durable decisions:

- approve;
- edit;
- reject;
- retry;
- confirm;
- abort.

Patch/payload/actor/reason фиксируются до продолжения execution.

### External callbacks

`Awaitable` выдаёт стабильный callback token, связанный с:

- execution;
- node;
- event;
- revision;
- PlanDigest.

Callback от устаревшей repair-итерации становится stale и не может разбудить новую.

### Targeted repair

Repair не делает `goto` и не рестартует весь pipeline.

```text
failed gate
    ↓
repair roots
    ↓
minimal dependency subgraph
    ↓
invalidate only affected outputs
    ↓
new revision epoch
    ↓
re-run
```

Repair ограничивается iteration/cost/duration/epsilon. Есть stagnation и oscillation detection.

### Adaptive provider routing

Provider сначала проходит hard filters:

- permissions;
- privacy;
- risk;
- cost;
- latency;
- availability;
- minimum quality.

Затем учитываются online-наблюдения:

- reliability;
- EWMA latency;
- EWMA quality;
- EWMA cost;
- consecutive failures;
- circuit breaker;
- exploration bonus.

Provider health может храниться durable и быть общей для нескольких coordinator-процессов.

### Global admission

`AdmissionController` защищает общий upstream across executions/processes:

- max concurrency;
- token-bucket rate;
- burst;
- crash-expiring permits.

Admission denial превращается в штатный `FailureRateLimit`, поэтому retry/backoff остаётся единым механизмом.

### Pure-work acceleration

Для безопасно повторяемой работы:

- content-addressed result cache;
- single-flight lease;
- hedged execution;
- ensemble execution;
- quality-based deterministic winner;
- aggregate budget accounting.

Speculation требует явного `Pure=true` и не предназначена для необратимых side effects.

### Time travel

- immutable versions;
- inspect historical version;
- fork execution из прошлого snapshot;
- replay без повторного вызова вероятностных activities.

### Live plan migration

`MigrateExecution` разрешает явную совместимую миграцию только в quiescent point. Completed semantics по умолчанию нельзя незаметно переопределить.

### Continue-as-new

Бесконечный логический процесс можно перенести в новое execution с выбранными durable facts/artifacts, сохранив старое execution как audit trail.

### Multi-plan Host

Один Host обслуживает несколько immutable PlanDigest одновременно. Старые и новые версии workflow могут завершаться параллельно.

Child workflow может работать на другом Plan и имеет deterministic ID:

```text
<parent>/<node>/<item>
```

Redelivery parent activity не создаёт duplicate child.

### Storage

Встроены:

| Backend | Назначение |
|---|---|
| `MemoryStore` | tests/ephemeral |
| `FileStore` | durable shared-filesystem multi-process |
| `PebbleStore` | high-throughput local durable engine |

`PebbleStore` атомарно хранит latest execution + immutable version, inbox и catalog.

Storage расширяется capability interfaces, а не одним огромным interface:

- `ExecutionCatalog`;
- `VersionedStore`;
- `ExecutionDeletionStore`;
- `VersionPruner`.

Для cloud/multi-host без shared FS можно реализовать `Store` поверх transactional SQL/KV.

### Schedules

Durable fixed-interval schedules имеют deterministic firing ID. Crash между workflow start и schedule cursor commit не создаёт duplicate execution благодаря `StartOrLoad`.

### Operations

- `Diagnostics`;
- `AuditExecution`;
- `AuditFleet`;
- `Watch` history stream;
- `QueryExecutions`;
- `Pause` / `Resume` / `Cancel`;
- `RewindFrom`;
- graceful `BeginDrain` / `Drain`;
- retention + archive hook;
- immutable-version pruning.

## External effects: важнейшая гарантия

ADGO **не обещает exactly-once внешний I/O**.

До вызова внешнего effect движок сохраняет task + idempotency key. Если процесс падает после принятого provider effect, но до локального completion commit, activity может быть доставлена повторно.

Поэтому effect handler обязан:

1. передавать `ActivityRequest.IdempotencyKey` provider'у; или
2. уметь определить уже выполненную операцию; или
3. вернуть `FailureAmbiguousSideEffect` и перейти в durable reconciliation.

Это честная и проверяемая модель вместо ложного exactly-once обещания.

## Storage и секреты

Execution storage — чувствительные application data.

Не помещайте API keys/passwords/tokens в `Execution.Data`.

Храните credentials в worker environment / secret manager. В durable state сохраняйте только ссылки и безопасные факты.

Operator patches не могут переписывать зарезервированные `__adgo:` keys.

## Проверки

Основной CI Axiom проверяет:

```bash
go test ./...
go test -race ./...
go vet ./...
```

плюс dependency/reachable-code scan, fuzz smoke, external-consumer compilation и performance jobs.

Для ADGO:

```bash
go test ./adgo/... -count=1
go test -race ./adgo -count=1
go vet ./adgo/...
go run ./adgo/examples/production
```

Regression suite моделирует не только happy path, но и worker death, fencing, heartbeat, crash recovery, provider fallback, repair anchors, migration, historical fork, callbacks, schedules, Pebble reopen, compensation recovery, cache, deterministic signals, operator rewind, speculation, drain, retention и multi-plan host.

## Навигация

- [`adgo/README.md`](adgo/README.md) — полный production guide;
- [`adgo/ARCHITECTURE.md`](adgo/ARCHITECTURE.md) — архитектурный контракт;
- [`adgo/OPERATIONS.md`](adgo/OPERATIONS.md) — runbook эксплуатации;
- [`adgo/examples/production`](adgo/examples/production) — production topology;
- [`adgo/examples/iris`](adgo/examples/iris) — quality/repair example;
- [`README.md`](README.md) — основной Axiom lifecycle Engine;
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — архитектура обычного Axiom runtime.
