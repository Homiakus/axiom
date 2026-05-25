# Glossary

| Термин | Runtime-эквивалент | Кратко |
|---|---|---|
| Action | Activity | Управляемый вызов Go-функции. |
| Always | Claim | Инвариант, который runtime проверяет до и после опасных изменений. |
| Condition | Computed / Fact | Именованное чистое условие. |
| CRFG | Internal graph | Контекстно-реактивный граф зависимостей. |
| Event | Signal | Вход в систему: оператор, scheduler, устройство, callback. |
| Function | Activity handler | Go-код с контрактом input/output. |
| History | Event log | Append-only запись входов, решений, action results и writes. |
| Normalizer | Compiler layer | Переводит TRIZ DSL в Axiom v0 / IR. |
| Profile | Policy | Retry, timeout, idempotency, audit, concurrency. |
| Rule | Rule | Поведение в форме `when -> do -> then`. |
| Scenario | Grouping | Человеческая группировка связанных rules. |
| State | Context | Durable память исполнения. |
| Trimming | Simplification | Удаление runtime-терминов с пользовательской поверхности. |
| View | Query | Read-only projection для UI/API. |
| Why / why-not | Explainability | Ответ runtime, почему rule сработал или был заблокирован. |
