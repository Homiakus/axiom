# axiom-demo

Демо-тест программа для проверки публичного API библиотеки Axiom.

Она намеренно лежит отдельно от `pkg/axiom`: это пример внешнего приложения,
которое импортирует `axiom/pkg/axiom`, загружает `.axm` файл, создаёт runtime
engine и выполняет несколько сценариев.

## Использование

```bash
# Валидация .axm файла (проверка компиляции и вывод сводки)
go run ./cmd/axiom validate examples/axiom-files/welcome.axm

# Запуск welcome-сценария (UserRegistered → SendWelcomeEmail)
go run ./cmd/axiom run-welcome examples/axiom-files/welcome.axm

# Запуск checkout-сценария (CheckoutRequested → CheckInventory + CalculateRisk
# → CheckoutConfirmed → ChargeCard)
go run ./cmd/axiom run-checkout examples/axiom-files/checkout.axm
```

## Как это работает

1. `loadModuleFile` — читает `.axm` с диска и компилирует через `axiom.Compile`
2. `runWelcome` / `runCheckout` — создают Engine, регистрируют activity, стартуют
   execution, отправляют сигналы, вызывают `RunUntilIdle` и выводят state + history
3. `summary` — возвращает сводку модуля (количество signals, contexts, activities, rules и т.д.)

Программа не предназначена для production использования — только для демонстрации
API и ручной проверки.
