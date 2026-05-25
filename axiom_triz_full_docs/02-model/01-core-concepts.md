# Core Concepts

TRIZ-слой оставляет на поверхности только то, что нужно для описания поведения.
Все runtime-детали остаются в normalizer и Axiom v0.

## `system`

Имя модели. В Axiom v0 соответствует `domain`.

```axiom
system HydroPilot
```

## `state`

Durable память. Меняется только в `then`.

```axiom
state Safety:
  estop: Bool = false
  alarm: Text = "none"
```

## `event`

То, что пришло извне или из scheduler/runtime. В Axiom v0 соответствует `signal`.

```axiom
event EmergencyStopPressed(reason: Text)
event MeasurementDue(zone: Int)
```

## `condition`

Именованное чистое условие. Не вызывает функции и не пишет state.

```axiom
condition CanUseHardware:
  System.running
  Safety.estop == false
```

Normalizer решает, станет ли это `computed`, `fact` или комбинацией.

## `profile`

Политика исполнения function: timeout, retry, idempotency, audit.

```axiom
profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent
  audited
```

## `function`

Go-функция с явным input/output контрактом. В runtime это managed `activity`.

```axiom
function SetLight(zone: Int, on: Bool) -> { ok: Bool }
```

## `rule`

Единица поведения:

```axiom
rule TurnLightOn when:
  LightOnDue
  CanUseHardware
do:
  result = SetLight(event.zone, true)
then:
  set Zone[event.zone].lightOn = result.ok
```

`when` выбирает момент и условия, `do` вызывает function, `then` пишет state.
Rule без `do` разрешён для чистых state writes.

## `always`

Инвариант безопасности. В Axiom v0 соответствует `claim`.

```axiom
always NoDosingDuringEstop:
  Safety.estop == true implies Dosing.active == false
```

## `view`

Read-only projection для UI/API. В Axiom v0 соответствует `query`.

```axiom
view Dashboard:
  alarm = Safety.alarm
  light = Zone[1].lightOn
```
