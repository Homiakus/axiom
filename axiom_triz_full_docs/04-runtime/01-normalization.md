# Normalization

Normalizer is the boundary between the readable TRIZ DSL and the strict runtime
model already implemented in `axiom-main`.

## Mapping

| TRIZ DSL | Axiom v0 / IR |
|---|---|
| `system` | `domain` |
| `state` | `context` |
| `event` | `signal` |
| `condition` | `computed` and/or `fact` |
| `profile` | `policy` |
| `function` | `activity` |
| `rule when/do/then` | `rule on/when/require/run/write` |
| `always` | `claim` |
| `view` | `query` |

## Stages

1. Parse TRIZ source and keep source ranges.
2. Build symbol table for user terms.
3. Infer triggers and dependencies from `when`.
4. Split `condition` into pure computed values and facts with exposed fields.
5. Expand `function` calls into activities and output bindings.
6. Expand profiles into policies.
7. Convert `then` writes into context write mappings.
8. Generate dependency indexes.
9. Emit Axiom v0 module or normalized IR.

## Source mapping

Every generated runtime entity must keep a pointer back to the user source:

```text
rule DosePHDown -> activity DosePHDown -> policy critical -> source lines
```

This is required for diagnostics, Studio cards and why/why-not.

## Non-goals

Normalizer must not:

- execute Go functions;
- guess missing safety laws;
- silently change rule meaning;
- hide unsafe actuator paths;
- generate state fields that the user did not declare.
