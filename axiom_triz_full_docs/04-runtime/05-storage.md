# Storage

Storage must preserve enough information for recovery, replay and audit.

## Minimal records

| Record | Purpose |
|---|---|
| execution | current status, model hash, state snapshot/version |
| history entry | append-only causal record |
| activity task | pending/running/completed function call |
| timer | delayed rule wakeup |
| model version | source hash, normalized IR hash, codegen hash |

`axiom-main` already has memory and Pebble-backed stores. TRIZ runtime should
reuse the same store boundary after normalization.

## Locks

One execution must be updated transactionally:

```text
lock execution
append history
apply writes
schedule tasks
save state
commit
```

Activity workers must use leases so another worker can recover abandoned work.

## Migration

No production store should silently reinterpret old history under a new model.
Allowed paths:

- continue old executions with old model;
- migrate state explicitly;
- reject incompatible migration;
- require manual repair.
