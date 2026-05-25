# Законы развития системы Axiom

## Закон повышения идеальности

Axiom развивается так:

```text
manual workflow → declarative rules
string maps → typed codegen
manual retry → profiles
manual audit → history
manual debug → why/why-not
runtime terms → human behavior terms
```

## Закон перехода в надсистему

Функции уходят в надсистемы:

| Функция | Надсистема |
|---|---|
| type checking | Go compiler |
| editing | Rule Studio |
| source control | Git |
| tests | generated Go tests |
| observability | logs/traces/metrics |
| deployment | single binary |
| design checking | AI/TRIZ assistant |

## Закон динамичности

Одна модель должна иметь разные представления:

```text
human scenario view
source DSL view
dependency graph view
normalized IR view
runtime trace view
```

## Закон согласования ритмов

В HydroPilot разные ритмы:

```text
water level: every 1 min
temperature: every 5 min
pH/TDS: every 10 min
solution check: after fresh measurements
light: daily schedule
```

Axiom должен согласовывать эти ритмы через events и rules.

## Закон увеличения управляемости

Система должна переходить от “код исполняется” к “поведение управляется”:

```text
runnable rules
blocked reasons
pending actions
safety laws
simulation
manual override
```

## Закон перехода к самодиагностике

Rule Studio должен сам обнаруживать:

```text
unsafe actuator path
missing condition
unused output
self-trigger loop
schedule without listener
field never written
law never satisfiable
```
