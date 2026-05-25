# Execution Semantics

Runtime executes normalized IR, not the human syntax directly.

## Input

Inputs are:

- event/signal;
- state patch;
- function/activity completion;
- timer wakeup.

Each input is appended to history before it affects state.

## Rule eligibility

A rule can run when:

1. its trigger is affected;
2. all `when` expressions are true;
3. all required conditions/facts are true;
4. relevant `always` laws are not violated;
5. profile/policy allows scheduling.

If any part is false, runtime records why the rule is blocked.

## Direct state rule

Rule without `do`:

```text
evaluate then writes
check claims
apply writes
append history
continue until fixpoint
```

## Function rule

Rule with `do`:

```text
evaluate function input
schedule managed activity
append ActivityScheduled
worker executes Go function
append ActivityCompleted or ActivityFailed
apply then writes from output
continue until fixpoint
```

External effects are not repeated during replay. Replay uses history.

## Fixpoint

After every write, runtime uses dependency indexes to find affected rules. It
continues until no more immediate rules can run.

Loop detection must stop non-convergent models and report a diagnostic instead
of spinning indefinitely.
