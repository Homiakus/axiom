# Противоречия и их разрешение

## Противоречие 1: простота DSL против строгости runtime

Требование A:

```text
DSL должен быть простым и понятным.
```

Требование B:

```text
Runtime должен быть строгим, безопасным и детерминированным.
```

Плохой компромисс:

```text
Добавлять в DSL все runtime-детали.
```

ТРИЗ-решение:

```text
Разделение по уровням:
  simple DSL → normalized IR → strict runtime.
```

## Противоречие 2: обычные функции против управляемых effects

Пользователь хочет писать:

```go
func(ctx, input) (output, error)
```

Runtime хочет:

```text
timeout
retry
idempotency
audit
history
replay
```

Решение:

```text
Функция остаётся простой.
Runtime оборачивает её в managed action.
```

## Противоречие 3: независимые правила против порядка процесса

Пользователь не должен писать порядок вручную, но процесс должен идти правильно.

Решение:

```text
Порядок возникает из state dependencies.
```

Пример:

```text
MeasurePH writes Zone.ph
MeasureTDS writes Zone.tds
CheckSolution waits for both
DosePHDown waits for CheckSolution result
```

## Противоречие 4: explainability против автоматического поведения

Автоматический граф может казаться магией.

Решение:

```text
Использовать history + indexes как источник explainability.
```

Каждое правило должно иметь:

```text
why ran
why blocked
what changed
what action called
what wrote state
what always checked
```

## Противоречие 5: safety против скорости разработки

Safety требует правил, но пользователь хочет быстро писать.

Решение:

```text
profiles + default safety templates + diagnostics.
```

Пример:

```axiom
do critical:
  result = DosePHDown(...)
```

Normalizer разворачивает это в policy with idempotency, timeout, audit.

## Противоречие 6: визуальность против точности

Граф красив, но может скрывать детали. Текст точен, но сложен.

Решение:

```text
двухпанельный редактор:
  слева source
  справа live карточки, graph, diagnostics, simulation
```
