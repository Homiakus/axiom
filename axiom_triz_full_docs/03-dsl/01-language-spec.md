# TRIZ DSL Language Specification

Это целевой user-facing язык. Текущий исполняемый `.axm` описан в
`axiom-main/docs/axiom-file-specification.md`; TRIZ DSL должен нормализоваться в
него или в тот же IR.

## File structure

Канонический порядок:

```text
system
state
event
condition
profile
function
rule
always
view
```

## `system`

```axiom
system HydroPilot
```

Один файл описывает одну модель.

## `state`

```axiom
state System:
  running: Bool = false
  mode: Text = "manual"
```

State fields имеют тип и опциональный default. State меняется только через
`then`.

## `event`

```axiom
event LightRequested(zone: Int, enabled: Bool, level: Int)
event EmergencyStopPressed(reason: Text)
```

Event payload доступен как `event.zone`, `event.reason`.

## `condition`

```axiom
condition CanDose:
  System.running
  Safety.estop == false
  Zone[event.zone].waterLevel != "low"
```

Condition содержит только чистые выражения. Несколько строк означают `and`.

## `profile`

```axiom
profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent
  audited
```

Profile применяется в `do profileName:`.

## `function`

```axiom
function MeasurePH(zone: Int) -> { value: Float, status: Text }
function TurnOffAllActuators() -> { ok: Bool }
```

Function объявляет Go-контракт. Runtime сам управляет вызовом через activity.

## `rule`

```axiom
rule TurnLightOn when:
  LightRequested
  CanUseHardware
do:
  result = SetLight(event.zone, event.enabled)
then:
  set Zone[event.zone].lightOn = result.ok
```

Rule без `do` допустим:

```axiom
rule EmergencyStop when:
  EmergencyStopPressed
then:
  set Safety.estop = true
  set Safety.alarm = event.reason
```

## `always`

```axiom
always NoPumpDryRun:
  Zone[zone].waterLevel == "low" implies Zone[zone].pumpOn == false
```

`always` должен быть чистым выражением без function calls.

## `view`

```axiom
view Dashboard:
  mode = System.mode
  alarm = Safety.alarm
```

View read-only. Он не вызывает function и не меняет state.

## Expressions

Минимальный набор:

```text
== != > >= < <=
and or not implies
in
exists
missing(...)
changed(...)
timer(...)
hash(...)
```

Ограничения:

- `changed(...)` используется только как trigger;
- `timer(...)` используется только как trigger;
- function call разрешён только в `do`;
- writes разрешены только в `then`.

## Reserved words

```text
system state event condition profile function rule when do then set always view
true false null and or not implies in exists missing changed timer hash
```
