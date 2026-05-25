# HydroPilot Mini TRIZ AXM

This is a readable target model, not the current Axiom v0 syntax. The full
implemented model is `axiom-main/examples/hydropilot/hydropilot.axm`.

```axiom
system HydroPilotMini

state System:
  running: Bool = false
  mode: Text = "manual"

state Safety:
  estop: Bool = false
  alarm: Text = "none"
  sirenOn: Bool = false

state Zone[1..4]:
  enabled: Bool = true
  ph: Float?
  tds: Float?
  temperature: Float?
  waterLevel: Text = "unknown"
  lightOn: Bool = false
  pumpOn: Bool = false

state Solution:
  zone: Int = 0
  status: Text = "unknown"
  needPHUp: Bool = false
  needPHDown: Bool = false
  needNutrient: Bool = false
  needDilution: Bool = false
  phDoseAmount: Float = 0.0

state Dosing:
  active: Bool = false
  phUpActive: Bool = false
  phDownActive: Bool = false

event PHMeasurementDue(zone: Int)
event TDSMeasurementDue(zone: Int)
event TemperatureMeasurementDue(zone: Int)
event WaterLevelMeasurementDue(zone: Int)
event SolutionCheckDue(zone: Int)
event LightRequested(zone: Int, enabled: Bool, level: Int)
event EmergencyStopPressed(reason: Text)

condition CanUseHardware:
  System.running
  Safety.estop == false

condition CanMeasure:
  CanUseHardware
  Zone[event.zone].enabled

condition CanDose:
  CanUseHardware
  Solution.zone in [1, 2, 3, 4]
  Zone[Solution.zone].waterLevel != "low"

condition MeasurementsFresh:
  Zone[event.zone].ph exists
  Zone[event.zone].tds exists
  Zone[event.zone].temperature exists
  Zone[event.zone].waterLevel != "unknown"

profile local:
  timeout: 1s
  retry: 0
  once

profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent
  audited

function MeasurePH(zone: Int) -> { value: Float, status: Text }
function MeasureTDS(zone: Int) -> { value: Float, status: Text }
function MeasureTemperature(zone: Int) -> { value: Float, status: Text }
function MeasureWaterLevel(zone: Int) -> { level: Text }

function CheckSolution(ph: Float, tds: Float, temperature: Float) -> {
  status: Text
  needPHUp: Bool
  needPHDown: Bool
  needNutrient: Bool
  needDilution: Bool
  phDoseAmount: Float
}

function DosePHDown(zone: Int, amount: Float) -> { ok: Bool }
function SetLight(zone: Int, enabled: Bool, level: Int) -> { ok: Bool }
function TurnOffAllActuators() -> { ok: Bool }
function SendNotification(message: Text) -> { sent: Bool }

rule MeasurePH when:
  PHMeasurementDue
  CanMeasure
do:
  result = MeasurePH(event.zone)
then:
  set Zone[event.zone].ph = result.value

rule MeasureTDS when:
  TDSMeasurementDue
  CanMeasure
do:
  result = MeasureTDS(event.zone)
then:
  set Zone[event.zone].tds = result.value

rule MeasureTemperature when:
  TemperatureMeasurementDue
  CanMeasure
do:
  result = MeasureTemperature(event.zone)
then:
  set Zone[event.zone].temperature = result.value

rule MeasureWaterLevel when:
  WaterLevelMeasurementDue
  CanMeasure
do:
  result = MeasureWaterLevel(event.zone)
then:
  set Zone[event.zone].waterLevel = result.level

rule CheckSolution when:
  SolutionCheckDue
  MeasurementsFresh
do local:
  result = CheckSolution(
    ph: Zone[event.zone].ph,
    tds: Zone[event.zone].tds,
    temperature: Zone[event.zone].temperature
  )
then:
  set Solution.zone = event.zone
  set Solution.status = result.status
  set Solution.needPHUp = result.needPHUp
  set Solution.needPHDown = result.needPHDown
  set Solution.needNutrient = result.needNutrient
  set Solution.needDilution = result.needDilution
  set Solution.phDoseAmount = result.phDoseAmount

rule DosePHDown when:
  Solution.needPHDown
  CanDose
do critical:
  result = DosePHDown(Solution.zone, Solution.phDoseAmount)
then:
  set Dosing.active = false
  set Dosing.phDownActive = false
  set Solution.needPHDown = false

rule ApplyLightRequest when:
  LightRequested
  CanUseHardware
do critical:
  result = SetLight(event.zone, event.enabled, event.level)
then:
  set Zone[event.zone].lightOn = result.ok and event.enabled

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
  set Zone[1].lightOn = false
  set Zone[2].lightOn = false
  set Zone[3].lightOn = false
  set Zone[4].lightOn = false
  set Dosing.active = false
  set Dosing.phUpActive = false
  set Dosing.phDownActive = false

rule NotifyAlarm when:
  Safety.alarm != "none"
do:
  result = SendNotification(Safety.alarm)
then:
  set Safety.sirenOn = result.sent

always NoDosingDuringEstop:
  Safety.estop == true implies Dosing.active == false

always NoOppositePHDosing:
  not (Dosing.phUpActive and Dosing.phDownActive)

always NoPumpDryRun:
  Zone[zone].waterLevel == "low" implies Zone[zone].pumpOn == false

always NoLightDuringEstop:
  Safety.estop == true implies all Zone.lightOn == false

view Dashboard:
  running = System.running
  mode = System.mode
  alarm = Safety.alarm
  zone1PH = Zone[1].ph
  zone1Light = Zone[1].lightOn
```
