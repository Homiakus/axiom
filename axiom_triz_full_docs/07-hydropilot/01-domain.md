# HydroPilot Domain

HydroPilot controls a hydroponic installation with measurements, dosing,
lighting, safety interlocks and operator commands.

## In the current project

The implemented example is:

```text
axiom-main/examples/hydropilot/hydropilot.axm
```

It uses Axiom v0 terms and a parameterized model: zones are represented by
`zoneId` fields instead of duplicated `Zone1..Zone4` contexts.

## In the TRIZ docs

`04-example-ideal-axm.md` shows a smaller user-facing version. It keeps the same
idea, but reads as behavior:

```text
measure -> store -> plan dose -> send command -> record result
```

## Main subsystems

| Subsystem | Role |
|---|---|
| Sensors | pH, TDS/EC, temperature, water level |
| Actuators | lights, pumps, dosing pumps, fill/drain, siren |
| Control | schedules, measurement cycles, solution correction |
| Safety | emergency stop, low water, stale sensor data |
| Operator | manual mode, service commands, calibration |
| Persistence | measurement history and audit |

## Main state groups

```text
System
Safety
Zone or parameterized zone state
Measurement
Solution / Dose
Lighting
Command
Calibration
Notification
Runtime
```

## Main events

```text
SchedulerTicked
MeasurementDue
SolutionCheckDue
LightRequested
ManualCommandRequested
EmergencyStopPressed
CalibrationRequested
UartCommandCompleted
SensorError
```
