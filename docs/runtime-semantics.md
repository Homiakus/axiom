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
| Production mode принимает `retry`, `timeout`, `parallel`, `once`, `first`, `latest` | Подтверждено кодом и test |

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
| `concurrency: first` сохраняет первый active task | Подтверждено кодом и test |
| `concurrency: latest` заменяет старые pending tasks новым pending task | Подтверждено кодом и test |
| `latest` принудительно отменяет уже running Go handler | Не реализовано намеренно |
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

`ActivityTask.Attempt` отражает фактическое число выданных handler attempts. `MaxAttempts` сохраняет полный budget (`retry + 1`). При retry runtime очищает текущий lease, возвращает task в `pending`, сохраняет последнюю ошибку и выставляет `NextAttemptAt`.

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

Если `retry` задан, а `backoff` отсутствует, runtime использует deterministic exponential backoff с базой `100ms`. Задержка ограничена сверху `30s`.

### Ошибки и retryability

Автоматически повторяются ошибки зарегистрированного handler, включая per-attempt timeout. Детерминированные ошибки runtime contract после handler, например несовместимый activity output, не превращаются в retry loop.

Отмена родительского `context.Context` прекращает ожидание и дальнейшие попытки. Уже сохранённый pending task остаётся в store и может быть продолжен другим worker/Engine позднее, если execution не был отдельно отменён.

### High-level и low-level API

`Run.Dispatch`, `Run.Signal` и `Run.Patch` сохраняют синхронную эргономику: если retry checkpoint создан, они ждут `NextAttemptAt` и продолжают drain, пока activity не завершится успешно, не исчерпает budget или caller context не завершится.

Низкоуровневый `Engine.RunUntilIdle` не спит до будущего retry. После сохранённого checkpoint он возвращает `RetryScheduledError`, который можно распознать через:

```go
errors.Is(err, axiom.ErrRetryScheduled)
```

Если pending tasks существуют, но ни одна ещё не достигла `NextAttemptAt`, `RunUntilIdle` возвращает `nil` как временно idle.

## Timeout

`timeout` применяется **к каждой handler-попытке отдельно** через `context.WithTimeout`.

Handler обязан соблюдать переданный `context.Context`. Библиотека не может безопасно принудительно остановить произвольный Go-код, который игнорирует отмену контекста.

Если попытка завершается по deadline, она считается retryable handler failure. Отмена родительского context прекращает текущий synchronous drain.

## Concurrency

### `parallel`

Runtime не добавляет activity-level сериализацию. Execution/store ограничения по-прежнему действуют.

### `once`

Вызовы одной activity сериализуются mutex-ом внутри конкретного `Engine`.

`once` — **process-local guarantee**. Он не является distributed lock между несколькими process/Engine.

### `first`

`first` создаёт одну activity lane на пару:

```text
execution ID + activity name
```

Если в lane уже есть `pending` или `running` task, новая scheduled task сохраняется со статусом `superseded`. Первая active task остаётся authoritative.

Это отличается от deduplication: superseded task сохраняется для аудита и получает `ActivitySuperseded` history entry.

### `latest`

`latest` заменяет **только pending work** в той же lane:

```text
pending A
schedule B -> A superseded, B pending
schedule C -> B superseded, C pending
```

Если task уже `running`, runtime не пытается насильно остановить произвольный Go handler. Новый task остаётся pending за running attempt. Следующий newer pending task может supersede именно этот pending task.

Таким образом current guarantee — **latest pending wins**, а не unsafe force-cancel running code.

### Идемпотентность и supersession

Явный непустой `idempotencyKey` сильнее supersession: повтор того же external intent дедуплицируется до решения `first/latest`.

Для `first/latest` пустая строка не трактуется как глобальный idempotency key. Иначе все unkeyed вызовы одной activity схлопывались бы ещё до supersession layer.

`TaskSuperseded` является terminal scheduling status. Такие tasks не выдаются worker-у и не считаются pending activity.

### Атомарность

Production mode требует `TransactionalStore`. Внутри scheduling transaction решение `ListTasks -> supersede/keep -> EnqueueTask -> history` выполняется как один store-level unit.

Встроенный Pebble store сериализует transactions на своём store instance, поэтому concurrent Engines, использующие один Pebble store, получают атомарное pending-supersession решение.

Custom `TransactionalStore` обязан обеспечивать transaction isolation, достаточную для согласованного task scheduling. Сам факт реализации интерфейса не превращает слабую пользовательскую транзакцию в distributed consensus.

## Production guardrail

`WithProductionMode()`:

1. требует `TransactionalStore`;
2. включает strict fast runtime;
3. разрешает durable `retry`/`backoff` и per-attempt `timeout`;
4. разрешает `concurrency: parallel`, `once`, `first`, `latest`;
5. сохраняет retry checkpoints и supersession decisions внутри store transaction;
6. отклоняет неизвестные concurrency modes через `AX508`.

## Idempotency

Task deduplication ищет task по execution, rule, activity и explicit idempotency key. Незавершённая или completed task с тем же explicit key не планируется повторно. Failed task не блокирует новое планирование.

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
- `ActivitySuperseded` — task был отброшен или заменён policy `first/latest`;
- `ActivityRetryScheduled` — attempt завершился retryable ошибкой и task durably возвращена в pending;
- `ActivityRetryExhausted` — текущая ошибка исчерпала `MaxAttempts`;
- `ActivityCompleted`;
- `ActivityFailed` — terminal failure;
- `WriteApplied`;
- `ExecutionReachedFixpoint`;
- `ExecutionCanceled`.

Для `first` history `ActivitySuperseded` указывает сохранённую earlier task. Для `latest` entry указывает `replacedBy` newer task ID.

`ActivityRetryScheduled` содержит task ID, activity/rule, текущий attempt, `maxAttempts`, delay, `nextAttemptAt` и ошибку. `ActivityFailed` не пишется для промежуточной retry-попытки.

## Lease recovery

Если процесс исчез после получения task, но до complete/fail checkpoint, lease может быть восстановлена через `RecoverExpiredLeases`: task возвращается из `running` в `pending` и может быть выдана снова.

Такой новый lease увеличивает `Attempt`, поскольку внешний effect предыдущей попытки мог успеть начаться. Поэтому idempotency contract остаётся обязательной частью безопасной работы с external activities.

## Runtime query namespace

Стабильные `runtime.*` поля:

- `runtime.id`;
- `runtime.domain`;
- `runtime.status`;
- `runtime.version`;
- `runtime.createdAt`;
- `runtime.updatedAt`;
- `runtime.moduleHash`;
- `runtime.compilerVersion`;
- `runtime.planVersion`.

Canonical `Plan`/`Open`/`New` paths отклоняют неизвестные runtime projections через `AX001`. Для Go-model рекомендуется discoverable namespace `model.Runtime.*`.

## Replay

Replay:

- читает history;
- сверяет module hash;
- восстанавливает context/runtime state;
- не вызывает completed activity повторно.

History без необходимых values или с несовпадающим module hash отклоняется diagnostic error.

## Pebble Persisted Format & Codec Compatibility

Встроенный Pebble store (`OpenPebble`, `store/pebble.Open`) защищает персистентные данные явным маркером формата:

1. **JSON по умолчанию:** значением по умолчанию является JSON codec (`codecJSON`).
2. **Gob опционален:** кодек Gob (`codecGob`) доступен через опцию `axiom.PebbleGobCodec()` (`pebblestore.WithGobCodec()`).
3. **Маркер схемы и кодека:** при инициализации хранилища в метаданные атомарно записываются ключи:
   - `meta/axiom-store-schema` со значением версии схемы (`"1"`);
   - `meta/axiom-store-codec` со значением активного кодека (`"json"` или `"gob"`).
4. **Быстрый отказ при несовпадении (Fail-Fast):** попытка открыть существующую базу данных с кодеком, отличным от сохранённого (например, открыть базу, созданную с `gob`, без передачи `PebbleGobCodec()`), завершается ошибкой `store codec mismatch` до чтения или модификации пользовательских записей.
5. **Адаптация legacy-хранилищ:** базы данных без маркеров формата автоматически исследуются на наличие существующих записей, определяют исходный кодек и записывают маркер формата при первом открытии.
6. **Отказ при повреждении или неизвестной версии (Fail-Closed):** неполные маркеры, неподдерживаемые версии схемы или повреждённые метаданные вызывают немедленный отказ на этапе `Open`.

## Store Context-Cancellation Semantics

Семантика отмены контекста (`context.Context`) строго регламентирована для всех бэкендов (`MemoryStore`, `FileStore`, `PebbleStore`, `FlowStore`):

1. **Fail-Fast до начала операций:** если контекст отменён или истёк дедлайн до вызова метода хранилища, операция немедленно возвращает `ctx.Err()` (`context.Canceled` или `context.DeadlineExceeded`) без захвата блокировок, изменения внутреннего состояния и без вызова мутационных коллбеков.
2. **Отмена при ожидании блокировок:** при ожидании освобождения файловой блокировки (`FileStore.withExecutionLock`) или транзакционных очередей отмена контекста немедленно прерывает ожидание с возвратом `ctx.Err()`, не повреждая и не удаляя блокировку активного владельца.
3. **Непрерываемость локального атомарного коммита (Atomic Commit Integrity):** как только транзакционный коммит/запись начата (коммит батча в Pebble, атомарный `fsync + rename` во временный файл в FileStore, фиксация изменений под мьютексом в MemoryStore), операция завершается неделимо до конца. Запрещено прерывать операцию в середине локального коммита, чтобы не оставлять частично записанных или повреждённых данных.

## Следующий reliability-слой

1. Явный distributed ownership/coordination contract для нескольких Engine/processes.
2. Runtime dispatch для policy `catch:`.
3. Operational runbook для stuck leases, failed executions и manual recovery.
4. Wall-clock scheduler для timer triggers.
5. Multi-file AXM resolver/linker.

