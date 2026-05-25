# State, Events, Rules

## State design

State должен описывать факты системы, а не шаги workflow.

Хорошо:

```axiom
state Payment:
  status: Text = "waiting"
  paidCount: Int = 0
```

Плохо:

```axiom
state Flow:
  currentStep: Text = "charge_card"
```

Шаги должны возникать из правил и зависимостей, а не храниться вручную.

## Events

Event фиксирует вход. Он не обязан описывать действие.

```axiom
event OperatorLightRequested(zone: Int, enabled: Bool, level: Int)
event UartCommandCompleted(zone: Int, status: Text, responseRaw: Text)
```

Event payload читается как `event.field` только в rules, которые реагируют на
этот event.

## Rules

Правило читается сверху вниз:

```axiom
rule Name when:
  trigger-or-condition
  condition
do optional-profile:
  result = Function(...)
then:
  set State.field = value
```

Минимальные правила:

- `when` должен содержать event, change/timer trigger или условие;
- `do` опционален;
- `then` пишет только state;
- function не меняет state напрямую.

## Triggers

| Trigger | Когда использовать |
|---|---|
| `EventName` | вход пришёл извне |
| `changed(State.field)` | правило зависит от изменения поля |
| `timer(24h after State.createdAt)` | отложенная реакция |
| named condition | поведение следует из состояния |

## Writes

Все изменения state должны быть видны в `then`:

```axiom
then:
  set Command.busy = false
  set Command.lastStatus = result.status
```

Так runtime может построить history, replay и why/why-not.
