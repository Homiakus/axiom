# HydroPilot Safety

HydroPilot controls physical devices, so safety must be modeled explicitly.

## Core laws

```axiom
always NoDosingDuringEstop:
  Safety.estop == true implies Dosing.active == false

always NoOppositePHDosing:
  not (Dosing.phUpActive and Dosing.phDownActive)

always NoPumpDryRun:
  Zone[zone].waterLevel == "low" implies Zone[zone].pumpOn == false

always NoLightDuringEstop:
  Safety.estop == true implies all Zone.lightOn == false

always NoDosingWithoutWater:
  Zone[zone].waterLevel == "low" implies Dosing.active == false
```

## Safety scenarios

| Scenario | Required behavior |
|---|---|
| emergency stop | latch safety state, turn off actuators, block hardware actions |
| low water | block dosing/pump, allow fill, notify operator |
| sensor error | mark data invalid, block dosing from stale data |
| calibration expired | warn operator, block automatic dosing if needed |
| UART failure | stop command chain, notify, require explicit recovery |

## Studio checks

Studio should show:

- all paths from event to actuator;
- each guard condition on that path;
- each `always` law covering the path;
- whether the action uses `critical` profile;
- what happens when estop is active.
