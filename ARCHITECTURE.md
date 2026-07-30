# Архитектура Axiom

## Назначение документа

Документ описывает подтверждённую кодом архитектуру библиотеки Axiom: frontends, канонический `Plan`, compiler/runtime, хранилища, activity boundary, историю и replay.

Он не является обещанием ещё не реализованного поведения. Текущие ограничения вынесены в отдельные разделы.

## Общая схема

```mermaid
flowchart LR
    Flow[Typed Go Flow] --> FlowRuntime[Flow runtime]
    FlowRuntime --> FlowStore[FlowStore]
    FlowRuntime --> Effects[Go effect handlers]

    GoModel[Declarative Go model] --> Plan[axiom.Plan]
    AXM[AXM source] --> Compiler[Parser + compiler]
    TOML[TOML table] --> AXMRender[TOML to AXM rendering]
    TRIZ[TRIZ source] --> Normalize[TRIZ normalization]
    AXMRender --> Compiler
    Normalize --> Compiler
    Compiler --> Plan

    Plan --> Engine[Compiled Engine]
    Engine --> Store[Store]
    Store --> Memory[Memory store]
    Store --> Pebble[Pebble transactional store]
    Engine --> Tasks[Activity tasks]
    Tasks --> Worker[Inline worker / StartWorker]
    Worker --> Activities[Registered Go activities]
    Engine --> History[Execution history]
    History --> Replay[ReplayFromHistory]
```

## Компоненты

| Компонент | Ответственность | Основные файлы |
|---|---|---|
| Public API | Загрузка, компиляция, options, activity registration, replay | `axiom.go`, `plan.go`, `runtime_aliases.go` |
| Typed Go Flow | File-free reducer API с отдельным store и effects | `flow.go` |
| Declarative Go model | Генерация AXM из типизированных Go declarations | `model/model.go`, `model/typed.go` |
| AXM frontend | Загрузка и компиляция `.axm` в `Plan` | `axm/axm.go`, `internal/lang/*`, `internal/compiler/*` |
| TOML frontend | Разбор таблицы решений и рендеринг в AXM | `table/toml.go` |
| TRIZ normalization | Преобразование пользовательского TRIZ-синтаксиса в AXM v0 | `internal/triz/*`, exported API в `axiom.go` |
| Canonical Plan | Общая форма для декларативных frontends | `plan.go` |
| Compiled runtime | Execution lifecycle, rules, claims, tasks, queries | `internal/runtime/*` |
| Stores | Memory и Pebble implementations | `internal/store/memory/*`, `internal/store/pebble/*`, `store/pebble/pebble.go` |
| Code generation | Типизированные activity boundaries | `cmd/axiomgen/*` |
| Diagrams | Mermaid и PlantUML из module/history | `diagram/diagram.go` |
| Compatibility analysis | Bundle diff и impact report | `bundle.go` |
| Benchmarks | Нагрузочные сценарии и percentile reports | `cmd/axiombench/main.go`, `benchmarks/latest.md` |

## Канонический Plan

`axiom.Plan` содержит имя, версию, digest, формат, уровень анализа и закрытую ссылку на скомпилированный module.

Декларативная Go-модель, AXM и TOML реализуют или используют `PlanSource` и сходятся в одном runtime. Typed Go Flow остаётся отдельным frontend, потому что произвольные Go handlers нельзя полностью статически проанализировать.

Текущие версии compiled artifacts задаются в compiler:

- DSL: `axm/v1`;
- compiler: `axiom-compiler/v2`;
- fast plan: `fast-plan/v2`.

## Компиляция

```mermaid
flowchart TD
    Source[AXM source] --> Parse[internal/lang.Parse]
    Parse --> AST[lang.Module AST]
    AST --> Symbols[Collect symbols]
    Symbols --> Validate[Validate expressions, activities, rules, policies, cycles]
    Validate --> Indexes[Build dependency indexes]
    Indexes --> IDs[Build stable ID tables]
    IDs --> Hash[Compute compiled hash]
    Hash --> Module[compiler.Module]
    Module --> Plan[axiom.Plan]
```

Компилятор обнаруживает синтаксические ошибки, неразрешённые ссылки, дубликаты, циклы, неверные activity/policy связи и ряд требований к external activity до запуска runtime.

## Execution lifecycle

```mermaid
stateDiagram-v2
    [*] --> Started
    Started --> Running: signal / patch / dispatch
    Running --> Waiting: fixpoint reached
    Waiting --> Running: next input or activity result
    Running --> Failed: runtime, claim or activity error
    Waiting --> Canceled: cancel
    Running --> Canceled: cancel
    Failed --> [*]
    Canceled --> [*]
```

`StatusCompleted` объявлен в runtime types и поддерживается replay representation, но автоматический переход execution в `Completed` в проверенном execution path не найден. Документация не должна обещать completion rule до появления явной реализации и tests.

`Run.Dispatch`:

1. проверяет execution handle и event;
2. создаёт execution, если он отсутствует;
3. отправляет signal;
4. запускает `RunUntilIdle`;
5. возвращается после исчерпания доступных inline tasks либо при ошибке.

Операции одного execution сериализуются keyed lock по ID внутри одного `Engine`.

## Обработка signal и rule

```mermaid
sequenceDiagram
    participant C as Caller
    participant E as Engine
    participant S as Store
    participant A as Activity

    C->>E: Dispatch(event)
    E->>S: Get/Create execution
    E->>S: Append SignalReceived
    E->>E: Recompute computed/facts
    E->>E: Evaluate rules and claims
    alt rule has activity
        E->>S: Append ActivityScheduled
        E->>S: Enqueue task
        E->>A: Execute registered handler
        A-->>E: result or error
        E->>S: Append ActivityCompleted/Failed
    end
    E->>E: Apply writes and claims
    E->>S: Append WriteApplied / ExecutionReachedFixpoint
    E->>S: Save execution
```

Точный набор history entries зависит от trace level:

- всегда или в основных путях: `ExecutionStarted`, `ContextPatched`, `SignalReceived`, `ActivityScheduled`, `ActivityDeduplicated`, `ActivityCompleted`, `ActivityFailed`, `WriteApplied`, `ExecutionReachedFixpoint`, `ExecutionCanceled`;
- `TraceFull`: `RuleScheduled`, `RuleSkipped`;
- `TraceAggregate`: `RulesEvaluated`.

Документация не должна перечислять несуществующие history events как реализованные.

## Хранилища и транзакции

### Memory store

Используется по умолчанию. Подходит для тестов и временных execution. Содержимое теряется при завершении процесса.

### Pebble

Pebble store реализует durable и transactional storage. Публичные варианты:

- sync по умолчанию;
- `PebbleNoSync` / `WithNoSync`;
- `PebbleSyncEvery` / `WithSyncEvery`;
- JSON или Gob codec.

`WithProductionMode` проверяет, что store реализует `TransactionalStore`.

### Граница транзакции

Изменения execution, history и task records выполняются через store transaction там, где runtime вызывает `withStoreTransaction`. Внешняя activity находится за границей локальной транзакции: её результат записывается после завершения Go handler.

Это означает, что внешняя система должна поддерживать безопасную идемпотентность по переданному business key.

## Activity tasks

Activity task содержит execution ID, rule, activity, input, idempotency key, status, attempt, max attempts, lease и result/error.

Runtime умеет:

- ставить task в очередь;
- выдавать task с lease;
- восстанавливать просроченный lease;
- дедуплицировать незавершённую или завершённую task по rule/activity/key;
- фиксировать completed или failed result.

### Текущее состояние policy

| Поле policy | Что подтверждено | Чего нельзя обещать |
|---|---|---|
| `retry` | Парсится; `MaxAttempts = retry + 1` записывается в task | Повтор после ошибки activity handler сейчас не выполняется автоматически |
| `timeout` | Парсится и валидируется как часть модели | Runtime не оборачивает вызов activity в отдельный timeout context |
| `concurrency` | Парсится | Значение policy не управляет планированием; фактическая сериализация задаётся execution lock, а worker concurrency — `WorkerOptions` |
| `idempotency` | Для external activity компилятор требует `required` и key; store выполняет task deduplication | Это не exactly-once гарантия внешнего API или оборудования |

До реализации недостающей семантики эти поля следует документировать как частично поддерживаемые.

## Typed Go Flow

Typed Go Flow — отдельный простой reducer runtime:

```text
load state -> reducer -> claims -> execute effects -> save state and history
```

Критическая граница: effects выполняются до `FlowStore.Save`. Если внешний эффект завершился, а сохранение затем вернуло ошибку, библиотека не может автоматически откатить внешний мир. Поэтому:

- effects должны быть идемпотентны;
- custom `FlowStore` должен иметь понятную модель отказов;
- для durable orchestration с task records предпочтителен compiled runtime.

## Replay

`ReplayFromHistory` восстанавливает execution из history и проверяет module hash. History, созданная другой моделью, отклоняется.

Завершённые activity не запускаются повторно во время replay: используются записанные результаты и writes.

## Границы доверия

1. **Входные events и patches.** Runtime проверяет объявленные signals и типы значений, но бизнес-авторизация остаётся приложению.
2. **Activity handlers.** Это доверенный пользовательский Go-код с доступом к сети, оборудованию и секретам приложения.
3. **Store.** Custom store должен соблюдать контракты атомарности, task leasing и history ordering.
4. **Execution ID.** Приложение отвечает за уникальность, маршрутизацию и распределённое владение.
5. **Generated code.** `*_axiom.gen.go` перегенерируется; пользовательская реализация activity хранится отдельно.

## Точки расширения

- `Store` и `TransactionalStore`;
- `FlowStore`;
- `Activity` / `ActTyped`;
- `PlanSource`;
- отдельные frontends поверх канонического Plan;
- tooling через `ModuleBundle`, dependency indexes и package `diagram`.

## Известные ограничения

- нет распределённого lock/owner router;
- runtime policy retry/timeout/concurrency реализованы не полностью;
- автоматический переход в `Completed` не подтверждён execution path;
- Typed Go Flow не имеет статического dependency graph;
- imports разбираются parser, но полноценная загрузка/линковка нескольких AXM modules не подтверждена в проверенном runtime path;
- exported TRIZ API присутствует, но его уровень стабильности должен быть явно определён владельцем проекта;
- отдельного HTTP API, миграций БД и env-based configuration в проекте не обнаружено.
