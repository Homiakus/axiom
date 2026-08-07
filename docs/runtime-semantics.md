# Runtime-семантика и границы гарантий

Этот документ отделяет реализованное поведение от parsed configuration и планируемых возможностей.

## Статусы подтверждения

- **Подтверждено кодом** — поведение реализовано в текущем runtime path и/или покрыто tests.
- **Частично реализовано** — model/compiler поддерживают параметр, но runtime semantics неполны.
- **Требует уточнения** — public surface существует, но стабильность или intended contract не зафиксированы.

## Execution

| Утверждение | Статус |
|---|---|
| Один execution идентифицируется строковым ID | Подтверждено кодом |
| Операции одного ID сериализуются внутри одного Engine | Подтверждено кодом |
| Разные IDs могут выполняться параллельно | Подтверждено кодом |
| Блокировка действует между процессами | Не реализовано |
| `Dispatch` автоматически создаёт отсутствующий execution | Подтверждено кодом |
| Production mode требует transactional store | Подтверждено кодом и test |
| Production mode отклоняет `retry`, `timeout`, `concurrency`, пока они не являются полными runtime-гарантиями | Подтверждено кодом и test (`AX508`) |

## Activity

| Утверждение | Статус |
|---|---|
| External activity должна быть зарегистрирована в Go | Подтверждено кодом |
| External activity требует idempotency policy и key | Подтверждено compiler validation |
| `ActTyped` отклоняет scalar input/output и nil handler при создании Engine | Подтверждено кодом и test (`AX507`) |
| `ActTyped` принимает structs, pointers to structs и maps со string keys | Подтверждено кодом |
| Tasks хранят input, status, attempts, lease, result/error | Подтверждено кодом |
| Completed task не выполняется повторно при replay/recovery | Подтверждено test |
| Failed handler автоматически повторяется по `retry` | Не реализовано в проверенном path |
| Handler автоматически отменяется по `timeout` policy | Не реализовано в проверенном path |
| `concurrency` policy управляет worker scheduling | Не реализовано в проверенном path |
| Expired running lease может быть возвращён в pending | Подтверждено кодом |

### Что означает production guardrail

Парсер и компилятор продолжают принимать `retry`, `timeout` и `concurrency`: это сохраняет формат модели и позволяет развивать runtime без миграции DSL. Но `WithProductionMode()` теперь fail-fast отклоняет план с такими полями, потому что production-конфигурация не должна выглядеть более надёжной, чем фактическое исполнение.

В development/test режиме эти поля по-прежнему можно компилировать для моделирования, tooling и будущей совместимости, однако нельзя строить correctness assumptions на их автоматическом исполнении.

## Idempotency

Task deduplication ищет task по execution, rule, activity и idempotency key. Незавершённая или completed task не планируется повторно. Failed task не блокирует новое планирование.

Это локальная гарантия store/runtime, а не гарантия exactly once для сети, платёжной системы или оборудования. Activity handler должен передавать тот же key внешней системе или реализовать собственный deduplication record.

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

Не добавляйте в reference список events, которых нет в runtime code или tests.

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

## Что требуется реализовать до усиления обещаний документации

1. Реальный retry loop с backoff/NextAttemptAt и history entries.
2. Timeout context для activity handler.
3. Явная semantics `concurrency` policy.
4. Tests для terminal/retryable errors и exhausted attempts.
5. После реализации убрать соответствующие `AX508` production-блокировки и заменить их тестами фактической гарантии.
6. Operational runbook для stuck leases, failed executions и manual recovery.
