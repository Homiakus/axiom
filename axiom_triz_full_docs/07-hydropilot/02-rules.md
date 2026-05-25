# HydroPilot Rules

HydroPilot rules should read as independent behavior, not as a single workflow.

## Measurement

```axiom
rule MeasurePH when:
  PHMeasurementDue
  CanMeasure
do:
  result = MeasurePH(event.zone)
then:
  set Zone[event.zone].ph = result.value
  set Zone[event.zone].phStatus = result.status
```

Same pattern applies to TDS, temperature and water level.

## Solution check

```axiom
rule CheckSolution when:
  SolutionCheckDue
  MeasurementsFresh
do:
  result = CheckSolution(
    ph: Zone[event.zone].ph,
    tds: Zone[event.zone].tds,
    temperature: Zone[event.zone].temperature
  )
then:
  set Solution.status = result.status
  set Solution.needPHUp = result.needPHUp
  set Solution.needPHDown = result.needPHDown
  set Solution.needNutrient = result.needNutrient
  set Solution.needDilution = result.needDilution
```

## Dosing

```axiom
rule DosePHDown when:
  Solution.needPHDown
  CanDose
do critical:
  result = DosePHDown(event.zone, Solution.phDoseAmount)
then:
  set Solution.needPHDown = false
  set Dosing.active = false
```

Critical dosing must be protected by `CanDose` and by `always` laws.

## Lighting

```axiom
rule LightOnByRequest when:
  LightRequested
  CanUseHardware
do:
  result = SetLight(event.zone, event.enabled)
then:
  set Zone[event.zone].lightOn = result.ok
```

## Emergency stop

```axiom
rule EmergencyStop when:
  EmergencyStopPressed
then:
  set Safety.estop = true
  set Safety.alarm = event.reason

rule TurnOffAllActuatorsOnEstop when:
  Safety.estop == true
do critical:
  result = TurnOffAllActuators()
then:
  set Actuators.allOff = result.ok
```

## Notifications

```axiom
rule NotifyAlarm when:
  Safety.alarm != "none"
  Notification.alarmSent == false
do:
  result = SendNotification(Safety.alarm)
then:
  set Notification.alarmSent = result.sent
```

Notification rules should be idempotent to avoid repeated alerts.
