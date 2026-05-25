# DSL Style Guide

## Naming

| Item | Style | Example |
|---|---|---|
| system | PascalCase | `HydroPilot` |
| state | PascalCase | `Safety` |
| state field | lowerCamelCase | `estopActive` |
| event | PascalCase | `LightRequested` |
| condition | PascalCase | `CanUseHardware` |
| profile | lowerCamelCase | `critical` |
| function | PascalCase | `SendUartRequest` |
| rule | PascalCase or lowerCamelCase | `DosePHDown` |
| always | PascalCase | `NoPumpDryRun` |
| view | PascalCase | `Dashboard` |

## Rules

Rule должен помещаться в один экран. Если `when` длинный, вынеси часть в
`condition`. Если `then` длинный, проверь, не смешаны ли две ответственности.

## Conditions

Condition должна быть чистой и называться как вопрос с ответом yes/no:

```axiom
condition CanDose:
  CanUseHardware
  Zone[event.zone].waterLevel != "low"
```

## Functions

Function делает одну внешнюю или локальную работу:

```axiom
function BuildLightingUartRequest(zone: Int, enabled: Bool, level: Int)
  -> { commandName: Text, payload: Text }
```

Не прячь state mutation в Go-код.

## Profiles

Используй несколько понятных profiles вместо inline-настроек в каждом rule:

```axiom
profile critical
profile externalCall
profile localCalculation
```

## Comments

Комментарий нужен для доменной причины, а не для пересказа кода:

```axiom
# pH correction is blocked on low water to avoid overdosing.
```
