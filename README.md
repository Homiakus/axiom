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
// Инициализируем новую доменную модель "CoffeeMachine" версии "1"
definition := model.New("CoffeeMachine").Version("1")

// Связываем структуру Machine со схемой состояния и задаём начальные значения (defaults)
machine := model.Bind[Machine](definition, "Machine").
    Default("CreditKopecks", 0).     // Кредит покупателя: 0 коп.
    Default("AcceptedKopecks", 0).   // Принятые деньги: 0 коп.
    Default("ReturnedKopecks", 0).   // Сдача/возвраты: 0 коп.
    Default("RevenueKopecks", 0).    // Выручка: 0 коп.
    Default("CashboxKopecks", 0).    // Физическая касса: 0 коп.
    Default("WaterML", 2000).        // Запас воды: 2000 мл
    Default("BeansG", 500).          // Запас зёрен: 500 г
    Default("MilkML", 1000).         // Запас молока: 1000 мл
    Default("Cups", 50).             // Запас стаканов: 50 шт
    Default("DrinksServed", 0)       // Выдано напитков: 0 шт

// Регистрируем типизированные события (имя события авто-выводится из Go-структуры)
moneyInserted := model.EventOf[MoneyInserted](definition)         // Внесение денег
espressoRequested := model.EventOf[EspressoRequested](definition)   // Запрос эспрессо
cappuccinoRequested := model.EventOf[CappuccinoRequested](definition) // Запрос капучино
cancelRequested := model.EventOf[CancelRequested](definition)       // Запрос отмены
```

## Приём денег

Все три счётчика записываются в рамках одного перехода.

```go
definition.Rule("acceptMoney").
    On(moneyInserted.Trigger()).                                                              // Триггер: событие внесения денег
    When(moneyInserted.Int("AmountKopecks").GT(0)).                                           // Условие: внесённая сумма > 0
    Set(machine.Int("CreditKopecks"), machine.Int("CreditKopecks").Add(moneyInserted.Int("AmountKopecks"))). // Увеличиваем кредит покупателя
    Set(machine.Int("AcceptedKopecks"), machine.Int("AcceptedKopecks").Add(moneyInserted.Int("AmountKopecks"))). // Увеличиваем общий счётчик принятых денег
    Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Add(moneyInserted.Int("AmountKopecks")))   // Увеличиваем кассу автомата
```

## Политика внешних операций

Приготовление напитка воздействует на насосы, нагреватель, кофемолку и механизм сдачи. Для таких операций задаётся политика выполнения.

```go
definition.Policy("hardwarePolicy").
    Retry(2).                     // До двух повторных попыток при физическом сбое
    Timeout(10 * time.Second).     // Таймаут одной попытки — 10 секунд
    Concurrency("once").           // Не выполнять операции параллельно для одного автомата
    Idempotency("required")        // Требовать обязательный ключ идемпотентности
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
definition.Activity("DispenseCappuccino").
    Input("purchaseId", cappuccinoRequested.String("PurchaseID")).                           // Передаём ID покупки
    Input("priceKopecks", cappuccinoPriceKopecks).                                          // Передаём цену капучино (140 ₽)
    Input("changeKopecks", machine.Int("CreditKopecks").Sub(cappuccinoPriceKopecks)).         // Передаём расчетную сдачу (Кредит - 140 ₽)
    Output("dispensed", "Bool").                                                            // Ожидаем подтверждение выдачи
    Output("priceKopecks", "Int").                                                           // Ожидаем подтверждённую цену
    Output("changeKopecks", "Int").                                                          // Ожидаем подтверждённую сдачу
    Effect("external").                                                                     // Операция является внешним физическим действием
    IdempotencyKey(cappuccinoRequested.String("PurchaseID")).                               // Ключ идемпотентности
    Policy("hardwarePolicy")                                                                // Связываем с политикой оборудования
```

Регистрация обработчика (использует строго типизированный `ActTyped`):

```go
axiom.ActTyped("DispenseCappuccino", func(
    ctx context.Context,
    input DispenseInput, // Строго типизированный входной DTO (без map[string]any и приведения типов!)
) (DispenseOutput, error) {
    // Здесь выполняются команды контроллеру оборудования без неудобных мап и приведения типов.
    return DispenseOutput{
        Dispensed:     true,                // Подтверждаем успешность приготовления
        PriceKopecks:  input.PriceKopecks,  // Возвращаем подтверждённую цену
        ChangeKopecks: input.ChangeKopecks, // Возвращаем подтверждённую сдачу
    }, nil
})
```

Если обработчик завершится ошибкой, правило не должно учитывать выручку и списывать ингредиенты.

## Продажа капучино

```go
definition.Rule("sellCappuccino").
    On(cappuccinoRequested.Trigger()).                                                       // Триггер: нажата кнопка капучино
    When(                                                                                    // Проверяем инварианты/гарды:
        machine.Int("CreditKopecks").GTE(14000),                                             //   - Кредит покупателя >= 140 ₽
        machine.Int("WaterML").GTE(60),                                                      //   - Вода >= 60 мл
        machine.Int("BeansG").GTE(10),                                                       //   - Кофе >= 10 г
        machine.Int("MilkML").GTE(120),                                                      //   - Молоко >= 120 мл
        machine.Int("Cups").GTE(1),                                                          //   - Стаканы >= 1 шт
    ).
    Run("DispenseCappuccino").                                                               // Запускаем физическую операцию вызова оборудования
    Set(machine.Int("CreditKopecks"), 0).                                                    // Сбрасываем кредит покупателя в 0
    Set(machine.Int("ReturnedKopecks"), machine.Int("ReturnedKopecks").Add(model.OutputInt("changeKopecks"))). // Учитываем выданную сдачу
    Set(machine.Int("RevenueKopecks"), machine.Int("RevenueKopecks").Add(model.OutputInt("priceKopecks"))).     // Учитываем признанную выручку
    Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Sub(model.OutputInt("changeKopecks"))).   // Уменьшаем кассу на сумму сдачи
    Set(machine.Int("WaterML"), machine.Int("WaterML").Sub(60)).                             // Списываем 60 мл воды
    Set(machine.Int("BeansG"), machine.Int("BeansG").Sub(10)).                               // Списываем 10 г кофе
    Set(machine.Int("MilkML"), machine.Int("MilkML").Sub(120)).                             // Списываем 120 мл молока
    Set(machine.Int("Cups"), machine.Int("Cups").Sub(1)).                                   // Списываем 1 стакан
    Set(machine.Int("DrinksServed"), machine.Int("DrinksServed").Add(1)).                   // Инкрементируем счётчик проданных напитков
    Set(machine.String("LastDrink"), "cappuccino").                                         // Записываем название последнего напитка
    Set(machine.Int("LastChangeKopecks"), model.OutputInt("changeKopecks")).                // Записываем сдачу для технического журнала
    Set(machine.Bool("LastDispensed"), model.OutputBool("dispensed"))                       // Записываем статус выдачи для журнала
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
    // Проверка бухгалтерского инварианта: Принято == Возвращено + (Выручка + Кредит)
    machine.Int("AcceptedKopecks").EQ(
        machine.Int("ReturnedKopecks").Add(
            machine.Int("RevenueKopecks").Add(machine.Int("CreditKopecks")),
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
    // Проверка сверки кассы: Физическая Касса == Выручка + Текущий Кредит
    machine.Int("CashboxKopecks").EQ(
        machine.Int("RevenueKopecks").Add(machine.Int("CreditKopecks")),
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
    axiom.ActTyped("DispenseEspresso", dispenseEspresso),
    axiom.ActTyped("DispenseCappuccino", dispenseCappuccino),
    axiom.ActTyped("ReturnMoney", returnMoney),
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
