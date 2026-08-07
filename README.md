# Axiom

**Русский** · [English](README.en.md)

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom — библиотека Go для моделирования переходов состояния, бизнес-процессов и таблиц решений.

Она объединяет несколько способов описания процесса с единым исполняемым представлением `axiom.Plan`:

- декларативная Go-модель (`model`) — **рекомендуемый старт для новых Go-проектов**;
- типизированный Go reducer (`axiom.Flow`) — для компактной логики без необходимости статического анализа;
- файловый DSL AXM (`axm`) — когда модель должна храниться отдельно от Go-кода;
- таблицы решений TOML (`table`) — для задач, естественно описываемых decision table.

Компилируемый runtime хранит состояние, историю и задания activity, проверяет инварианты (`claim`), поддерживает replay и может использовать транзакционное Pebble-хранилище.

Подробная схема выбора frontend: [`docs/api-guide.md`](docs/api-guide.md).

## Когда использовать

Axiom подходит для объектов с собственным жизненным циклом: заказов, заявок, оборудования, партий продукции, платёжных операций и других процессов, где переход необходимо проверить, записать и объяснить.

Axiom не заменяет:

- обычный CRUD без переходов и инвариантов;
- брокер сообщений;
- распределённый планировщик;
- межпроцессную или распределённую блокировку.

Операции одного `execution ID` сериализуются только внутри одного экземпляра `Engine`. Владение execution между процессами приложение организует отдельно.

## Требования

- Go 1.26 или новее.
- Для запуска с памятью внешние сервисы и переменные окружения не требуются.
- Для долговременного хранения используется встроенная интеграция с CockroachDB Pebble.

## Установка

В существующий Go-модуль:

```bash
go get github.com/Homiakus/axiom
```

До первого стабильного `v1` публичный API развивается по pre-v1 SemVer policy. Правила совместимости и release checklist: [`docs/versioning.md`](docs/versioning.md).

Для разработки самой библиотеки:

```bash
git clone https://github.com/Homiakus/axiom.git
cd axiom
go test ./...
```

Все команды разработки выполняются из корня репозитория, если явно не указано иное.

## Быстрый старт: декларативная Go-модель

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

    definition.Claim("nonNegative", current.Int("Value").GreaterOrEqual(0))

    engine, err := axiom.Open(definition)
    if err != nil {
        log.Fatal(err)
    }

    run := engine.Execution("counter-1")
    if err := run.Dispatch(context.Background(), SetValue{Value: 7}); err != nil {
        log.Fatal(err)
    }

    var state Counter
    if err := run.State(context.Background(), &state); err != nil {
        log.Fatal(err)
    }

    fmt.Println(state.Value) // 7
}
```

`Dispatch` создаёт execution при первом обращении, отправляет типизированное событие и выполняет доступные inline activities до состояния idle.

Для сравнений typed fields с Go-значениями новый код рекомендуется писать через `Equal`, `GreaterOrEqual`, `LessThan` и другие строгие helpers: literal имеет тот же `T`, поэтому часть ошибок ловит уже компилятор Go.

## Долговременное хранение

По умолчанию `Plan.New()` / `axiom.Open()` используют хранилище в памяти. Для Pebble:

```go
store, err := axiom.OpenPebble("data/axiom")
if err != nil {
    return err
}
defer store.Close()

engine, err := axiom.Open(
    definition,
    axiom.WithStore(store),
)
```

Режим `axiom.WithProductionMode()` дополнительно требует хранилище, реализующее `TransactionalStore`, и включает строгий fast runtime. Модель, не поддерживаемая строгим runtime, будет отклонена при создании `Engine`.

`retry` и `timeout` теперь исполняются runtime вокруг activity handler. `concurrency: once` сериализует вызовы одной activity внутри конкретного `Engine`, а `parallel` не добавляет сериализацию. `concurrency: latest/first` пока отклоняются production mode с `AX508`, поскольку для них требуется корректная durable task-supersession semantics.

## Activity

Для прикладного кода предпочитайте типизированные обработчики:

```go
engine, err := axiom.Open(
    definition,
    axiom.ActTyped("SendEmail", func(
        ctx context.Context,
        input SendEmailInput,
    ) (SendEmailOutput, error) {
        // Вызов внешней системы должен быть идемпотентным.
        return SendEmailOutput{Sent: true}, nil
    }),
)
```

`ActTyped` принимает struct / pointer-to-struct или map со строковыми ключами для входа и выхода. Неподходящая typed-сигнатура или nil handler отклоняются при создании `Engine` (`AX507`), а не проявляются поздней ошибкой во время activity.

`axiom.Act` с `axiom.Input` / `axiom.Output` остаётся удобным для динамических integration boundaries, где payload уже представлен как `map[string]any`.

Для activity с `effect: external` компилятор требует policy с `idempotency: required` и `idempotencyKey`. Это обеспечивает дедупликацию заданий в используемом store, но не является гарантией exactly-once во внешней системе.

## Поддерживаемые способы описания процесса

| Способ | Пакет | Когда выбирать | Статический анализ | Хранение модели |
|---|---|---|---:|---|
| Declarative Go | `github.com/Homiakus/axiom/model` | **По умолчанию для нового Go-кода** | Да (`static`) | Go-код |
| Typed Go Flow | `github.com/Homiakus/axiom` | Маленький reducer / произвольная Go-логика | Нет (`opaque`) | Go-код |
| AXM | `github.com/Homiakus/axiom/axm` | Модель вне Go / tooling | Да (`static`) | `.axm` |
| TOML | `github.com/Homiakus/axiom/table` | Decision tables | Да (`static`) | `.toml` |

Все декларативные frontends компилируются в `axiom.Plan`. Typed Go Flow использует отдельный reducer runtime и не преобразуется в статический граф зависимостей.

## Runtime API

Для одного процесса предпочитайте handle `Run`:

```go
run := engine.Execution("order-42")

err := run.Dispatch(ctx, Event{...})
err = run.State(ctx, &state)
status, err := run.Status(ctx)
history, err := run.History(ctx)
explanation, err := run.Explain(ctx)
```

`Run` также предоставляет `Signal`, `Patch`, `PendingActivities` и `Cancel`. Низкоуровневые методы `Engine`, требующие повторной передачи execution ID, в основном полезны для integration/tooling слоёв.

## Основные команды

```bash
# Проверить зависимости
go mod tidy
git diff --exit-code -- go.mod go.sum

# Запустить все тесты
go test ./...

# Проверить гонки в критичных пакетах
go test -race . ./internal/runtime/... ./internal/store/...

# Статический анализ Go
go vet ./...

# Запустить проверяемый пример
go run ./examples/coffee-machine
```

Ожидаемые денежные итоги примера проверяются в GitHub Actions. CI также собирает внешний consumer module, чтобы публичные пакеты проверялись с точки зрения реального пользователя библиотеки.

## Кодогенератор

`axiomgen` генерирует типизированную границу activity из AXM или TOML:

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

Команда печатает JSON-отчёт о созданных, обновлённых и пропущенных файлах. Подробнее: [`docs/axiomgen.md`](docs/axiomgen.md).

## Производительность

В репозитории есть воспроизводимый benchmark runner:

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

Актуальный baseline: [`benchmarks/latest.md`](benchmarks/latest.md). Значения зависят от оборудования и не являются SLA.

## Важные текущие ограничения

1. `retry` выполняется как немедленный in-process повтор handler и пока не является durable task-level retry с backoff/`NextAttemptAt` и отдельной history-записью на каждую попытку.
2. `concurrency: once` действует внутри одного `Engine`; `latest/first` пока не имеют безопасной supersession semantics и отклоняются production mode.
3. Блокировка одного `execution ID` действует внутри одного `Engine`, а не между процессами.
4. Typed Go Flow выполняет effects перед вызовом `FlowStore.Save`. Обработчики effects должны быть идемпотентными, а пользовательский store — учитывать возможную ошибку сохранения после внешнего эффекта.
5. In-memory store предназначен для разработки и тестов; он не обеспечивает восстановление после перезапуска процесса.

Подробности и границы гарантий: [`ARCHITECTURE.md`](ARCHITECTURE.md) и [`docs/runtime-semantics.md`](docs/runtime-semantics.md).

## Документация

- [Навигация по документации](docs/README.md)
- [Выбор публичного API](docs/api-guide.md)
- [Версионирование и совместимость](docs/versioning.md)
- [Каталог примеров](examples/README.md)
- [Архитектура](ARCHITECTURE.md)
- [Локальная разработка](DEVELOPMENT.md)
- [Правила внесения изменений](CONTRIBUTING.md)
- [Политика безопасности](SECURITY.md)
- [Спецификация AXM](docs/axiom-file-specification.md)
- [Кодогенератор](docs/axiomgen.md)
- [Runtime-семантика и ограничения](docs/runtime-semantics.md)

## Лицензия

Apache-2.0. См. [`LICENSE`](LICENSE).
