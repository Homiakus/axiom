# Примеры

- `axiom-files/` — чистые `.axm` примеры для компилятора и runtime:
  - `welcome.axm` — минимальный welcome-flow: signal UserRegistered, rule captureRegistration + sendWelcomeEmail, activity SendWelcomeEmail
  - `checkout.axm` — полноценный checkout-flow: CheckInventory, CalculateRisk, ChargeCard с фактами CanCheckout/CanPayByCard и claims paymentHasId/noDoublePayment
  - `claims.axm` — пример claim/invariant проверки: always-условия, защита от нарушения инвариантов
  - `reminder.axm` — пример сценария с таймером: напоминание через 24h после создания заказа

- `control-panel/` — отдельная интерактивная TUI-программа (Bubble Tea) для
  управления Axiom execution: запуск, отправка сигналов, просмотр состояния и
  истории. Держит собственный `go.mod` с UI-зависимостями, чтобы корневой модуль
  Axiom оставался чистой библиотекой.

- `hydropilot/` — сгенерированная axiomgen'ом типизированная обвязка для
  hydropilot.axm. Содержит `hydro_pilot_axiom.gen.go` (DO NOT EDIT) и
  `hydro_pilot_activities.go` (реализации activity).

Примеры не являются частью публичного API библиотеки.
