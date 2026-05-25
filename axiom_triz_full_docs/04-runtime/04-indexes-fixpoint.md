# Dependency Indexes and Fixpoint

Indexes let runtime react to changed state without scanning every rule.

## Core indexes

| Index | Used for |
|---|---|
| state field -> computed/condition | recompute derived values |
| state field -> rule | trigger `changed(...)` rules |
| event -> rule | route event input |
| timer -> rule | wake delayed rules |
| function -> rule | apply output writes |
| state field -> always | check affected invariants |

## Example

```text
Zone.ph changed
  -> phHigh recomputed
  -> DoseNeeded recomputed
  -> PlanDose rule checked
  -> NoUnsafeDosing checked
```

## Fixpoint algorithm

```text
queue affected rules
while queue not empty:
  evaluate next rule
  if blocked: record reason
  if runnable without function: apply writes and enqueue dependents
  if runnable with function: schedule activity
stop when no immediate work remains
```

## Loop detection

A model is suspicious when a rule writes a field that immediately retriggers the
same rule without a converging guard. Validator should catch obvious cases;
runtime still needs a max-iteration guard.
