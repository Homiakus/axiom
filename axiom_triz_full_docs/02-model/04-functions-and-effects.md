# Functions and Effects

Function — это граница между declarative rules и обычным Go-кодом.

```axiom
function SendNotification(message: Text) -> { sent: Bool }
```

## Контракт

Function:

- получает только input из rule;
- возвращает output по объявленной схеме;
- не меняет state напрямую;
- не решает, какие rules запускать дальше;
- может быть повторена runtime, если profile разрешает retry.

## Виды

| Вид | Пример | Runtime mapping |
|---|---|---|
| pure/local | calculate payload, parse response | `activity effect: local` |
| external | UART, DB, email, payment | `activity effect: external` |
| critical | actuator, money, irreversible command | external + strict policy |

## Profile

Опасная или внешняя function должна иметь profile:

```axiom
do critical:
  result = DosePHDown(event.zone, amount)
```

Normalizer разворачивает `critical` в policy с timeout, idempotency, audit и
ограничениями concurrency.

## Почему function не пишет state

Если Go-код сам меняет state, runtime теряет:

- replay без повторного external effect;
- explainability;
- проверку `always`;
- deterministic tests;
- понятную историю изменений.

Поэтому Go возвращает output, а state меняет только `then`.
