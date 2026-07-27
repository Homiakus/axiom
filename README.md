# Axiom

**Русский** · [English](README.en.md)

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom — библиотека для описания и исполнения переходов состояния, бизнес-процессов и таблиц решений на Go.

Она нужна там, где недостаточно просто изменить структуру в памяти. Axiom связывает в одной модели:

- типизированные события;
- правила изменения состояния;
- проверяемые инварианты;
- внешние операции с политиками повторов и идемпотентности;
- транзакционное хранение;
- историю, объяснение результата и повторное воспроизведение.

Файлы описания не обязательны. Процесс можно задать обычным Go-кодом, декларативной Go-моделью, AXM или TOML. Декларативные варианты компилируются в один `axiom.Plan` и используют общий исполнитель.

## Когда Axiom подходит

Axiom полезен, если состояние принадлежит конкретной сущности — заказу, заявке, устройству, партии продукции — и каждое изменение должно быть проверено и записано.

Типичные задачи:

- обработка заказов и платежей;
- согласования и многошаговые заявки;
- управление состояниями оборудования;
- фоновые операции с повторами;
- таблицы решений;
- восстановление состояния из истории;
- аудит причин, по которым сработало правило.

## Когда Axiom не нужен

Не стоит использовать Axiom для простого CRUD без переходов и инвариантов. Библиотека также не заменяет очередь сообщений, распределённый планировщик или систему межпроцессной блокировки. Сериализация одного `execution ID` гарантируется внутри одного экземпляра `Engine`; распределённое владение нужно организовать отдельно.

## Установка

Требуется Go 1.26 или новее.

```bash
go get github.com/Homiakus/axiom
```

## Основной пример: оплата заказа и отправка чека

В примере есть один заказ и два события:

1. `OrderCreated` записывает номер заказа, адрес покупателя и сумму.
2. `PaymentCaptured` записывает идентификатор платежа и переводит заказ в оплаченное состояние.
3. После оплаты запускается внешняя операция `SendReceipt`.
4. Инварианты запрещают оплаченное состояние без идентификатора платежа и отправленный чек без оплаты.
5. Состояние и история сохраняются в Pebble и могут быть восстановлены повторным воспроизведением.

Ниже показана основная часть модели. Полный запускаемый пример находится в [`examples/order/main.go`](examples/order/main.go).

```go
definition := model.New("Order").Version("1")

order := model.State[Order](definition, "Order").
    Default("Paid", false).
    Default("ReceiptSent", false)

created := model.Event[OrderCreated](definition, "OrderCreated")
captured := model.Event[PaymentCaptured](definition, "PaymentCaptured")

definition.Policy("receiptPolicy").
    Retry(3).
    Timeout(3 * time.Second).
    Concurrency("once").
    Idempotency("required")

definition.Activity("SendReceipt").
    Input("orderId", order.Field("ID")).
    Input("email", order.Field("CustomerEmail")).
    Input("paymentId", order.Field("PaymentID")).
    Output("sent", "Bool").
    Effect("external").
    IdempotencyKey(order.Field("PaymentID")).
    Policy("receiptPolicy")

definition.Rule("createOrder").
    On(created.Trigger()).
    Set(order.Field("ID"), created.Field("OrderID")).
    Set(order.Field("CustomerEmail"), created.Field("CustomerEmail")).
    Set(order.Field("Total"), created.Field("Total"))

definition.Rule("capturePayment").
    On(captured.Trigger()).
    Set(order.Field("PaymentID"), captured.Field("PaymentID")).
    Set(order.Field("Paid"), model.Lit(true))

definition.Rule("sendReceipt").
    On(order.Changed("Paid")).
    When(model.Eq(order.Field("Paid"), model.Lit(true))).
    Run("SendReceipt").
    Set(order.Field("ReceiptSent"), model.Ref("output.sent"))

definition.Claim(
    "paidOrderHasPaymentID",
    model.Implies(
        model.Eq(order.Field("Paid"), model.Lit(true)),
        model.Exists(order.Field("PaymentID")),
    ),
)

definition.Claim(
    "receiptRequiresPayment",
    model.Implies(
        model.Eq(order.Field("ReceiptSent"), model.Lit(true)),
        model.Eq(order.Field("Paid"), model.Lit(true)),
    ),
)
```

Компиляция модели, подключение хранилища и исполнение событий:

```go
plan, err := definition.Compile()
if err != nil {
    return err
}

store, err := axiom.OpenPebble("data/orders")
if err != nil {
    return err
}
defer store.Close()

engine, err := plan.New(
    axiom.WithStore(store),
    axiom.Act("SendReceipt", sendReceipt),
)
if err != nil {
    return err
}

run := engine.Execution("order-42")

if err := run.Dispatch(ctx, OrderCreated{
    OrderID:       "order-42",
    CustomerEmail: "customer@example.com",
    Total:         12900,
}); err != nil {
    return err
}

if err := run.Dispatch(ctx, PaymentCaptured{
    PaymentID: "pay-9001",
}); err != nil {
    return err
}

var state Order
if err := run.State(ctx, &state); err != nil {
    return err
}

history, err := run.History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
```

### Что показывает этот пример

- Ошибки в именах полей, типах выражений, правилах и инвариантах обнаруживаются при `Compile`, до обработки событий.
- Если переход нарушает `Claim`, новое состояние и история не фиксируются.
- Внешняя операция отделена от правил и получает явную политику повторов и ключ идемпотентности.
- `Dispatch` создаёт экземпляр процесса при первом событии и выполняет встроенные операции до состояния ожидания.
- Два параллельных вызова для одного `execution ID` выполняются последовательно и не теряют обновления.
- История содержит входные события, записи состояния и результаты внешних операций.
- `ReplayFromHistory` восстанавливает состояние с той же версией скомпилированного плана.

Запуск полного примера:

```bash
go run ./examples/order
```

## Выбор способа описания процесса

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

AXM и TOML — только способы получить `axiom.Plan`; исполнитель от формата не зависит.

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
run := engine.Execution("order-42")

err := run.Dispatch(ctx, OrderCreated{OrderID: "order-42"})

var state OrderState
err = run.State(ctx, &state)

status, err := run.Status(ctx)
history, err := run.History(ctx)
pending, err := run.PendingActivities(ctx)
explanation, err := run.Explain(ctx)
err = run.Cancel(ctx)
```

Низкоуровневый API остаётся доступным:

```go
err := engine.Start(ctx, "order-42", initialContext)
err = engine.Signal(ctx, "order-42", "OrderCreated", payload)
err = engine.Patch(ctx, "order-42", patch)
result, err := engine.Query(ctx, "order-42", "state")
err = engine.RunUntilIdle(ctx, "order-42")
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
history, err := engine.Execution("order-42").History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
if err != nil {
    return err
}

explanation, err := engine.Execution("order-42").Explain(ctx)
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
