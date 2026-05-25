# DSL Patterns

## Measurement pattern

```axiom
rule MeasureX when:
  XMeasurementDue
  CanMeasure
do:
  result = MeasureX(zone)
then:
  set Zone[zone].x = result.value
  set Zone[zone].xStatus = result.status
```

## Check-and-act pattern

```axiom
rule CheckSolution when:
  SolutionCheckDue
  MeasurementsFresh
do:
  result = CheckSolution(...)
then:
  set Solution.needAction = result.needAction

rule ApplyAction when:
  Solution.needAction == "dose_ph_down"
  CanDose
do critical:
  result = DosePHDown(...)
then:
  set Solution.needAction = "none"
```

## Emergency stop pattern

```axiom
rule EmergencyStop when:
  EmergencyStopPressed
then:
  set Safety.estop = true
  set Safety.alarm = "emergency_stop"

rule DisableActuators when:
  Safety.estop == true
do critical:
  result = TurnOffAllActuators()
then:
  set Actuators.allOff = true

always NoActuatorDuringEstop:
  Safety.estop == true implies Actuators.allOff == true
```

## Schedule pattern

```axiom
event LightOnDue(zone: Int)
event LightOffDue(zone: Int)

rule LightOn when:
  LightOnDue
  CanUseHardware
do:
  result = SetLight(event.zone, true)
then:
  set Zone[event.zone].lightOn = true
```

## Manual override pattern

```axiom
rule ManualLight when:
  LightRequested
  CanUseHardware
do:
  result = SetLight(event.zone, event.on)
then:
  set Zone[event.zone].lightOn = event.on
```

## Notification pattern

```axiom
rule NotifyAlarm when:
  Safety.alarm != "none"
  Notification.alarmSent == false
do:
  result = SendNotification(Safety.alarm)
then:
  set Notification.alarmSent = result.sent
```
