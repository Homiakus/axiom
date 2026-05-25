# Versioning and Migration

## DSL version

Add optional version:

```axiom
system HydroPilot
dsl: "triz-0.1"
```

or metadata block in future.

## Model hash

Runtime stores:

```text
source hash
normalized IR hash
codegen hash
```

## Execution migration

Options:

```text
continue old executions on old model
migrate state with migration script
manual repair
reject incompatible migration
```

## Migration file

```axiom
migration HydroPilot_001_to_002:
  set NewField = "default"
  rename Old.field -> New.field
```

Future feature.

## Axiom v0 migration

Tools:

```bash
axiom migrate old.axm --to-triz new.triz.axm
axiom normalize new.triz.axm --to-v0 normalized.axm
```

## Compatibility rule

No production runtime should silently change meaning of existing execution.
