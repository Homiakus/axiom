# Runtime-семантика и границы гарантий

Этот документ отделяет реализованное поведение от parsed configuration и планируемых возможностей.

## Статусы подтверждения

- **Подтверждено кодом** — поведение реализовано в текущем runtime path и покрыто tests.
- **Частично реализовано** — часть semantics существует, но остаются ограничения, важные для production.
- **Не реализовано** — синтаксис может существовать, но соответствующей runtime-гарантии нет.

## Execution

| Утверждение | Статус |
|---|---|
| Один execution идентифицируется строковым ID | Подтверждено кодом |
| Операции одного ID сериализуются внутри одного Engine | Подтверждено кодом |
| Разные IDs могут выполняться параллельно | Подтверждено кодом |
| Блокировка действует между процессами | Не реализовано |
| `Dispatch` автоматически создаёт отсутствующий execution | Подтверждено кодом |
| Production mode требует transactional store | Подтверждено кодом и test |
| Production mode принимает `retry`, `timeout`, `concurrency: once/parallel` | Подтверждено кодом и test |
| Production mode отклоняет `concurrency: latest/first` | Подтверждено кодом и test (`AX508`) |

## Activity

| Утверждение | Статус |
|---|---|
| External activity должна быть зарегистрирована в Go | Подтверждено кодом |
| External activity требует idempotency policy и key | Подтверждено compiler validation |
| `ActTyped` отклоняет scalar input/output и nil handler при создании Engine | Подтверждено кодом и test (`AX507`) |
| `ActTyped` принимает structs, pointers to structs и maps со string keys | Подтверждено кодом |
| Tasks хранят input, status, attempts, lease, result/error | Подтверждено кодом |
| Completed task не выполняется повторно при replay/recovery | Подтверждено test |
| Handler повторяется по `retry` | Подтверждено: до `retry + 1` вызовов |
| `timeout` отменяет контекст одной handler-попытки | Подтверждено кодом и test |
| `concurrency: once` сериализует вызовы одной activity | Подтверждено внутри одного Engine |
| `concurrency: parallel` не добавляет сериализацию | Подтверждено |
| `concurrency: latest/first` выполняет supersession | Не реализовано; production отклоняет |
| Expired running lease может быть возвращён в pending | Подтверждено кодом |

## Retry

`retry: N` означает **до N повторов после исходного вызова**, то есть максимум `N + 1` вызовов зарегистрированного handler.

Текущая реализация выполняет эти повторы внутри одного leased task и одного процесса:

```text
lease task -> handler attempt 1 -> ... -> handler attempt N+1 -> complete/fail task
```

Повторы сейчас немедленные. Backoff, `NextAttemptAt` между handler-попытками и отдельные durable history entries для каждой попытки ещё не реализованы.

Это важная граница: поле `ActivityTask.Attempt` отражает попытки выдачи task/lease, а не каждую внутреннюю попытку handler. Поэтому текущий retry защищает от кратковременной ошибки handler во время живого процесса, но не является полноценной durable retry queue после падения процесса между попытками.

## Timeout

`timeout` применяется **к каждой handler-попытке отдельно** через `context.WithTimeout`.

Handler обязан соблюдать переданный `context.Context`. Библиотека не может безопасно принудительно остановить произвольный Go-код, который игнорирует отмену контекста.

Если попытка завершается по deadline, она считается ошибочной и может быть повторена в пределах `retry`. Отмена родительского context прекращает дальнейшие повторы.

## Concurrency

Поддерживаемые режимы:

- `parallel` — runtime не добавляет ограничение на параллельные вызовы activity;
- `once` — вызовы одной activity сериализуются mutex-ом внутри конкретного `Engine`;
- `latest` и `first` — синтаксис парсится, но production mode возвращает `AX508`, потому что корректная semantics требует атомарной отмены/замещения pending tasks.

`once` — **process-local guarantee**. Он не является распределённым lock между несколькими процессами или несколькими Engine. Для такого сценария приложению всё ещё нужен ownership/router или внешний coordination mechanism.

## Production guardrail

`WithProductionMode()`:

1. требует `TransactionalStore`;
2. включает strict fast runtime;
3. разрешает `retry`, `timeout`, `concurrency: once` и `concurrency: parallel`;
4. отклоняет `concurrency: latest/first` через `AX508` до появления корректной durable task supersession semantics.

Таким образом production mode больше не блокирует те policy-поля, которые runtime действительно выполняет, но не обещает semantics, которых ещё нет.

## Idempotency

Task deduplication ищет task по execution, rule, activity и idempotency key. Незавершённая или completed task не планируется повторно. Failed task не блокирует новое планирование.

Это локальная гарантия store/runtime, а не exactly-once гарантия для сети, платёжной системы или оборудования. Activity handler должен передавать тот же key внешней системе или реализовать собственный deduplication record.

Retry делает идемпотентность ещё важнее: handler может быть вызван несколько раз при одном task.

## Claims и writes

Claims проверяются до и после writes. При нарушении write откатывается в рабочем execution snapshot, а ошибка возвращается caller.

Transactional store определяет атомарность persisted execution/history/task records. Внешний effect не может быть откатан локальной БД.

## History

Подтверждённые entry types включают:

- `ExecutionStarted`;
- `ContextPatched`;
- `SignalReceived`;
- `RuleScheduled` и `RuleSkipped` при full trace;
- `RulesEvaluated` при aggregate trace;
- `ActivityScheduled`;
- `ActivityDeduplicated`;
- `ActivityCompleted`;
- `ActivityFailed`;
- `WriteApplied`;
- `ExecutionReachedFixpoint`;
- `ExecutionCanceled`.

Внутренние handler-retry пока не создают отдельные history entries. `ActivityFailed` фиксируется только после исчерпания доступных retry или terminal cancellation.

## Replay

Replay:

- читает history;
- сверяет module hash;
- восстанавливает context/runtime state;
- не вызывает completed activity повторно.

History без необходимых values или с несовпадающим module hash отклоняется diagnostic error.

## Typed Go Flow

Flow runtime имеет другую durability model:

```text
load -> reducer -> claims -> effects -> save
```

Если effect завершился, а `FlowStore.Save` затем завершился ошибкой, внешний effect уже нельзя отменить. Это ограничение должно быть известно до использования Flow для платежей или оборудования.

## Следующий reliability-слой

1. Durable task-level retry с backoff и `NextAttemptAt`.
2. History entries для каждой retry-попытки и исчерпания retry budget.
3. `latest/first` через атомарную task supersession semantics.
4. Явное distributed ownership/coordination contract для нескольких Engine/processes.
5. Operational runbook для stuck leases, failed executions и manual recovery.
