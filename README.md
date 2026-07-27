# Axiom

**Русский** · [English](README.en.md)

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom — библиотека для переходов состояния, бизнес-процессов и таблиц решений на Go.

Она подходит для систем, в которых изменение состояния нужно:

- проверить до записи;
- связать с типизированным событием;
- выполнить вместе с внешней операцией;
- сохранить транзакционно;
- объяснить по журналу;
- воспроизвести после сбоя или во время аудита.

Описание процесса можно хранить в Go, AXM или TOML. Декларативные варианты компилируются в единый `axiom.Plan` и используют общий исполнитель.

## Когда библиотека полезна

Axiom рассчитан на сущности с собственным жизненным циклом: заказ, заявка, станок, кофейный автомат, партия продукции, платёжная операция.

Хорошие сценарии:

- управление оборудованием;
- учёт денег и материалов;
- согласования;
- оркестрация внешних операций;
- повторные попытки и идемпотентность;
- инварианты состояния;
- аудит и replay.

Axiom не нужен для обычного CRUD без переходов и инвариантов. Он также не заменяет брокер сообщений, распределённый планировщик и межпроцессную блокировку. Операции одного `execution ID` сериализуются внутри одного экземпляра `Engine`; распределённое владение организуется отдельно.

## Установка

Требуется Go 1.26 или новее.

```bash
go get github.com/Homiakus/axiom
```

# Пример: кофейный автомат с учётом денег

Полный запускаемый файл: [`examples/coffee-machine/main.go`](examples/coffee-machine/main.go).

Автомат должен:

1. принять деньги;
2. сохранить кредит текущего покупателя;
3. проверить цену и остатки ингредиентов;
4. выполнить команду реальному оборудованию;
5. учесть подтверждённую цену и сдачу;
6. списать ингредиенты;
7. проверить бухгалтерские инварианты;
8. записать состояние и историю в Pebble;
9. восстановить состояние через replay.

## Меню

Деньги хранятся целыми копейками. `14000` означает `140,00 ₽`. Для расчётов не используется `float64`.

| Напиток | Цена | Вода | Кофе | Молоко | Стакан |
|---|---:|---:|---:|---:|---:|
| Эспрессо | 90,00 ₽ | 40 мл | 8 г | — | 1 |
| Капучино | 140,00 ₽ | 60 мл | 10 г | 120 мл | 1 |

Цена и рецепт находятся в модели. Событие выбора напитка не содержит цену, поэтому клиент не может заказать капучино по цене эспрессо.

## Состояние автомата

| Поле | Смысл |
|---|---|
| `CreditKopecks` | Деньги текущего покупателя, ещё не признанные выручкой |
| `AcceptedKopecks` | Все принятые деньги |
| `ReturnedKopecks` | Сдача и возвраты |
| `RevenueKopecks` | Стоимость выданных напитков |
| `CashboxKopecks` | Физические деньги в кассе |
| `WaterML`, `BeansG`, `MilkML`, `Cups` | Остатки расходных материалов |
| `DrinksServed` | Число выданных напитков |
| `LastDrink`, `LastChangeKopecks` | Данные для экрана и журнала |

```go
type Machine struct {
    CreditKopecks int `json:"creditKopecks"`

    AcceptedKopecks int `json:"acceptedKopecks"`
    ReturnedKopecks int `json:"returnedKopecks"`
    RevenueKopecks  int `json:"revenueKopecks"`
    CashboxKopecks  int `json:"cashboxKopecks"`

    WaterML int `json:"waterML"`
    BeansG  int `json:"beansG"`
    MilkML  int `json:"milkML"`
    Cups    int `json:"cups"`

    DrinksServed      int    `json:"drinksServed"`
    LastDrink         string `json:"lastDrink"`
    LastChangeKopecks int    `json:"lastChangeKopecks"`
    LastDispensed     bool   `json:"lastDispensed"`
}
```

## События

| Событие | Источник | Что означает |
|---|---|---|
| `MoneyInserted` | Монетоприёмник или терминал | Покупатель внёс деньги |
| `EspressoRequested` | Кнопка эспрессо | Запрошена покупка эспрессо |
| `CappuccinoRequested` | Кнопка капучино | Запрошена покупка капучино |
| `CancelRequested` | Кнопка отмены | Нужно вернуть весь текущий кредит |

```go
type MoneyInserted struct {
    AmountKopecks int `json:"amountKopecks"`
}

func (MoneyInserted) AxiomEventName() string {
    return "MoneyInserted"
}

type CappuccinoRequested struct {
    // Идентификатор связывает событие с одной физической выдачей.
    PurchaseID string `json:"purchaseId"`
}

func (CappuccinoRequested) AxiomEventName() string {
    return "CappuccinoRequested"
}
```

## Таблица переходов

| Событие | Условия | Изменения | Внешняя операция |
|---|---|---|---|
| `MoneyInserted` | Сумма больше нуля | Увеличить кредит, принятые деньги и кассу | Нет |
| `EspressoRequested` | Кредит ≥ 90 ₽; достаточно воды, кофе и стаканов | Учесть 90 ₽ выручки, выдать сдачу, списать рецепт | `DispenseEspresso` |
| `CappuccinoRequested` | Кредит ≥ 140 ₽; достаточно воды, кофе, молока и стаканов | Учесть 140 ₽ выручки, выдать сдачу, списать рецепт | `DispenseCappuccino` |
| `CancelRequested` | Кредит больше нуля | Вернуть кредит, уменьшить кассу | `ReturnMoney` |

Если условие не выполнено, правило не запускается. Например, капучино нельзя выдать при кредите 100 ₽ или при пустой ёмкости молока.

## Создание модели

```go
definition := model.New("CoffeeMachine").Version("1")

machine := model.State[Machine](definition, "Machine").
    Default("CreditKopecks", 0).
    Default("AcceptedKopecks", 0).
    Default("ReturnedKopecks", 0).
    Default("RevenueKopecks", 0).
    Default("CashboxKopecks", 0).
    Default("WaterML", 2000).
    Default("BeansG", 500).
    Default("MilkML", 1000).
    Default("Cups", 50).
    Default("DrinksServed", 0)

moneyInserted := model.Event[MoneyInserted](definition, "MoneyInserted")
espressoRequested := model.Event[EspressoRequested](definition, "EspressoRequested")
cappuccinoRequested := model.Event[CappuccinoRequested](definition, "CappuccinoRequested")
cancelRequested := model.Event[CancelRequested](definition, "CancelRequested")
```

## Приём денег

Все три счётчика записываются в рамках одного перехода.

```go
definition.Rule("acceptMoney").
    On(moneyInserted.Trigger()).
    When(model.GT(
        moneyInserted.Field("AmountKopecks"),
        model.Lit(0),
    )).
    Set(
        machine.Field("CreditKopecks"),
        add(
            machine.Field("CreditKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    ).
    Set(
        machine.Field("AcceptedKopecks"),
        add(
            machine.Field("AcceptedKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    ).
    Set(
        machine.Field("CashboxKopecks"),
        add(
            machine.Field("CashboxKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    )
```

В примере `add` и `sub` только формируют выражения декларативной модели:

```go
func add(left, right model.Expr) model.Expr {
    return model.Raw(fmt.Sprintf("(%s + %s)", left, right))
}

func sub(left, right model.Expr) model.Expr {
    return model.Raw(fmt.Sprintf("(%s - %s)", left, right))
}
```

## Политика внешних операций

Приготовление напитка воздействует на насосы, нагреватель, кофемолку и механизм сдачи. Для таких операций задаётся политика выполнения.

```go
definition.Policy("hardwarePolicy").
    Retry(2).
    Timeout(10 * time.Second).
    Concurrency("once").
    Idempotency("required")
```

| Настройка | Значение | Назначение |
|---|---:|---|
| `Retry` | 2 | До двух повторов после первой попытки |
| `Timeout` | 10 с | Ограничение времени одной попытки |
| `Concurrency` | `once` | Не выполнять одно задание параллельно |
| `Idempotency` | `required` | Требовать ключ операции |

## Activity приготовления капучино

Цена и сдача вычисляются до обращения к оборудованию. Обработчик возвращает подтверждённые суммы, а правило учитывает именно их.

```go
cappuccinoChange := sub(
    machine.Field("CreditKopecks"),
    model.Lit(cappuccinoPriceKopecks),
)

definition.Activity("DispenseCappuccino").
    Input("purchaseId", cappuccinoRequested.Field("PurchaseID")).
    Input("priceKopecks", model.Lit(cappuccinoPriceKopecks)).
    Input("changeKopecks", cappuccinoChange).
    Output("dispensed", "Bool").
    Output("priceKopecks", "Int").
    Output("changeKopecks", "Int").
    Effect("external").
    IdempotencyKey(cappuccinoRequested.Field("PurchaseID")).
    Policy("hardwarePolicy")
```

Регистрация обработчика:

```go
axiom.Act("DispenseCappuccino", func(
    ctx context.Context,
    input axiom.Input,
) (axiom.Output, error) {
    // Здесь выполняются команды контроллеру оборудования.
    // Реальный обработчик должен помнить PurchaseID и не выдавать
    // второй напиток для уже завершённой операции.
    return axiom.Output{
        "dispensed":     true,
        "priceKopecks":  input["priceKopecks"],
        "changeKopecks": input["changeKopecks"],
    }, nil
})
```

Если обработчик завершится ошибкой, правило не должно учитывать выручку и списывать ингредиенты.

## Продажа капучино

```go
definition.Rule("sellCappuccino").
    On(cappuccinoRequested.Trigger()).
    When(
        model.GTE(machine.Field("CreditKopecks"), model.Lit(14000)),
        model.GTE(machine.Field("WaterML"), model.Lit(60)),
        model.GTE(machine.Field("BeansG"), model.Lit(10)),
        model.GTE(machine.Field("MilkML"), model.Lit(120)),
        model.GTE(machine.Field("Cups"), model.Lit(1)),
    ).
    Run("DispenseCappuccino").
    Set(machine.Field("CreditKopecks"), model.Lit(0)).
    Set(
        machine.Field("ReturnedKopecks"),
        add(
            machine.Field("ReturnedKopecks"),
            model.Ref("output.changeKopecks"),
        ),
    ).
    Set(
        machine.Field("RevenueKopecks"),
        add(
            machine.Field("RevenueKopecks"),
            model.Ref("output.priceKopecks"),
        ),
    ).
    Set(
        machine.Field("CashboxKopecks"),
        sub(
            machine.Field("CashboxKopecks"),
            model.Ref("output.changeKopecks"),
        ),
    ).
    Set(machine.Field("WaterML"), sub(machine.Field("WaterML"), model.Lit(60))).
    Set(machine.Field("BeansG"), sub(machine.Field("BeansG"), model.Lit(10))).
    Set(machine.Field("MilkML"), sub(machine.Field("MilkML"), model.Lit(120))).
    Set(machine.Field("Cups"), sub(machine.Field("Cups"), model.Lit(1))).
    Set(
        machine.Field("DrinksServed"),
        add(machine.Field("DrinksServed"), model.Lit(1)),
    ).
    Set(machine.Field("LastDrink"), model.Lit("cappuccino")).
    Set(
        machine.Field("LastChangeKopecks"),
        model.Ref("output.changeKopecks"),
    ).
    Set(
        machine.Field("LastDispensed"),
        model.Ref("output.dispensed"),
    )
```

## Денежные инварианты

После каждого перехода проверяются два равенства.

### Сохранение денег

```text
принято = возвращено + выручка + текущий кредит
```

```go
definition.Claim(
    "moneyIsConserved",
    model.Eq(
        machine.Field("AcceptedKopecks"),
        add(
            machine.Field("ReturnedKopecks"),
            add(
                machine.Field("RevenueKopecks"),
                machine.Field("CreditKopecks"),
            ),
        ),
    ),
)
```

### Сверка физической кассы

Для автомата без начального разменного фонда:

```text
касса = выручка + текущий кредит
```

```go
definition.Claim(
    "cashboxMatchesAccounting",
    model.Eq(
        machine.Field("CashboxKopecks"),
        add(
            machine.Field("RevenueKopecks"),
            machine.Field("CreditKopecks"),
        ),
    ),
)
```

При наличии стартового разменного фонда в состояние добавляется `OpeningFloatKopecks`:

```text
касса = стартовый фонд + выручка + текущий кредит
```

Отдельный `Claim` запрещает отрицательные деньги и остатки ингредиентов.

## Движение денег в примере

| Шаг | Операция | Кредит | Принято | Возвращено | Выручка | Касса |
|---:|---|---:|---:|---:|---:|---:|
| 0 | Начальное состояние | 0 ₽ | 0 ₽ | 0 ₽ | 0 ₽ | 0 ₽ |
| 1 | Внесено 200 ₽ | 200 ₽ | 200 ₽ | 0 ₽ | 0 ₽ | 200 ₽ |
| 2 | Капучино 140 ₽, сдача 60 ₽ | 0 ₽ | 200 ₽ | 60 ₽ | 140 ₽ | 140 ₽ |
| 3 | Внесено 100 ₽ | 100 ₽ | 300 ₽ | 60 ₽ | 140 ₽ | 240 ₽ |
| 4 | Эспрессо 90 ₽, сдача 10 ₽ | 0 ₽ | 300 ₽ | 70 ₽ | 230 ₽ | 230 ₽ |
| 5 | Внесено 50 ₽ | 50 ₽ | 350 ₽ | 70 ₽ | 230 ₽ | 280 ₽ |
| 6 | Отмена и возврат 50 ₽ | 0 ₽ | 350 ₽ | 120 ₽ | 230 ₽ | 230 ₽ |

Итоговые равенства:

```text
350 ₽ = 120 ₽ + 230 ₽ + 0 ₽
230 ₽ = 230 ₽ + 0 ₽
```

Остатки после двух напитков:

| Ресурс | Было | Израсходовано | Осталось |
|---|---:|---:|---:|
| Вода | 2 000 мл | 100 мл | 1 900 мл |
| Кофе | 500 г | 18 г | 482 г |
| Молоко | 1 000 мл | 120 мл | 880 мл |
| Стаканы | 50 | 2 | 48 |

## Компиляция, Pebble и запуск

```go
plan, err := definition.Compile()
if err != nil {
    return err
}

store, err := axiom.OpenPebble("data/coffee-machine")
if err != nil {
    return err
}
defer store.Close()

engine, err := plan.New(
    axiom.WithStore(store),
    axiom.Act("DispenseEspresso", dispenseEspresso),
    axiom.Act("DispenseCappuccino", dispenseCappuccino),
    axiom.Act("ReturnMoney", returnMoney),
)
if err != nil {
    return err
}

// Один execution ID соответствует одному физическому автомату.
run := engine.Execution("coffee-machine-01")

_ = run.Dispatch(ctx, MoneyInserted{AmountKopecks: 20000})
_ = run.Dispatch(ctx, CappuccinoRequested{PurchaseID: "sale-0001"})
```

Pebble отвечает за транзакционное хранение, но сам по себе не включает строгий fast runtime. `WithStrictFastRuntime` и `WithProductionMode` включаются явно. В этом примере используется обычный скомпилированный runtime, поскольку бухгалтерские `Claim` содержат арифметические выражения.

## История и replay

```go
var state Machine
if err := run.State(ctx, &state); err != nil {
    return err
}

history, err := run.History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(
    plan.Module(),
    history,
)
```

Replay проверяет хеш скомпилированного плана. Историю нельзя случайно воспроизвести другой версией модели.

## Запуск примера

```bash
go run ./examples/coffee-machine
```

Ожидаемый итог:

```text
принято:    350,00 ₽
возвращено: 120,00 ₽
выручка:    230,00 ₽
касса:      230,00 ₽
кредит:     0,00 ₽
напитков:   2
вода:       1900 мл
кофе:       482 г
молоко:     880 мл
стаканы:    48
```

CI запускает этот пример и проверяет денежные итоги, а не только компиляцию файла.

## Что демонстрирует пример

| Возможность | Результат |
|---|---|
| Типизированные события | Нельзя передать произвольный набор полей |
| Компиляция модели | Ошибки имён, типов, policy и activity обнаруживаются до запуска |
| Условия правил | Недостаток денег или ингредиентов блокирует продажу |
| Activity | Физическая операция отделена от расчётных правил |
| Идемпотентность | Повтор задания связывается с тем же `PurchaseID` |
| `Claim` | Нарушение денежного баланса откатывает переход |
| Pebble | Состояние и история хранятся транзакционно |
| `execution ID` | События одного автомата выполняются последовательно |
| История | Видны деньги, продажи, сдача, возвраты и результаты activity |
| Replay | Состояние восстанавливается из журнала |

# Способы описания процесса

| API | Для чего | Файлы | Статический анализ |
|---|---|---:|---:|
| Typed Go Flow | Небольшая машина состояний с произвольной логикой Go | Нет | Нет |
| Декларативная Go-модель | Проверяемые правила, activities и claims | Нет | Полный |
| AXM | Версионируемая модель вне приложения | Да | Полный |
| TOML | Таблица переходов как конфигурация | Да | Полный |
| Низкоуровневый runtime | Явные `Start`, `Signal`, `Patch`, `RunUntilIdle` | Необязательно | Зависит от модели |

## API экземпляра процесса

```go
run := engine.Execution("coffee-machine-01")

err := run.Dispatch(ctx, MoneyInserted{AmountKopecks: 10000})

var state Machine
err = run.State(ctx, &state)

status, err := run.Status(ctx)
history, err := run.History(ctx)
pending, err := run.PendingActivities(ctx)
explanation, err := run.Explain(ctx)
err = run.Cancel(ctx)
```

## Режимы Pebble

```go
// fsync на каждом commit
store, err := axiom.OpenPebble("data/axiom")

// Быстрее, но последние записи могут быть потеряны при аварии.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleNoSync(),
)

// Периодический flush.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleSyncEvery(10*time.Millisecond),
)
```

`WithProductionMode` требует транзакционное хранилище и включает строгий fast runtime. Модели с неподдерживаемыми строгим режимом выражениями отклоняются при создании `Engine`.

## Конкурентность

Внутри одного процесса и одного `Engine`:

- операции одного `execution ID` сериализуются;
- разные `execution ID` могут выполняться параллельно;
- состояние и история записываются атомарно при транзакционном store;
- типы целых чисел сохраняются после повторного открытия Pebble.

Для нескольких процессов нужен маршрутизатор владельца, распределённая блокировка или store с эквивалентными гарантиями.

## Производительность

Базовый прогон выполнен на общем GitHub runner: `linux/amd64`, Go 1.26.5, 4 логических CPU, конкурентность 8. Значения предназначены для поиска крупных регрессий, а не как независимый от оборудования SLA.

| Сценарий | p95 | p99 | Производительность |
|---|---:|---:|---:|
| Go Flow, разные экземпляры | 3,841 мс | 4,788 мс | 9 028 операций/с |
| Go Flow, один общий экземпляр | 20,777 мс | 24,880 мс | 772 операции/с |
| Скомпилированный runtime, разные экземпляры | 0,505 мс | 3,011 мс | 55 011 операций/с |
| Скомпилированный runtime, один общий экземпляр | 1,085 мс | 1,437 мс | 50 938 операций/с |
| Pebble NoSync | 3,904 мс | 5,061 мс | 8 773 операции/с |
| Pebble Sync | 8,688 мс | 10,225 мс | 1 437 операций/с |
| Replay 1 000 событий | 1,977 мс | 2,541 мс | 761 replay/с |

Полный отчёт: [`benchmarks/latest.md`](benchmarks/latest.md).

## Проверка проекта

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
go run ./examples/coffee-machine
```

CI также собирает отдельный пользовательский модуль, использующий только публичные пакеты.

## Пакеты

| Пакет | Назначение |
|---|---|
| `github.com/Homiakus/axiom` | `Plan`, runtime, execution API и Typed Go Flow |
| `github.com/Homiakus/axiom/model` | Декларативная Go-модель |
| `github.com/Homiakus/axiom/axm` | AXM frontend |
| `github.com/Homiakus/axiom/table` | TOML frontend |
| `github.com/Homiakus/axiom/store/pebble` | Pebble-хранилище |
| `github.com/Homiakus/axiom/cmd/axiomgen` | Генератор типизированных границ |
| `github.com/Homiakus/axiom/cmd/axiombench` | Нагрузочный тест и перцентили |

## Лицензия

Apache-2.0. См. [`LICENSE`](LICENSE).
