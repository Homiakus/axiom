# Safety Laws

`always` описывает состояние, которое нельзя нарушать.

```axiom
always NoDosingDuringEstop:
  Safety.estop == true implies Dosing.active == false
```

## Типы законов

| Тип | Пример |
|---|---|
| physical safety | pump не работает при low water |
| logical consistency | `paidCount <= 1` |
| business safety | заказ не shipped без payment |
| data consistency | готовый dose plan имеет zone, amount и payload |

## Где проверять

Runtime должен проверять `always`:

- после входного event/patch;
- перед dangerous function, если write impact понятен;
- перед применением `then`;
- после применения `then`;
- при replay;
- в generated tests.

## В Studio

Studio должна показывать:

- какие rules могут включить actuator;
- какие conditions их защищают;
- какие `always` покрывают этот путь;
- почему правило заблокировано;
- какой write потенциально нарушает закон.

Safety — не отдельный документ после кода, а часть модели поведения.
