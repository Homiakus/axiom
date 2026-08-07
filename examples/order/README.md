# Пример процесса заказа

`order` — runnable-пример для прикладного Go-кода, которому нужны не только переходы состояния, но и production-oriented runtime semantics.

Он показывает рекомендуемый путь для более крупной модели:

```text
Go structs
   ↓
model.Definition
   ↓
axiom.Open(...)
   ↓
Engine
   ↓
Run = engine.Execution(id)
```

## Что показывает пример

1. `model.Bind` и `model.EventOf` описывают state/event schemas из Go-структур.
2. `model.Key` + `StateField` / `EventField` позволяют объявить имена полей один раз и переиспользовать typed references без россыпи строк по правилам.
3. `OrderCreated` создаёт заказ, `PaymentCaptured` переводит его в `paid`.
4. Изменение `Order.status` запускает внешнюю activity `SendReceipt`.
5. Activity зарегистрирована через `axiom.ActTyped`.
6. `retry`, per-attempt `timeout`, `concurrency: once` и обязательная idempotency policy используются вместе с `WithProductionMode()`.
7. Pebble даёт транзакционное durable store; пример создаёт временный каталог и удаляет его при завершении.
8. Claims проверяют, что оплаченный заказ имеет `PaymentID`, а чек не считается отправленным до оплаты.
9. Runtime вызывается через `Run`: `Dispatch` → `State`.

Поле `Total` хранит сумму целым числом в минимальных денежных единицах. В реальном приложении это соглашение лучше закрепить отдельным доменным типом.

## Запуск

Из корня репозитория:

```bash
go run ./examples/order
```

Ожидаемый финал содержит:

```text
send receipt for pay-42
status=paid total=1599 receiptSent=true
```

Порядок строк логирования может отличаться, но итоговое состояние должно быть тем же.

## Что копировать в приложение

Копируйте структуру модели, typed field keys, `ActTyped`, явную обработку ошибок и `Run` API. Не копируйте временный каталог Pebble: production-приложение должно использовать постоянный путь, lifecycle storage и межпроцессную ownership/coordination политику, подходящую его архитектуре.
