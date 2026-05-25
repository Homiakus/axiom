# Validation

## Validation stages

1. Lexical.
2. Parse.
3. Symbol resolution.
4. Type checking.
5. Scope checking.
6. Effect safety.
7. Profile safety.
8. Always law feasibility.
9. Dependency cycles.
10. Runtime convergence.

## Error code families

```text
AXT001 syntax
AXT100 symbols
AXT200 types
AXT300 effects
AXT400 rules
AXT500 safety
AXT600 runtime/convergence
AXT700 style/TRIZ suggestions
```

## Examples

```text
AXT101 unresolved reference: CanDose
AXT201 type mismatch: expected Float, got Text
AXT301 function call in condition is not pure
AXT401 rule has then block but no trigger/condition
AXT501 actuator action without safety condition
AXT601 possible non-convergent rule loop
AXT701 duplicate condition can be trimmed
```

## Severity

```text
error
warning
suggestion
info
```

## Validator output

Must be machine-readable and human-readable.

```json
{
  "code": "AXT501",
  "severity": "error",
  "message": "DosePHDown can run during emergency stop.",
  "line": 120,
  "suggestion": "Add CanDose condition."
}
```
