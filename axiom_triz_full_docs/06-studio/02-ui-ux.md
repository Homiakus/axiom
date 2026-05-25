# UI/UX

## Desktop layout

```text
Navigation | Main card/list | Explanation
```

Navigation shows scenarios, state groups, functions and diagnostics.
Main area shows selected rule/function/state. Explanation shows why/why-not,
graph edges, history and source.

## Mobile layout

Use tabs:

```text
Project | Rules | Actions | Simulation | Source
```

Cards stack vertically. Graph becomes a timeline.

## Rule card

Show only behavior first:

```text
name
scenario
when
do
then
safety
why / why-not
diagnostics
```

Advanced foldout can show normalized `signal/context/activity/policy` terms.

## Function card

```text
name
called by
inputs
outputs
profile
idempotency key
Go signature
implementation status
safety risks
```

## State inspector

```text
field
current value
written by
read by
protected by
last changed
```

## UX rules

- Default view uses TRIZ terms.
- Runtime/IR terms are advanced mode.
- Every blocked rule has a reason.
- Every dangerous function shows its safety coverage.
- Every state write links back to a rule.
