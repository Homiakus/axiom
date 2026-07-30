# Axiom

**Русский** · [English](README.en.md)

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Axiom — библиотека Go для моделирования переходов состояния, бизнес-процессов и таблиц решений.

Она объединяет несколько способов описания процесса с единым исполняемым представлением `axiom.Plan`:

- типизированный Go reducer (`axiom.Flow`);
- декларативная Go-модель (`model`);
- файловый DSL AXM (`axm`);
- таблицы решений TOML (`table`).

Компилируемый runtime хранит состояние, историю и задания activity, проверяет инварианты (`claim`), поддерживает replay и может использовать транзакционное Pebble-хранилище.

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
    current := model.State[Counter](definition, "Current")
    setValue := model.Event[SetValue](definition, "SetValue")

    definition.Rule("set").
        On(setValue.Trigger()).
        Set(current.Field("Value"), setValue.Field("Value"))

    plan, err := definition.Compile()
    if err != nil {
        log.Fatal(err)
    }

    engine, err := plan.New()
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

## Долговременное хранение

По умолчанию `Plan.New()` использует хранилище в памяти. Для Pebble:

```go
store, err := axiom.OpenPebble("data/axiom")
if err != nil {
    return err
}
defer store.Close()

engine, err := plan.New(axiom.WithStore(store))
```

Режим `axiom.WithProductionMode()` дополнительно требует хранилище, реализующее `TransactionalStore`, и включает строгий fast runtime. Модель, не поддерживаемая строгим runtime, будет отклонена при создании `Engine`.

## Activity

Внешние операции регистрируются как Go-функции:

```go
engine, err := plan.New(
    axiom.ActTyped("SendEmail", func(
        ctx context.Context,
        input SendEmailInput,
    ) (SendEmailOutput, error) {
        // Вызов внешней системы должен быть идемпотентным.
        return SendEmailOutput{Sent: true}, nil
    }),
)
```

Для activity с `effect: external` компилятор требует policy с `idempotency: required` и `idempotencyKey`. Это обеспечивает дедупликацию заданий в используемом store, но не является гарантией exactly-once во внешней системе.

## Поддерживаемые способы описания процесса

| Способ | Пакет | Статический анализ | Хранение модели |
|---|---|---:|---|
| Typed Go Flow | `github.com/Homiakus/axiom` | Нет (`opaque`) | Go-код |
| Декларативная Go-модель | `github.com/Homiakus/axiom/model` | Да (`static`) | Go-код |
| AXM | `github.com/Homiakus/axiom/axm` | Да (`static`) | `.axm` |
| TOML | `github.com/Homiakus/axiom/table` | Да (`static`) | `.toml` |

Все декларативные frontends компилируются в `axiom.Plan`. Typed Go Flow использует отдельный reducer runtime и не преобразуется в статический граф зависимостей.

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

Ожидаемые денежные итоги примера проверяются в GitHub Actions.

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

1. `policy.retry`, `policy.timeout` и `policy.concurrency` присутствуют в модели, но не должны считаться полностью реализованными runtime-гарантиями. В текущем inline runtime ошибка activity переводит task и execution в `Failed`; автоматический повтор после ошибки и таймаут вокруг вызова activity не выполняются.
2. Блокировка одного `execution ID` действует внутри одного `Engine`, а не между процессами.
3. Typed Go Flow выполняет effects перед вызовом `FlowStore.Save`. Обработчики effects должны быть идемпотентными, а пользовательский store — учитывать возможную ошибку сохранения после внешнего эффекта.
4. In-memory store предназначен для разработки и тестов; он не обеспечивает восстановление после перезапуска процесса.

Подробности и границы гарантий: [`ARCHITECTURE.md`](ARCHITECTURE.md) и [`docs/runtime-semantics.md`](docs/runtime-semantics.md).

## Документация

- [Навигация по документации](docs/README.md)
- [Архитектура](ARCHITECTURE.md)
- [Локальная разработка](DEVELOPMENT.md)
- [Правила внесения изменений](CONTRIBUTING.md)
- [Политика безопасности](SECURITY.md)
- [Спецификация AXM](docs/axiom-file-specification.md)
- [Кодогенератор](docs/axiomgen.md)
- [Runtime-семантика и ограничения](docs/runtime-semantics.md)

## Лицензия

Apache-2.0. См. [`LICENSE`](LICENSE).
