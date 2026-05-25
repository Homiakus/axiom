# Axiom Files — примеры .axm DSL

Здесь лежат `.axm` файлы, которые используются как документационные примеры и
как тестовые входные данные для компилятора и runtime библиотеки.

## Файлы

| Файл | Domain | Описание | Что демонстрирует |
|------|--------|---------|-------------------|
| `welcome.axm` | Welcome | Минимальный welcome-flow | signal, context, computed, fact, policy, activity, rule, claim. Две rules: captureRegistration (on UserRegistered) и sendWelcomeEmail (on changed(User.email) с require RegisteredUser) |
| `checkout.axm` | Checkout | Полноценный checkout-flow | Множественные activity (CheckInventory, CalculateRisk, ChargeCard), факты CanCheckout/CanPayByCard, claims paymentHasId/noDoublePayment, policies externalCall/paymentCritical |
| `claims.axm` | Claims | Проверка инвариантов | Различные виды claim-условий: always, implies, exists, сравнения |
| `reminder.axm` | Reminder | Сценарий напоминания | Timer-based rule: `on timer(24h after Order.createdAt)`, повторная отправка с проверкой состояния |

## Использование в тестах

Файлы автоматически проверяются тестом `pkg/axiom/example_files_test.go` —
каждый `.axm` компилируется, чтобы примеры не расходились с реальным компилятором.

## Генерация Go-обвязки

Для любого из этих файлов можно сгенерировать типизированную Go-обвязку:

```bash
go run ./tools/axiomgen --file examples/axiom-files/welcome.axm
```

Подробнее — в [документации axiomgen](../../docs/axiomgen.md).
