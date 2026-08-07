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
| Tasks хранят input, status, durable attempt budget, lease, retry deadline, result/error | Подтверждено кодом |
| Completed task не выполняется повторно при replay/recovery | Подтверждено test |
| `retry: N` даёт максимум `N + 1` durable handler attempts | Подтверждено кодом и test |
| `timeout` отменяет контекст одной handler-попытки | Подтверждено кодом и test |
| `concurrency: once` сериализует вызовы одной activity | Подтверждено внутри одного Engine |
| `concurrency: parallel` не добавляет сериализацию | Подтверждено |
| `concurrency: latest/first` выполняет supersession | Не реализовано; production отклоняет |
| Expired running lease может быть возвращён в pending | Подтверждено кодом |

## Durable retry

`retry: N` означает **до N повторов после исходной попытки**, то есть максимум `N + 1` persisted task attempts.

Одна lease соответствует одной попытке handler:

```text
lease attempt 1
  -> handler failure
  -> persist pending + Attempt=1 + NextAttemptAt
  -> process may exit

lease attempt 2
  -> handler success
  -> complete task
```

`ActivityTask.Attempt` теперь отражает фактическое число выданных handler attempts. `MaxAttempts` сохраняет полный budget (`retry + 1`). При retry runtime очищает текущий lease, возвращает task в `pending`, сохраняет последнюю ошибку и выставляет `NextAttemptAt`.

Retry checkpoint переживает создание нового `Engine` с тем же store. Для Pebble он также переживает закрытие и повторное открытие store. Это делает retry durable относительно падения процесса **между** попытками.

### Backoff

Policy может задавать задержку явно:

```axiom
policy externalCall:
  retry: 3
  backoff: 250ms
```

Duration без wrapper означает fixed delay. Эквивалентная явная форма:

```axiom
backoff: fixed(250ms)
```

Для exponential delay:

```axiom
backoff: exponential(100ms)
```

В Go-model используются:

```go
policy := definition.Policy("externalCall")
policy.Retry(3).Backoff(250 * time.Millisecond)
policy.Retry(3).ExponentialBackoff(100 * time.Millisecond)
```

Если `retry` задан, а `backoff` отсутствует, runtime использует deterministic exponential backoff с базой `100ms`. Задержка ограничена сверху `30s`, чтобы повреждённая или слишком большая policy не создавала практически бесконечный wait.

### Ошибки и retryability

Автоматически повторяются ошибки зарегистрированного handler, включая per-attempt timeout. Детерминированные ошибки runtime contract после handler, например несовместимый activity output, не превращаются в retry loop.

Отмена родительского `context.Context` прекращает ожидание и дальнейшие попытки. Уже сохранённый pending task остаётся в store и может быть продолжен другим worker/Engine позднее, если execution не был отдельно отменён.

### High-level и low-level API

`Run.Dispatch`, `Run.Signal` и `Run.Patch` сохраняют синхронную эргономику: если retry checkpoint создан, они ждут `NextAttemptAt` и продолжают drain, пока activity не завершится успешно, не исчерпает budget или caller context не завершится.

Низкоуровневый `Engine.RunUntilIdle` не спит до будущего retry. После сохранённого checkpoint он возвращает `RetryScheduledError`, который можно распознать через:

```go
errors.Is(err, axiom.ErrRetryScheduled)
```

Это позволяет внешнему worker/scheduler сразу освободить goroutine. Если pending tasks существуют, но ни одна ещё не достигла `NextAttemptAt`, `RunUntilIdle` возвращает `nil` как временно idle.

## Timeout

`timeout` применяется **к каждой handler-попытке отдельно** через `context.WithTimeout`.

Handler обязан соблюдать переданный `context.Context`. Библиотека не может безопасно принудительно остановить произвольный Go-код, который игнорирует отмену контекста.

Если попытка завершается по deadline, она считается retryable handler failure. Отмена родительского context прекращает текущий synchronous drain.

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
3. разрешает durable `retry`, per-attempt `timeout`, `concurrency: once` и `concurrency: parallel`;
4. сохраняет retry checkpoint и соответствующую history атомарно внутри store transaction;
5. отклоняет `concurrency: latest/first` через `AX508` до появления корректной durable task supersession semantics.

## Idempotency

Task deduplication ищет task по execution, rule, activity и idempotency key. Незавершённая или completed task не планируется повторно. Failed task не блокирует новое планирование.

Это локальная гарантия store/runtime, а не exactly-once гарантия для сети, платёжной системы или оборудования. Activity handler должен передавать тот же key внешней системе или реализовать собственный deduplication record.

Durable retry делает идемпотентность особенно важной: внешний effect мог успеть произойти до сетевой ошибки или падения процесса, поэтому одна task может законно вызвать handler несколько раз.

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
- `ActivityRetryScheduled` — attempt завершился retryable ошибкой и task durably возвращена в pending;
- `ActivityRetryExhausted` — текущая ошибка исчерпала `MaxAttempts`;
- `ActivityCompleted`;
- `ActivityFailed` — terminal failure;
- `WriteApplied`;
- `ExecutionReachedFixpoint`;
- `ExecutionCanceled`.

`ActivityRetryScheduled` содержит task ID, activity/rule, текущий attempt, `maxAttempts`, delay, `nextAttemptAt` и ошибку. `ActivityFailed` не пишется для промежуточной retry-попытки.

## Lease recovery

Если процесс исчез после получения task, но до complete/fail checkpoint, lease может быть восстановлена через `RecoverExpiredLeases`: task возвращается из `running` в `pending` и может быть выдана снова.

Такой новый lease увеличивает `Attempt`, поскольку внешний effect предыдущей попытки мог успеть начаться. Поэтому idempotency contract остаётся обязательной частью безопасной работы с external activities.

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

1. `latest/first` через атомарную task supersession semantics.
2. Явный distributed ownership/coordination contract для нескольких Engine/processes.
3. Runtime dispatch для policy `catch:`.
4. Operational runbook для stuck leases, failed executions и manual recovery.
5. Wall-clock scheduler для timer triggers.
