# Anti-patterns

## 1. Workflow step state

Плохо:

```axiom
state Flow:
  step: Text = "measure"
```

Лучше: хранить факты системы и дать rules вывести следующий шаг.

## 2. Side effect in condition

Плохо:

```axiom
condition CanMeasure:
  PingDevice()
```

Condition должна быть чистой. Ping — это function/rule.

## 3. Function mutates state

Плохо: Go-функция сама меняет `Safety.estop`.

Лучше: function возвращает output, `then` пишет state.

## 4. Unsafe actuator rule

Плохо:

```axiom
rule PumpOn when:
  PumpRequested
do critical:
  result = SetPump(true)
then:
  set Pump.on = result.ok
```

Лучше:

```axiom
rule PumpOn when:
  PumpRequested
  CanUseHardware
  WaterLevelOk
do critical:
  result = SetPump(true)
then:
  set Pump.on = result.ok
```

## 5. Duplicate conditions

Если одинаковые проверки повторяются в rules, вынеси их в named condition.

## 6. Missing `always`

Любой actuator path должен иметь не только guard в rule, но и invariant:

```axiom
always NoPumpDryRun:
  Water.level == "low" implies Pump.on == false
```

## 7. Overloaded function

Плохо:

```axiom
function HandleEverything(command: Text) -> { ok: Bool }
```

Лучше: маленькие functions с явными input/output. Runtime тогда может объяснить,
что именно было вызвано и почему.
