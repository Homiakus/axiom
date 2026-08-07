# Declarative Go model

Это **рекомендуемый старт для новых Go-приложений** на Axiom.

Пример показывает Go-native declarative model с typed state/events, reusable field keys, rules, claims, activity policy и typed activity implementation.

```text
Go structs
   ↓
model.Definition
   ↓ Compile
axiom.Plan
   ↓ New/Open
Engine
   ↓ Execution(id)
Run
```

## Запуск

```bash
go run ./examples/model
```

## Что здесь считается рекомендуемым стилем

- `model.Bind[T]` для state schema.
- `model.EventOf[T]` для event schema.
- `model.Key[Owner, Value]` для полей, которые используются многократно.
- `model.StateField` / `model.EventField` для typed expressions без повторения строковых имён по всей модели.
- строгие helpers `Equal`, `GreaterOrEqual`, `PlusField` и т. п. вместо legacy operators с `any`, когда известен тип операндов.
- `axiom.ActTyped` для application activities.
- `axiom.Open(definition, ...)` как короткий путь `Definition → Plan → Engine`.
- `engine.Execution(id)` как основной runtime handle.
- явная обработка всех ошибок.

## Field keys

Строки полностью убрать из reflection-based schema невозможно без code generation, но их можно локализовать в одном месте:

```go
var (
    userEmail = model.Key[User, string]("Email")
    userSent  = model.Key[User, bool]("WelcomeSent")
)

email := model.StateField(user, userEmail)
sent := model.StateField(user, userSent)
```

Owner type не позволяет случайно применить ключ `User` к другому state/event type. Value type проверяется при использовании ключа; для optional pointer field разрешён его logical pointed-to type.

Для маленьких моделей `user.String("Email")` остаётся нормальным коротким API. `FieldKey` предназначен прежде всего для средних и больших моделей, где одно поле встречается во многих rules/claims/activities.
