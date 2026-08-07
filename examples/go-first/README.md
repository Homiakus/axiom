# Typed Go Flow

`go-first` показывает самый маленький Go-only frontend Axiom: typed reducer + typed effects без declarative model/compiler graph.

Выбирайте `Flow`, когда логика компактна и обычный Go-код важнее статического анализа rules/claims/activities.

```text
Event
  ↓
Handle[S, E]
  ↓
FlowResult[S]
  ├─ new state
  └─ typed effect commands
        ↓
EffectHandler
```

## Запуск

```bash
go run ./examples/go-first
```

Пример:

- регистрирует `Increment` handler;
- изменяет typed `State`;
- создаёт effect command `LogCount`;
- обрабатывает effect отдельным typed handler;
- читает итоговое состояние и history через `FlowExecution`.

## Когда перейти на `model`

Используйте declarative `model` вместо Flow, если нужны статически анализируемые rules, claims, activities, queries, policy metadata, diagram/tooling support или сериализуемое canonical `Plan`.

Важно: Flow effects выполняются до `FlowStore.Save`. Для внешних эффектов обработчик должен быть идемпотентным, потому что ошибка сохранения после выполненного эффекта не может «отменить» уже совершённое внешнее действие.
