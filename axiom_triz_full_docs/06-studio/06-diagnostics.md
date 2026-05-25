# Diagnostics

Diagnostics should be readable by humans and stable for tools.

## Families

| Family | Examples |
|---|---|
| syntax | unknown keyword, bad indentation, unclosed block |
| symbols | unknown state/event/condition/function |
| types | wrong argument, wrong result field, invalid comparison |
| rules | no trigger, write to non-state field, output used without function |
| safety | actuator without guard, possible estop bypass |
| runtime | non-convergent loop, broad dependency, missing listener |
| TRIZ style | duplicate condition, overloaded function, workflow-step smell |

## Format

```json
{
  "code": "AXT501",
  "severity": "error",
  "message": "DosePHDown can run during emergency stop.",
  "source": {"file": "hydro.axm", "line": 120},
  "suggestion": "Add CanDose to the rule or protect the path with an always law."
}
```

## Studio behavior

Diagnostics should link to:

- source line;
- affected rule/function/state;
- normalized runtime entity;
- suggested fix when safe to suggest.

Never hide unknowns. If simulator cannot evaluate an expression, show `UNKNOWN`
instead of guessing.
