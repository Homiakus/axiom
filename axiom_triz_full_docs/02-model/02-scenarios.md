# Scenarios

## Что такое scenario

Scenario — человеческая группировка правил вокруг цели.

Пример HydroPilot:

```text
Measurement
Solution Control
Light Control
Water Control
Safety
Calibration
Notification
Manual Control
```

Scenario не обязан быть runtime-сущностью. Это слой UX и документации.

## Scenario состоит из

```text
цель
events
conditions
rules
functions
state writes
always laws
diagnostics
tests
```

## Пример: Measure pH

```text
Goal:
  получить актуальное значение pH зоны.

Starts:
  PHMeasurementDue
  ManualMeasurementRequested(kind = "ph")

Allowed:
  CanMeasure
  sensor ready

Action:
  MeasurePH(zone)

Writes:
  Zone[zone].ph
  Zone[zone].phStatus
  Zone[zone].phMeasuredAt

May trigger:
  CheckSolution
  NotifyPHOutOfRange
```

## Пример: Light schedule

```text
Goal:
  включать/выключать свет по расписанию.

Events:
  LightOnDue
  LightOffDue

Rules:
  TurnLightOnBySchedule
  TurnLightOffBySchedule

Safety:
  disabled during emergency stop
```

## Пример: Solution correction

```text
Goal:
  поддерживать раствор в диапазоне.

Events:
  SolutionCheckDue
  measurement results changed

Rules:
  CheckSolution
  DosePHUp
  DosePHDown
  DoseNutrient
  DiluteSolution

Safety:
  no dosing during estop
  no opposite dosing
  no dosing without water
```
