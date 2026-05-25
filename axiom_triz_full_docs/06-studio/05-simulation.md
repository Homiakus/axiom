# Studio Simulation

## Simulation panel

Inputs:

```text
event name
event payload
state assumptions
mock function outputs
max steps
```

Outputs:

```text
timeline
runnable/blocked rules
action calls
writes
final state
always results
unknown expressions
```

## Example

Given:

```text
Safety.estop = false
System.running = true
Zone[1].ph = 7.4
Zone[1].waterLevel = normal
```

When:

```text
SolutionCheckDue(zone=1)
```

Timeline:

```text
Event SolutionCheckDue
Rule CheckSolution RUNNABLE
Action CheckSolution scheduled
Mock output needPHDown=true
Write Solution.needPHDown=true
Rule DosePHDown RUNNABLE
Action DosePHDown scheduled
Write Solution.needPHDown=false
Always NoOppositeDosing PASS
Fixpoint
```

## Unknowns

If simulator cannot evaluate:

```text
UNKNOWN expression: all Zone.lightOn == false
```

It must show unknown, not guess.

## Mock outputs

User can enter:

```json
{
  "MeasurePH": {"value": 7.4, "status": "ok"},
  "CheckSolution": {"needPHDown": true}
}
```

## Step mode

Buttons:

```text
Run all
Step
Reset
Apply event
Apply mock result
```
