# Axiom

**Русский** · [English](README.en.md)

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**Axiom — Go-библиотека для проверяемых переходов состояния, бизнес-процессов и таблиц решений.**

Она нужна там, где недостаточно просто изменить struct в памяти: переход должен иметь явные правила, пройти инварианты, записаться в историю, безопасно вызвать внешний эффект и оставаться объяснимым после выполнения.

Для нового Go-проекта рекомендуемый frontend — пакет [`model`](model/).

## Модель за 30 секунд

Основной declarative lifecycle один и тот же независимо от того, где описан процесс:

```text
Go model / AXM / TOML
         ↓
      axiom.Plan
         ↓
       Engine
         ↓
Run = engine.Execution(id)
```

- **Definition** описывает состояние, события, rules, claims, activities и policies.
- **Plan** — canonical compiled representation.
- **Engine** объединяет Plan, store, activity implementations и runtime options.
- **Run** — основной handle одного durable execution: `Dispatch`, `State`, `Status`, `History`, `Explain`, `Cancel`.

`axiom.Flow` существует отдельно как компактный typed reducer API для сценариев, которым не нужен статический граф модели.

## Когда Axiom подходит

Хорошие кандидаты: заказы, заявки, оборудование, технологические циклы, партии продукции, платежи, approval flows и другие объекты с собственным lifecycle.

Axiom особенно полезен, если нужны одновременно:

- явные допустимые переходы;
- инварианты (`claim`), которые нельзя нарушить;
- воспроизводимая history/replay модель;
- внешние activities с retry/timeout/idempotency;
- объяснение текущего состояния и выполненных решений;
- один runtime для Go model, AXM и TOML definitions.

Axiom **не** является брокером сообщений, распределённым scheduler, distributed lock manager или заменой простого CRUD.

## Какой API выбрать

| Frontend | Пакет | Выбирать когда | Static analysis |
|---|---|---|---:|
| Declarative Go | `github.com/Homiakus/axiom/model` | **по умолчанию для нового Go-кода** | Да |
| Typed Go Flow | `github.com/Homiakus/axiom` | маленький reducer, важнее произвольный Go | Нет (`opaque`) |
| AXM | `github.com/Homiakus/axiom/axm` | definition должна жить вне Go | Да |
| TOML table | `github.com/Homiakus/axiom/table` | задача естественно является decision table | Да |

Подробный decision guide: [`docs/api-guide.md`](docs/api-guide.md).

## Требования и установка

- Go 1.26+.
- Для in-memory режима не нужны внешние сервисы.
- Для durable storage есть встроенная интеграция с CockroachDB Pebble.

```bash
go get github.com/Homiakus/axiom
```

До первого стабильного `v1` действует pre-v1 compatibility policy: [`docs/versioning.md`](docs/versioning.md).

## Быстрый старт

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Homiakus/axiom"
    "github.com/Homiakus/axiom/model"
)

type Counter struct {
    Value int `json:"value"`
}

type SetValue struct {
    Value int `json:"value"`
}

func main() {
    definition := model.New("Counter")
    current := model.Bind[Counter](definition, "Current")
    setValue := model.EventOf[SetValue](definition)

    definition.Rule("set").
        On(setValue.Trigger()).
        Set(current.Int("Value"), setValue.Int("Value"))

    definition.Claim(
        "nonNegative",
        current.Int("Value").GreaterOrEqual(0),
    )

    engine, err := axiom.Open(definition)
    if err != nil {
        log.Fatal(err)
    }

    run := engine.Execution("counter-1")
    ctx := context.Background()

    if err := run.Dispatch(ctx, SetValue{Value: 7}); err != nil {
        log.Fatal(err)
    }

    var state Counter
    if err := run.State(ctx, &state); err != nil {
        log.Fatal(err)
    }

    fmt.Println(state.Value) // 7
}
```

`axiom.Open(definition)` компилирует `model.Definition` в `Plan` и создаёт `Engine`. `Dispatch` создаёт execution при первом обращении, применяет событие и drain'ит доступные inline activities до idle или до durable retry boundary.

## Большие модели: меньше строковых имён

Короткие helpers вроде `order.Int("Total")` удобны в маленькой модели. Когда одно поле используется десятки раз, строка начинает размножаться по rules/claims/activities и хуже переживает рефакторинг.

Для этого есть reusable typed field keys:

```go
type Order struct {
    Status string `json:"status"`
    Total  int    `json:"total"`
}

var (
    orderStatus = model.Key[Order, string]("Status")
    orderTotal  = model.Key[Order, int]("Total")
)

definition := model.New("Orders")
order := model.Bind[Order](definition, "Order")

status := model.StateField(order, orderStatus)
total := model.StateField(order, orderTotal)

model.StateDefault(order, orderStatus, "new")
definition.Claim("totalNonNegative", total.GreaterOrEqual(0))
```

`FieldKey[Owner, Value]` локализует имя поля в одном месте. Owner type не позволяет применить ключ к чужому state/event type, а Value type сверяется с реальным Go field при использовании. Для optional pointer fields можно использовать pointed-to logical type.

Для событий используется `model.EventField`, для `changed(...)` trigger — `model.StateChanged`.

Это **не code generation**: имена по-прежнему связываются через reflection и `axiom`/`json` tags, но typo surface и повторение строк заметно уменьшаются.

## Typed expressions

`TypedField[T]` сохраняет compatibility operators (`EQ`, `GT`, `Add` и др.), но новый код лучше писать через строгие helpers:

```go
total.GreaterOrEqual(0)
status.Equal("paid")
left.EqualField(right)
subtotal.PlusField(tax)
```

Literal helpers принимают тот же `T`, а field-to-field helpers требуют одинаковый `TypedField[T]`. Поэтому часть ошибок ловится компилятором Go ещё до compilation модели.

## Activities

Для application code предпочитайте `ActTyped`:

```go
type ChargeInput struct {
    OrderID string `json:"orderId"`
    Amount  int    `json:"amount"`
}

type ChargeOutput struct {
    PaymentID string `json:"paymentId"`
}

engine, err := axiom.Open(
    definition,
    axiom.ActTyped("Charge", func(
        ctx context.Context,
        input ChargeInput,
    ) (ChargeOutput, error) {
        return ChargeOutput{PaymentID: "pay-1"}, nil
    }),
)
```

Input/output `ActTyped` должны быть struct, pointer-to-struct или map со string keys. Unsupported shape и nil handler отклоняются при создании Engine (`AX507`), а не превращаются в позднюю ошибку activity.

`axiom.Act` с `axiom.Input` / `axiom.Output` оставлен для dynamic integration boundaries, где `map[string]any` уже является естественным контрактом.

## Durable storage и production mode

По умолчанию используется memory store. Для Pebble:

```go
store, err := axiom.OpenPebble("data/axiom")
if err != nil {
    return err
}
defer store.Close()

engine, err := axiom.Open(
    definition,
    axiom.WithStore(store),
    axiom.WithProductionMode(),
)
```

`WithProductionMode()` требует `TransactionalStore` и включает strict fast runtime.

Текущие activity guarantees:

- `retry` сохраняет `Attempt`, `MaxAttempts` и `NextAttemptAt` в store и может продолжиться новым Engine после process restart при durable store;
- `timeout` применяется к каждой попытке отдельно;
- `parallel` не добавляет сериализацию;
- `once` сериализует activity внутри одного Engine;
- `first` оставляет первый active task в lane `execution + activity`;
- `latest` заменяет более старые **pending** tasks, но не пытается небезопасно force-cancel уже running Go handler;
- external activity всё равно должна быть идемпотентной: durable retry даёт at-least-once execution, а не exactly-once внешний эффект.

Детальный контракт: [`docs/runtime-semantics.md`](docs/runtime-semantics.md).

## Runtime API

Для одного execution используйте `Run`:

```go
run := engine.Execution("order-42")

if err := run.Dispatch(ctx, Submitted{Total: 1500}); err != nil {
    return err
}

var state Order
if err := run.State(ctx, &state); err != nil {
    return err
}

status, err := run.Status(ctx)
history, err := run.History(ctx)
explanation, err := run.Explain(ctx)
```

Также доступны `Signal`, `Patch`, `PendingActivities` и `Cancel`. Низкоуровневые Engine methods, где execution ID приходится передавать каждый раз, в основном нужны integration/tooling слоям.

## Примеры

Каталог [`examples/`](examples/) теперь является runnable learning path:

| Пример | Команда | Назначение |
|---|---|---|
| `model` | `go run ./examples/model` | рекомендуемый declarative Go API |
| `go-first` | `go run ./examples/go-first` | typed reducer Flow |
| `order` | `go run ./examples/order` | Pebble + production activity semantics |
| `axiom-files` | `go run ./examples/axiom-files` | AXM file frontend |
| `table` | `go run ./examples/table` | TOML decision table frontend |
| `triz` | `go run ./examples/triz` | normalization + diagnostics + source map |
| `coffee-machine` | `go run ./examples/coffee-machine` | большой end-to-end reference |

Подробности: [`examples/README.md`](examples/README.md).

## Code generation

`axiomgen` генерирует typed activity boundary из AXM/TOML:

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

Подробнее: [`docs/axiomgen.md`](docs/axiomgen.md).

## Проверка проекта

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
go run ./examples/coffee-machine
```

CI дополнительно выполняет vulnerability scan, fuzz smoke tests, внешний consumer module и performance job. Это важно для библиотеки: публичный API проверяется не только внутренними тестами, но и из отдельного downstream Go module.

Benchmark runner и актуальный baseline: [`benchmarks/latest.md`](benchmarks/latest.md).

## Границы гарантий

1. Lock одного `execution ID` действует внутри одного `Engine`, а не является distributed ownership protocol.
2. `once` также локален одному Engine; `first/latest` атомарны в пределах гарантий выбранного `TransactionalStore`.
3. `latest` означает **latest pending wins**, а не force-cancel произвольного running Go handler.
4. Durable retry не делает внешний side effect exactly-once — idempotency остаётся обязанностью integration boundary.
5. `Flow` выполняет effects до `FlowStore.Save`; effect handlers должны быть идемпотентными.
6. Memory store подходит для разработки/тестов и не переживает restart процесса.

## Документация

- [Навигация](docs/README.md)
- [Public API guide](docs/api-guide.md)
- [Runtime semantics](docs/runtime-semantics.md)
- [Versioning и compatibility](docs/versioning.md)
- [AXM specification](docs/axiom-file-specification.md)
- [axiomgen](docs/axiomgen.md)
- [Architecture](ARCHITECTURE.md)
- [Development](DEVELOPMENT.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Examples](examples/README.md)

## License

Apache-2.0. См. [`LICENSE`](LICENSE).
