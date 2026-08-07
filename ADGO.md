# Axiom ADGO — выбор runtime и эксплуатационный контракт

ADGO (**Adaptive Durable Graph Orchestration**) — графовый orchestration runtime внутри Axiom для процессов, где обычной модели «событие → переход состояния» недостаточно.

Основная реализация и полный API reference находятся в [`adgo/README.md`](adgo/README.md), архитектурные принципы — в [`adgo/ARCHITECTURE.md`](adgo/ARCHITECTURE.md).

## Когда использовать обычный Axiom Engine, а когда ADGO

| Задача | Рекомендуемый runtime |
|---|---|
| Бизнес-объект с lifecycle: заказ, заявка, оборудование, платёж | `axiom.Engine` + `model` |
| Таблица решений | `table` |
| Компактный typed reducer | `axiom.Flow` |
| Длинный граф с параллельными ветками, quality gates и repair | `adgo` |
| LLM/search/agent workflow с вероятностными workers | `adgo` |
| Durable waits, human approval, fan-out/join, compensation | `adgo` |
| Targeted rework вместо полного перезапуска pipeline | `adgo` |

Ключевое различие: обычный Axiom моделирует прежде всего **состояние доменного объекта**, ADGO — **durable execution графа**.

## Базовый принцип

```text
probabilistic workers
LLM / search / browser / tools
          │
          │ facts + artifacts
          ▼
 deterministic ADGO control plane
          │
          ├── gates
          ├── policies
          ├── budgets
          ├── retries
          ├── repair
          ├── human approval
          └── compensation
```

Activity не должна произвольно выбирать следующую вершину графа. Она возвращает наблюдения. Решение о переходе принимает deterministic control plane.

## Что хранит ADGO

ADGO хранит компактное execution state:

- `ExecutionID`;
- `PlanID`, `PlanVersion`, `PlanDigest`;
- status каждой вершины;
- attempts и durable retry timers;
- leases активных задач;
- revision counters;
- waiting events;
- quality vector;
- budgets;
- compensation stack;
- execution history;
- ссылки на artifacts.

Большие доменные объекты — документы, изображения, корпуса источников, черновики — должны жить вне execution state. В ADGO следует хранить их `ArtifactRef` или другие стабильные ссылки.

## Immutable Plan и resume

Каждое execution прикреплено к конкретному `PlanDigest`.

```text
Definition
   ↓ compile + validation
immutable Plan
   ↓
Execution(plan digest pinned)
```

После рестарта runtime продолжает тот же execution только с тем же планом. Нельзя незаметно заменить граф или политику уже выполняющегося процесса.

Если прикладная система имеет собственную конфигурацию поверх ADGO, включайте behavior-affecting настройки в версию/идентичность Definition, но не помещайте credentials в durable state.

## Activity delivery: at-least-once

ADGO не обещает exactly-once внешний эффект.

Перед вызовом activity runtime сохраняет task/attempt/lease/idempotency key. Если процесс упал после внешнего эффекта, но до commit результата, activity может быть доставлена повторно.

Поэтому внешняя activity должна выполнять одно из трёх правил:

1. передавать `ActivityRequest.IdempotencyKey` внешнему сервису;
2. уметь определить, что операция уже завершена;
3. при неоднозначном результате возвращать `FailureAmbiguousSideEffect` и переходить к reconciliation.

Небезопасный external-effect node без timeout, idempotency и bounded retry должен быть отвергнут компилятором.

## Targeted repair

Repair — не `goto` и не полный рестарт pipeline.

```text
failed gate
    ↓
violations
    ↓
repair root(s)
    ↓
DependencyRepairPlanner
    ↓
minimal affected subgraph
    ↓
re-run only affected work
    ↓
re-evaluate gate
```

Работа вне affected subgraph сохраняется.

Каждый repair root обязан иметь полный `LoopBound`:

- `MaxIterations`;
- `MaxCost`;
- `MaxDuration`;
- `Epsilon`.

Это не позволяет вероятностному worker бесконечно «улучшать» результат.

## Независимые repair budgets

Если несколько gates ремонтируют общий downstream node, не используйте один общий revision counter. Применяйте отдельные repair anchors:

```text
gate A -> anchor A --\
                    +--> shared work
gate B -> anchor B --/
```

У каждого anchor собственный `LoopBound` и revision epoch. Этот паттерн покрыт regression test `adgo/repair_anchor_test.go`.

## Durable waits и human-in-the-loop

Wait/Human nodes являются частью execution state, а не блокирующим вызовом Go.

Runtime может остановиться со статусом waiting/awaiting-human, процесс может завершиться, а затем новое событие через durable inbox продолжит execution.

Для high-risk side effects рекомендуется approval до вызова effect activity.

## Compensation

Для обратимых side effects регистрируйте compensation handlers. При cancellation/permanent failure compensation stack выполняется в обратном порядке.

Compensation не является магической транзакцией: handler также должен быть идемпотентным и устойчивым к повторной доставке.

## Storage

Встроены:

- `MemoryStore` — tests/local ephemeral runs;
- `FileStore` — durable reference backend.

`FileStore` использует immutable execution snapshots, CAS versioning, filesystem locks и durable inbox.

Типовая структура:

```text
root/
├── executions/<execution>/commits/<version>.json
├── executions/<execution>/inbox/<event>.json
└── locks/<execution>.lock
```

Для production deployment с несколькими hosts используйте реализацию `Store` с подходящей транзакционной/CAS семантикой или общий filesystem с корректными гарантиями. `FileStore` — reference implementation, а не universal distributed database.

## Наблюдаемость

Для диагностики используйте:

- `Execution.Status`;
- `Execution.History`;
- `Execution.Metrics`;
- `Explain(plan, execution, nodeID)`;
- `VerifyReplay` для аудита immutable committed snapshots.

Полезные метрики:

- wall time;
- active compute time;
- queue time;
- retries/timeouts;
- repair count;
- human interventions;
- cost/tokens;
- quality gain per cost;
- quality gain per repair.

## Типовые причины остановки

### `waiting`

Это не обязательно ошибка. Проверьте:

- durable timer / `NotBefore`;
- ожидаемое external event;
- provider throttle;
- ambiguous side effect reconciliation.

### `awaiting_human`

Требуется approval/reconciliation event. Не обходите это ручным изменением snapshot.

### `deadlocked`

Используйте `Explain`. Обычно причина — отсутствующий fact, неактивируемая dependency или логическая ошибка Definition.

### `failed`

Проверьте failure class, retry exhaustion, budget exhaustion и compensation history.

## Безопасное обновление workflow

Для уже запущенного execution не редактируйте граф «на месте».

Правильная модель:

1. создайте новую Definition/версию;
2. скомпилируйте новый immutable Plan;
3. новые executions запускайте на новой версии;
4. старые executions завершайте на pinned Plan;
5. если нужна адаптация — используйте validated `PlanProposal -> child Plan`, а не mutation live parent graph.

## Проверки перед выпуском

```bash
go test ./adgo/...
go test -race ./adgo
go vet ./adgo/...
go run ./adgo/examples/iris
```

Для изменения repair/recovery/idempotency обязательно добавляйте regression test, моделирующий crash/retry/re-delivery, а не только happy path.

## Навигация

- [`adgo/README.md`](adgo/README.md) — полный usage guide и API examples;
- [`adgo/ARCHITECTURE.md`](adgo/ARCHITECTURE.md) — внутренние архитектурные решения;
- [`adgo/examples/iris`](adgo/examples/iris) — runnable workflow;
- [`README.md`](README.md) — основной Axiom lifecycle Engine;
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — архитектура основного Axiom runtime.
