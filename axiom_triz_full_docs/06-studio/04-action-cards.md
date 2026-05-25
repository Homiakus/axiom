# Action Cards

## Purpose

Action Cards show how rules connect to user code.

## Fields

```text
Action name
Description
Called by rules
Input mapping
Output schema
Output fields used
Profile
Idempotency
Timeout/retry
Go signature
Implementation status
Safety risks
```

## Example

```text
Action: DosePHDown

Called by:
  DosePHDown
  ManualDosePHDown

Input:
  zone = event.zone
  amount = Solution.phDoseAmount

Output:
  ok: Bool
  actualAmount: Float

Writes after action:
  Dosing.active = false
  Solution.needPHDown = false

Profile:
  critical

Safety:
  Requires CanDose
  Protected by NoDosingDuringEstop
```

## Diagnostics

```text
Action has no profile but looks external.
Action output is unused.
Action can be called by rule without safety condition.
Action name not implemented in Go.
Action has ambiguous idempotency key.
```
