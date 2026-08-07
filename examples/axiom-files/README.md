# Примеры AXM

Этот каталог показывает внешний AXM frontend: process definition хранится отдельно от Go-кода, но компилируется в тот же `axiom.Plan` и использует тот же runtime API.

```text
.axm file
   ↓
axm.Load / axm.Parse
   ↓
axiom.Plan
   ↓
plan.New(...)
   ↓
Run
```

## Быстрый запуск

Из корня репозитория:

```bash
go run ./examples/axiom-files
```

`main.go` загружает `welcome.axm`, привязывает внешний `SendWelcomeEmail` через `ActTyped`, отправляет `UserRegistered` и читает итоговое состояние через `Run.State`.

## Файлы

| Файл | Domain | Назначение |
|---|---|---|
| `welcome.axm` | `Welcome` | основной runnable walkthrough: signal, context, computed, fact, policy, activity, rules и claim |
| `checkout.axm` | `Checkout` | несколько activities, facts, claims и policies |
| `claims.axm` | `Claims` | инварианты и expression forms |
| `reminder.axm` | `Reminder` | timer trigger и повторный сценарий на уровне модели |

Все `.axm` модели дополнительно компилируются тестом `examples_test.go`.

## Когда выбирать AXM

Используйте AXM, когда definition должна:

- храниться отдельно от Go binary;
- редактироваться tooling/Studio;
- поставляться или версионироваться независимо от implementation activities;
- анализироваться без загрузки application package.

Если definition является обычной частью Go-приложения и отдельный файл не нужен, `model` обычно удобнее и даёт лучший refactoring/DX.

## Генерация Go activity boundary

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

`axiomgen` не имеет интерактивного режима: он печатает JSON report и создаёт/обновляет файлы в `--out`. Подробнее: [документация axiomgen](../../docs/axiomgen.md).

## Проверка

```bash
go test ./...
go run ./examples/axiom-files
```

При изменении AXM syntax сначала добавляйте parser/compiler/runtime coverage, затем обновляйте example и только после подтверждения фактической поддержки — specification docs.
