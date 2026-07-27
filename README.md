# Axiom

**Русский** · [English](README.en.md)

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom — библиотека для описания и исполнения переходов состояния, бизнес-процессов и таблиц решений на Go.

Она нужна, когда изменение состояния должно быть не только выполнено, но и проверено, записано и при необходимости воспроизведено. В одной модели можно связать:

- типизированные события;
- правила переходов;
- инварианты состояния;
- внешние операции с повторами и ключами идемпотентности;
- транзакционное хранение;
- историю, объяснение результата и replay.

Файлы описания не обязательны. Процесс можно задать обычным Go-кодом, декларативной Go-моделью, AXM или TOML. Декларативные варианты компилируются в один `axiom.Plan` и исполняются общим runtime.

## Когда Axiom подходит

Axiom полезен, если состояние принадлежит конкретной сущности — заказу, заявке, устройству, партии продукции, банковской операции — и каждое изменение должно быть проверено и записано.

Типичные задачи:

- управление состояниями оборудования;
- расчёт и контроль денежных потоков;
- обработка заказов и платежей;
- согласования и многошаговые заявки;
- фоновые операции с повторами;
- таблицы решений;
- восстановление состояния из истории;
- аудит причин срабатывания правил.

## Когда Axiom не нужен

Axiom избыточен для простого CRUD без переходов и инвариантов. Библиотека не заменяет брокер сообщений, распределённый планировщик или межпроцессную блокировку. Сериализация одного `execution ID` гарантируется внутри одного экземпляра `Engine`; распределённое владение нужно организовать отдельно.

## Установка

Требуется Go 1.26 или новее.

```bash
go get github.com/Homiakus/axiom
```

# Основной пример: кофейный автомат

Пример показывает один физический автомат, который:

- принимает деньги;
- хранит текущий кредит покупателя;
- проверяет цену и остатки ингредиентов;
- готовит эспрессо или капучино;
- выдаёт сдачу;
- возвращает деньги при отмене;
- ведёт выручку и физическую кассу;
- сохраняет историю операций в Pebble;
- восстанавливает состояние через replay.

Полный запускаемый код находится в [`examples/coffee-machine/main.go`](examples/coffee-machine/main.go).

## Меню и расход ингредиентов

Все денежные значения хранятся в копейках. Например, `14000` означает `140,00 ₽`. Для денег не используется `float64`.

| Напиток | Цена | Вода | Кофе | Молоко | Стакан |
|---|---:|---:|---:|---:|---:|
| Эспрессо | 90,00 ₽ | 40 мл | 8 г | — | 1 |
| Капучино | 140,00 ₽ | 60 мл | 10 г | 120 мл | 1 |

Цена и рецепт находятся в модели, а не приходят от клиента. Поэтому событие `CappuccinoRequested` не может подменить цену или уменьшить расход молока.

## Состояние автомата

| Поле | Что хранит | Изменяется когда |
|---|---|---|
| `CreditKopecks` | Деньги текущего покупателя | Внесение денег, покупка, отмена |
| `AcceptedKopecks` | Сколько денег автомат принял за всё время | Внесение денег |
| `ReturnedKopecks` | Сумма сдачи и отменённых покупок | Покупка, отмена |
| `RevenueKopecks` | Оплаченные напитки | Успешная покупка |
| `CashboxKopecks` | Физические деньги в кассе | Внесение, сдача, возврат |
| `WaterML`, `BeansG`, `MilkML`, `Cups` | Остатки ингредиентов | Успешная выдача напитка |
| `DrinksServed` | Число выданных напитков | Успешная выдача |
| `LastDrink`, `LastChangeKopecks` | Данные для экрана и журнала | Покупка или возврат |

```go
// Все суммы — в копейках. Это исключает ошибки округления денег.
type Machine struct {
    CreditKopecks   int `json:"creditKopecks"`
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

| Событие | Источник | Назначение |
|---|---|---|
| `MoneyInserted` | Монетоприёмник или платёжный терминал | Увеличить кредит и кассу |
| `EspressoRequested` | Кнопка эспрессо | Проверить условия и приготовить эспрессо |
| `CappuccinoRequested` | Кнопка капучино | Проверить условия и приготовить капучино |
| `CancelRequested` | Кнопка отмены | Вернуть весь текущий кредит |

```go
type MoneyInserted struct {
    AmountKopecks int `json:"amountKopecks"`
}

func (MoneyInserted) AxiomEventName() string { return "MoneyInserted" }

type CappuccinoRequested struct {
    // Используется как ключ идемпотентности внешней операции.
    PurchaseID string `json:"purchaseId"`
}

func (CappuccinoRequested) AxiomEventName() string {
    return "CappuccinoRequested"
}
```

## Таблица переходов

| Событие | Условие | Изменения состояния | Внешняя операция |
|---|---|---|---|
| `MoneyInserted` | `amount > 0` | `credit += amount`, `accepted += amount`, `cashbox += amount` | Нет |
| `EspressoRequested` | Кредит ≥ 90 ₽, вода ≥ 40 мл, кофе ≥ 8 г, есть стакан | Выручка +90 ₽, списание ингредиентов, кредит → 0, расчёт сдачи | `DispenseEspresso` |
| `CappuccinoRequested` | Кредит ≥ 140 ₽, вода ≥ 60 мл, кофе ≥ 10 г, молоко ≥ 120 мл, есть стакан | Выручка +140 ₽, списание ингредиентов, кредит → 0, расчёт сдачи | `DispenseCappuccino` |
| `CancelRequested` | Кредит > 0 | Касса уменьшается на кредит, кредит → 0, возврат увеличивается | `ReturnMoney` |

Если условие перехода не выполнено, правило не запускается. Например, при кредите 100 ₽ событие `CappuccinoRequested` не выдаст напиток и не изменит денежные счётчики.

## Инициализация модели

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

Три счётчика изменяются одним переходом. Если запись в хранилище завершится ошибкой, переход не должен оставить частично обновлённое состояние.

```go
definition.Rule("acceptMoney").
    On(moneyInserted.Trigger()).
    When(model.GT(
        moneyInserted.Field("AmountKopecks"),
        model.Lit(0),
    )).
    // Кредит текущего покупателя.
    Set(
        machine.Field("CreditKopecks"),
        add(
            machine.Field("CreditKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    ).
    // Общая сумма всех принятых средств.
    Set(
        machine.Field("AcceptedKopecks"),
        add(
            machine.Field("AcceptedKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    ).
    // Монета уже физически находится внутри автомата.
    Set(
        machine.Field("CashboxKopecks"),
        add(
            machine.Field("CashboxKopecks"),
            moneyInserted.Field("AmountKopecks"),
        ),
    )
```

`add` и `sub` — небольшие функции для построения арифметических выражений модели:

```go
func add(left, right model.Expr) model.Expr {
    return model.Raw(fmt.Sprintf("(%s + %s)", left, right))
}

func sub(left, right model.Expr) model.Expr {
    return model.Raw(fmt.Sprintf("(%s - %s)", left, right))
}
```

## Продажа капучино

Цена и расход ингредиентов заданы константами приложения. Событие содержит только идентификатор покупки.

```go
const (
    cappuccinoPriceKopecks = 14000
    cappuccinoWaterML      = 60
    cappuccinoBeansG       = 10
    cappuccinoMilkML       = 120
)

// Сдача вычисляется из состояния до перехода.
cappuccinoChange := sub(
    machine.Field("CreditKopecks"),
    model.Lit(cappuccinoPriceKopecks),
)

definition.Rule("sellCappuccino").
    On(cappuccinoRequested.Trigger()).
    When(
        // Недостаточный кредит или ингредиенты блокируют переход.
        model.GTE(machine.Field("CreditKopecks"), model.Lit(cappuccinoPriceKopecks)),
        model.GTE(machine.Field("WaterML"), model.Lit(cappuccinoWaterML)),
        model.GTE(machine.Field("BeansG"), model.Lit(cappuccinoBeansG)),
        model.GTE(machine.Field("MilkML"), model.Lit(cappuccinoMilkML)),
        model.GTE(machine.Field("Cups"), model.Lit(1)),
    ).
    // Сначала выполняется операция с реальным оборудованием.
    Run("DispenseCappuccino").
    // После успешной операции фиксируется единый переход состояния.
    Set(machine.Field("CreditKopecks"), model.Lit(0)).
    Set(
        machine.Field("ReturnedKopecks"),
        add(machine.Field("ReturnedKopecks"), cappuccinoChange),
    ).
    Set(
        machine.Field("RevenueKopecks"),
        add(machine.Field("RevenueKopecks"), model.Lit(cappuccinoPriceKopecks)),
    ).
    Set(
        machine.Field("CashboxKopecks"),
        sub(machine.Field("CashboxKopecks"), cappuccinoChange),
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
    Set(machine.Field("LastChangeKopecks"), cappuccinoChange).
    Set(machine.Field("LastDispensed"), model.Ref("output.dispensed"))
```

## Внешняя операция и идемпотентность

Приготовление напитка воздействует на реальное оборудование. Повтор одного и того же задания не должен выдать второй стакан, поэтому `PurchaseID` используется как ключ идемпотентности.

```go
definition.Policy("hardwarePolicy").
    Retry(2).
    Timeout(10 * time.Second).
    Concurrency("once").
    Idempotency("required")

definition.Activity("DispenseCappuccino").
    Input("purchaseId", cappuccinoRequested.Field("PurchaseID")).
    Input("priceKopecks", model.Lit(cappuccinoPriceKopecks)).
    Input("changeKopecks", cappuccinoChange).
    Output("dispensed", "Bool").
    Effect("external").
    IdempotencyKey(cappuccinoRequested.Field("PurchaseID")).
    Policy("hardwarePolicy")
```

Обработчик подключается при создании `Engine`:

```go
engine, err := plan.New(
    axiom.WithStore(store),
    axiom.Act("DispenseCappuccino", func(
        ctx context.Context,
        input axiom.Input,
    ) (axiom.Output, error) {
        // Здесь находятся команды контроллеру кофемолки, насоса,
        // нагревателя и механизма выдачи сдачи.
        return axiom.Output{"dispensed": true}, nil
    }),
)
```

Если обработчик вернёт ошибку, денежные и складские изменения перехода не должны быть зафиксированы.

В этом примере используется обычный скомпилированный runtime: денежные инварианты содержат арифметические выражения, которые строгий fast runtime пока намеренно не принимает. `WithProductionMode` следует включать только для моделей, полностью поддерживаемых строгим набором выражений.

## Денежные инварианты

После каждого перехода Axiom проверяет два равенства.

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

Если в реальном автомате есть стартовый разменный фонд, в состояние добавляется `OpeningFloatKopecks`, а равенство принимает вид:

```text
касса = стартовый разменный фонд + выручка + текущий кредит
```

Отдельный `Claim` запрещает отрицательные деньги и остатки ингредиентов.

## Разбор трёх операций

Пример выполняет две продажи и одну отмену.

| Шаг | Действие | Кредит после шага | Принято всего | Возвращено всего | Выручка | Касса |
|---:|---|---:|---:|---:|---:|---:|
| 0 | Начальное состояние | 0 ₽ | 0 ₽ | 0 ₽ | 0 ₽ | 0 ₽ |
| 1 | Внесено 200 ₽ | 200 ₽ | 200 ₽ | 0 ₽ | 0 ₽ | 200 ₽ |
| 2 | Куплен капучино за 140 ₽, сдача 60 ₽ | 0 ₽ | 200 ₽ | 60 ₽ | 140 ₽ | 140 ₽ |
| 3 | Внесено 100 ₽ | 100 ₽ | 300 ₽ | 60 ₽ | 140 ₽ | 240 ₽ |
| 4 | Куплен эспрессо за 90 ₽, сдача 10 ₽ | 0 ₽ | 300 ₽ | 70 ₽ | 230 ₽ | 230 ₽ |
| 5 | Внесено 50 ₽ | 50 ₽ | 350 ₽ | 70 ₽ | 230 ₽ | 280 ₽ |
| 6 | Покупка отменена, возвращено 50 ₽ | 0 ₽ | 350 ₽ | 120 ₽ | 230 ₽ | 230 ₽ |

Итоговая проверка:

```text
350 ₽ = 120 ₽ + 230 ₽ + 0 ₽
230 ₽ = 230 ₽ + 0 ₽
```

## Запуск и replay

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

var state Machine
_ = run.State(ctx, &state)

history, err := run.History(ctx)
if err != nil {
    return err
}

// Состояние строится заново из истории. Runtime проверяет хеш плана.
replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
```

Запуск полного примера:

```bash
go run ./examples/coffee-machine
```

Ожидаемый финансовый итог:

```text
принято:    350,00 ₽
возвращено: 120,00 ₽
выручка:    230,00 ₽
касса:      230,00 ₽
кредит:     0,00 ₽
напитков:   2
```

## Что показывает пример

| Свойство Axiom | Практический результат в автомате |
|---|---|
| Компиляция модели | Ошибки в именах полей, типах выражений и правилах обнаруживаются до приёма денег |
| `Claim` | Ошибка в формуле кассы или списании ингредиентов откатывает переход |
| Транзакционное хранилище | Деньги, ингредиенты и история не расходятся из-за частичной записи |
| Идемпотентная activity | Повтор задания не должен выдать второй напиток |
| Сериализация `execution ID` | Параллельные сигналы одного автомата не теряют обновления |
| История | Можно восстановить последовательность монет, продаж, сдачи и отмен |
| Replay | Состояние восстанавливается после замены контроллера или проверки журнала |
| Единый `Plan` | Ту же модель можно получать из Go, AXM или TOML |

# Выбор способа описания процесса

| Способ | Когда использовать | Отдельные файлы | Статический анализ |
|---|---|---:|---:|
| Typed Go Flow | Небольшая локальная машина состояний с произвольной логикой на Go | Нет | Нет |
| Декларативная Go-модель | Правила и инварианты должны проверяться до запуска | Нет | Полный |
| AXM | Сложная версионируемая модель хранится отдельно от приложения | Да | Полный |
| TOML | Процесс удобнее редактировать как таблицу переходов | Да | Полный |
| Низкоуровневый API | Требуется явное управление `Start`, `Signal`, `Patch` и `RunUntilIdle` | Необязательно | Зависит от модели |

## Минимальный Typed Go Flow

Для простых случаев декларативная модель не обязательна:

```go
type Counter struct {
    Count int
}

type Increment struct {
    By int
}

flow := axiom.NewFlow("counter", Counter{})

axiom.Handle(flow, func(
    _ context.Context,
    state Counter,
    event Increment,
) (axiom.FlowResult[Counter], error) {
    state.Count += event.By
    return axiom.Next(state), nil
})

engine, err := axiom.OpenFlow(flow)
if err != nil {
    return err
}

run := engine.Execution("counter-1")
if err := run.Dispatch(ctx, Increment{By: 2}); err != nil {
    return err
}

state, err := run.State(ctx)
```

`Flow` использует произвольный Go-код, поэтому библиотека не может построить для него полный граф зависимостей. Такой режим имеет уровень анализа `axiom.AnalysisOpaque`.

## AXM и TOML

AXM и TOML — способы получить `axiom.Plan`; исполнитель от формата не зависит.

```go
axmPlan, err := axm.Load("workflow.axm")
if err != nil {
    return err
}

axmEngine, err := axmPlan.New()

tablePlan, err := table.Load("workflow.toml")
if err != nil {
    return err
}

tableEngine, err := tablePlan.New()
```

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

Низкоуровневый API остаётся доступным:

```go
err := engine.Start(ctx, "coffee-machine-01", initialContext)
err = engine.Signal(ctx, "coffee-machine-01", "MoneyInserted", payload)
err = engine.Patch(ctx, "coffee-machine-01", patch)
result, err := engine.Query(ctx, "coffee-machine-01", "state")
err = engine.RunUntilIdle(ctx, "coffee-machine-01")
```

## Pebble и режимы записи

По умолчанию скомпилированный исполнитель хранит данные в памяти. Для сохранения на диске используется Pebble.

```go
// fsync при каждом commit
store, err := axiom.OpenPebble("data/axiom")

// Без fsync при каждом commit: быстрее, но последние записи могут быть потеряны
// при аварии процесса или компьютера.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleNoSync(),
)

// Периодический flush вместо fsync на каждой операции.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleSyncEvery(10*time.Millisecond),
)
```

`WithProductionMode` включает строгий быстрый исполнитель и требует транзакционного хранилища. Если модель использует неподдерживаемую конструкцию, создание `Engine` завершается ошибкой, а не переключается на медленный путь.

## Конкурентность

Гарантии действуют внутри одного процесса и одного экземпляра `Engine`:

- операции для одного `execution ID` сериализуются;
- разные `execution ID` могут выполняться параллельно;
- состояние и история записываются атомарно при использовании транзакционного хранилища;
- транзакции Pebble не подменяют общий объект хранилища внутри `Engine`;
- целые числа сохраняют тип после записи и повторного открытия Pebble.

Если несколько процессов могут изменять один и тот же `execution ID`, перед Axiom нужен маршрутизатор владельца, распределённая блокировка или хранилище с эквивалентными гарантиями.

## История, объяснение и replay

```go
history, err := engine.Execution("coffee-machine-01").History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
if err != nil {
    return err
}

explanation, err := engine.Execution("coffee-machine-01").Explain(ctx)
```

Для replay требуется та же версия плана, которая создала историю. Несовпадение хеша модуля считается ошибкой.

## Производительность

Текущий базовый прогон выполнен на общем GitHub runner: `linux/amd64`, Go 1.26.5, 4 логических CPU, конкурентность 8. Эти числа подходят для грубого поиска регрессий, но не являются независимым от оборудования SLA.

| Сценарий | p95 | p99 | Производительность |
|---|---:|---:|---:|
| Go Flow, разные экземпляры | 3,841 мс | 4,788 мс | 9 028 операций/с |
| Go Flow, один общий экземпляр | 20,777 мс | 24,880 мс | 772 операции/с |
| Скомпилированный исполнитель, разные экземпляры | 0,505 мс | 3,011 мс | 55 011 операций/с |
| Скомпилированный исполнитель, один общий экземпляр | 1,085 мс | 1,437 мс | 50 938 операций/с |
| Новый экземпляр в памяти | 0,800 мс | 4,058 мс | 40 239 операций/с |
| Pebble NoSync | 3,904 мс | 5,061 мс | 8 773 операции/с |
| Pebble Sync | 8,688 мс | 10,225 мс | 1 437 операций/с |
| Replay истории из 1 000 событий | 1,977 мс | 2,541 мс | 761 replay/с |

Подробный отчёт: [`benchmarks/latest.md`](benchmarks/latest.md).

Локальный запуск:

```bash
go run ./cmd/axiombench \
  -memory-ops 20000 \
  -pebble-ops 1000 \
  -replay-events 1000 \
  -replay-runs 200 \
  -concurrency 8 \
  -strict=true \
  -json benchmark-results.json \
  -markdown benchmark-results.md
```

## Что проверяет CI

- обычные тесты всех пакетов;
- race detector для исполнителя и хранилищ;
- конкурентные обновления одного экземпляра процесса;
- параллельные Pebble-транзакции;
- rollback при ошибке внешней операции;
- сохранение целочисленных типов;
- replay и проверку итогового состояния;
- нагрузочный тест на 16 workers и 8 000 операций;
- сборку отдельного пользовательского модуля только через публичные пакеты;
- строгий прогон p50, p95 и p99.

## Пакеты

| Пакет | Назначение |
|---|---|
| `github.com/Homiakus/axiom` | `Plan`, исполнитель, API экземпляра процесса и Typed Go Flow |
| `github.com/Homiakus/axiom/model` | Декларативная модель на Go |
| `github.com/Homiakus/axiom/axm` | Загрузка AXM |
| `github.com/Homiakus/axiom/table` | Загрузка таблиц переходов из TOML |
| `github.com/Homiakus/axiom/store/pebble` | Публичный пакет Pebble-хранилища |
| `github.com/Homiakus/axiom/cmd/axiomgen` | Необязательная генерация типизированных границ |
| `github.com/Homiakus/axiom/cmd/axiombench` | Нагрузочный тест и расчёт перцентилей |

## Разработка

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
```

## Лицензия

Apache-2.0. См. [`LICENSE`](LICENSE).
