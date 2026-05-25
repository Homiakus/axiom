# Observability

## Logs

Structured logs:

```text
execution_id
rule
function
event
attempt
duration
status
error_code
```

## Metrics

```text
executions_started
executions_failed
rules_evaluated
rules_runnable
rules_blocked
actions_scheduled
actions_completed
actions_failed
action_latency
replay_duration
fixpoint_iterations
```

## Traces

OpenTelemetry spans:

```text
Axiom.Execution
Axiom.Rule.Evaluate
Axiom.Action.Schedule
Axiom.Action.Execute
Axiom.Write.Apply
Axiom.Always.Check
```

## Studio live view

```text
current state
pending actions
recent history
blocked rules
last errors
next timers
```

## Audit

History is audit source. Logs are secondary.

Audit report should include:

```text
who/what sent event
why rule ran
what function was called
what output returned
what state changed
what safety laws passed
```
