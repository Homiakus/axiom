# History and Replay

History is the durable source of truth for one execution.

## Entries

Runtime records:

- input event/signal;
- state patch;
- rule decision;
- activity scheduled;
- activity completed or failed;
- writes applied;
- claim checks;
- fixpoint reached.

## Replay

Replay rebuilds state from history. It must not call external functions again.

```text
history + model version -> deterministic state
```

If model version changed, runtime must either keep old executions on the old
model or run an explicit migration.

## Explainability

Why/why-not is built from history plus indexes:

- which input affected the rule;
- which conditions were true/false/unknown;
- which function was called;
- which output fields were written;
- which always laws were checked.
