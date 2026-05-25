package main

const sampleSource = `system HydroPilot

state System:
  ready: Bool = true
  mode: Text = "auto"

state Safety:
  estop: Bool = false
  locked: Bool = false

state Zone[1..4]:
  ph: Float?
  tds: Float?
  temperature: Float?
  waterLevel: Text = "unknown"

state Light:
  on: Bool = false

state Doser:
  active: Bool = false

event PHMeasurementDue(zone: Int)
event LightOnDue
event EmergencyStopPressed

condition CanUseHardware:
  System.ready
  Safety.estop == false
  Safety.locked == false

rule MeasurePH on PHMeasurementDue:
  CanUseHardware
  do:
    result = MeasurePH(zone)
  then:
    set Zone[zone].ph = result.value
    set Zone[zone].phMeasuredAt = now

rule TurnLightOn on LightOnDue:
  CanUseHardware
  Light.on == false
  do:
    result = SetLight(on: true)
  then:
    set Light.on = true

rule EmergencyStop on EmergencyStopPressed:
  do:
    result = TurnOffAllActuators()
  then:
    set Safety.estop = true
    set Light.on = false
    set Doser.active = false

always emergencyStopDisablesActuators:
  Safety.estop == true implies Light.on == false
  Safety.estop == true implies Doser.active == false
`
