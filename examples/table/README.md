# TOML decision table

Этот пример показывает внешний сериализованный frontend: процесс хранится в `welcome.toml`, компилируется в тот же `axiom.Plan`, а runtime и Go activities остаются обычными.

```text
welcome.toml
    ↓
table.Load
    ↓
axiom.Plan
    ↓
plan.New(...)
    ↓
Run
```

Выбирайте `table` не потому, что TOML короче Go, а когда модель действительно удобнее редактировать как decision table или хранить отдельно от бинарника приложения.

## Запуск

Из корня репозитория:

```bash
go run ./examples/table
```

`main.go` загружает `welcome.toml`, регистрирует `SendWelcomeEmail` через `ActTyped`, отправляет `UserRegistered` и читает итоговое состояние через `Run.State`.

## Что важно

- TOML decoder отклоняет неизвестные поля конфигурации.
- Ошибки parsing/compilation возвращаются до создания runtime.
- Activity implementation не хранится в TOML: внешний эффект привязывается в Go.
- `table.Load` возвращает canonical `*axiom.Plan`, поэтому дальше используется тот же runtime API, что и у Go model/AXM.
- Для production storage и runtime safeguards добавьте `axiom.WithStore(...)` и `axiom.WithProductionMode()` при `plan.New(...)`.
