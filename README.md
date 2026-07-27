# Axiom

**Русский** · [English](README.en.md)

[![CI](https://github.com/Homiakus/axiom/actions/workflows/test.yml/badge.svg)](https://github.com/Homiakus/axiom/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

**Axiom — детерминированный движок переходов состояния, бизнес-процессов и принятия решений для Go.**

Для быстрого старта используйте обычные типизированные Go-reducer-функции. Когда важны статическая проверка и анализ зависимостей — декларативную Go-модель. Если описание процесса должно храниться отдельно от приложения — AXM или TOML. Все статически анализируемые frontends компилируются в единый канонический `axiom.Plan` и исполняются одним детерминированным runtime.

Axiom предназначен для систем, где изменения состояния должны быть объяснимыми, воспроизводимыми и безопасными при конкурентной работе: бизнес-процессы, управляющая логика, согласования, оркестрация, таблицы решений и надёжные фоновые операции.

## Почему Axiom

- **Go-first:** можно начать без DSL и сгенерированных файлов.
- **Детерминированность:** одинаковые план, состояние и событие дают одинаковый результат перехода.
- **Типизированные границы:** вместо ручных `map[string]any` отправляются именованные Go-структуры.
- **Статическая проверка:** декларативные модели, AXM и TOML проверяются до запуска.
- **Надёжное исполнение:** встроенное Pebble-хранилище обеспечивает транзакционную персистентность.
- **Replay и аудит:** состояние восстанавливается из истории, а причины срабатывания правил можно изучить.
- **Безопасная конкуренция:** изменения одного execution сериализуются, независимые execution выполняются параллельно.
- **Activities:** внешние побочные эффекты изолируются в зарегистрированных Go-обработчиках.
- **Claims:** инварианты проверяются как часть модели переходов.
- **Impact analysis:** можно сравнивать скомпилированные bundles и определять затронутые правила и поля.

## Установка

Текущая версия Axiom требует Go 1.26 или новее.

```bash
go get github.com/Homiakus/axiom
```

## Выбор API

| API | Для каких задач | Нужны файлы | Статический анализ |
|---|---|---:|---:|
| Typed Go Flow | Локальные reducer-функции, команды и конечные автоматы | Нет | Непрозрачный |
| Declarative Go Model | Проверяемые процессы, полностью описанные на Go | Нет | Полный |
| AXM frontend | Богатые версионируемые описания процессов | Да | Полный |
| TOML table frontend | Таблицы переходов, хранящиеся как конфигурация | Да | Полный |
| Low-level runtime | Существующие интеграции и явное управление жизненным циклом | Необязательно | Зависит от источника |

## Самый быстрый старт: типизированный Go Flow

Flow — это типизированный reducer с необязательными effects и claims. Это самый компактный API, не требующий отдельного файла схемы.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Homiakus/axiom"
)

type Counter struct {
    Count int `json:"count"`
}

type Increment struct {
    By int `json:"by"`
}

type LogCount struct {
    Count int
}

func main() {
    ctx := context.Background()

    flow := axiom.NewFlow("counter", Counter{})

    axiom.Handle(flow, func(
        _ context.Context,
        state Counter,
        event Increment,
    ) (axiom.FlowResult[Counter], error) {
        state.Count += event.By
        return axiom.Next(
            state,
            axiom.Call(LogCount{Count: state.Count}),
        ), nil
    })

    axiom.EffectHandler(flow, func(_ context.Context, command LogCount) error {
        fmt.Printf("count=%d\n", command.Count)
        return nil
    })

    axiom.AddClaim(flow, func(state Counter) error {
        if state.Count < 0 {
            return fmt.Errorf("count must not be negative")
        }
        return nil
    })

    engine, err := axiom.OpenFlow(flow)
    if err != nil {
        log.Fatal(err)
    }

    run := engine.Execution("counter-1")
    if err := run.Dispatch(ctx, Increment{By: 2}); err != nil {
        log.Fatal(err)
    }

    state, err := run.State(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(state.Count) // 2
}
```

Обработчики Flow являются произвольным Go-кодом, поэтому уровень их анализа — `axiom.AnalysisOpaque`. При ошибке claim, handler или effect новое состояние и история не фиксируются.

Запуск полного примера:

```bash
go run ./examples/go-first
```

## Декларативная Go-модель

Пакет `model` позволяет хранить процесс в Go-коде, сохраняя проверку компилятором, индексы зависимостей, activities, claims, replay и impact analysis.

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/Homiakus/axiom"
    "github.com/Homiakus/axiom/model"
)

type User struct {
    ID          *string `json:"id"`
    Email       *string `json:"email"`
    WelcomeSent bool    `json:"welcomeSent"`
}

type UserRegistered struct {
    UserID string `json:"userId"`
    Email  string `json:"email"`
}

func (UserRegistered) AxiomEventName() string { return "UserRegistered" }

func main() {
    definition := model.New("Welcome")

    user := model.State[User](definition, "User").
        Default("WelcomeSent", false)
    registered := model.Event[UserRegistered](definition, "UserRegistered")

    definition.Policy("emailPolicy").
        Retry(2).
        Timeout(5 * time.Second).
        Concurrency("once").
        Idempotency("required")

    definition.Activity("SendWelcomeEmail").
        Input("userId", user.Field("ID")).
        Input("email", user.Field("Email")).
        Output("sent", "Bool").
        Effect("external").
        IdempotencyKey(user.Field("ID")).
        Policy("emailPolicy")

    definition.Rule("captureRegistration").
        On(registered.Trigger()).
        Set(user.Field("ID"), registered.Field("UserID")).
        Set(user.Field("Email"), registered.Field("Email"))

    definition.Rule("sendWelcomeEmail").
        On(user.Changed("Email")).
        When(model.Eq(user.Field("WelcomeSent"), model.Lit(false))).
        Run("SendWelcomeEmail").
        Set(user.Field("WelcomeSent"), model.Ref("output.sent"))

    definition.Claim(
        "welcomeSentRequiresEmail",
        model.Implies(
            model.Eq(user.Field("WelcomeSent"), model.Lit(true)),
            model.Exists(user.Field("Email")),
        ),
    )

    engine, err := axiom.Open(
        definition,
        axiom.Act("SendWelcomeEmail", func(
            context.Context,
            axiom.Input,
        ) (axiom.Output, error) {
            return axiom.Output{"sent": true}, nil
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    err = engine.Execution("user-1").Dispatch(
        context.Background(),
        UserRegistered{
            UserID: "user-1",
            Email:  "user@example.com",
        },
    )
    if err != nil {
        log.Fatal(err)
    }
}
```

Запуск полного примера:

```bash
go run ./examples/model
```

## Необязательные AXM- и TOML-frontends

Определения, хранящиеся вне кода приложения, компилируются в тот же `axiom.Plan`, что и декларативная Go-модель.

```go
import (
    "github.com/Homiakus/axiom/axm"
    "github.com/Homiakus/axiom/table"
)

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

_, _ = axmEngine, tableEngine
```

AXM и TOML являются входными форматами, а не обязательными требованиями runtime. Разные подсистемы приложения могут использовать разные представления, сохраняя общую инфраструктуру исполнения, хранения и наблюдаемости.

## Execution API

Скомпилированный engine предоставляет удобный handle для одного надёжного execution:

```go
run := engine.Execution("order-42")

// Создаёт execution, если он отсутствует, отправляет типизированное событие
// и выполняет зарегистрированные inline activities до состояния ожидания.
err := run.Dispatch(ctx, OrderCreated{OrderID: "42"})

var state OrderState
err = run.State(ctx, &state)

status, err := run.Status(ctx)
history, err := run.History(ctx)
pending, err := run.PendingActivities(ctx)
explanation, err := run.Explain(ctx)
err = run.Cancel(ctx)
```

Низкоуровневое управление жизненным циклом остаётся доступным для явной оркестрации:

```go
err := engine.Start(ctx, "order-42", initialContext)
err = engine.Signal(ctx, "order-42", "OrderCreated", payload)
err = engine.Patch(ctx, "order-42", patch)
result, err := engine.Query(ctx, "order-42", "state")
err = engine.RunUntilIdle(ctx, "order-42")
```

## Надёжное хранение с Pebble

По умолчанию скомпилированный runtime использует хранилище в памяти. Для надёжного исполнения откройте транзакционное Pebble-хранилище и закройте его при завершении приложения.

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
    axiom.Act("ChargeCard", chargeCard),
)
```

Режимы надёжности:

```go
// Синхронные commits: максимальная надёжность, наибольшая задержка записи.
store, err := axiom.OpenPebble("data/axiom")

// Без fsync при каждом commit: быстрее, но последние записи могут быть
// потеряны при аварии процесса или компьютера.
store, err = axiom.OpenPebble("data/axiom", axiom.PebbleNoSync())

// Группировка commits с периодическим flush.
store, err = axiom.OpenPebble(
    "data/axiom",
    axiom.PebbleSyncEvery(10*time.Millisecond),
)
```

`WithProductionMode` включает строгий быстрый runtime и требует транзакционного хранилища. Ошибки конфигурации выявляются при запуске вместо незаметного перехода на неподдерживаемый медленный путь исполнения.

## Activities и побочные эффекты

Правила определяют, когда должна быть запланирована activity, а Go-код реализует фактическое внешнее действие.

```go
func chargeCard(ctx context.Context, input axiom.Input) (axiom.Output, error) {
    // Внешняя операция должна быть идемпотентной по ключу из модели.
    return axiom.Output{
        "transactionId": "txn-123",
        "approved":      true,
    }, nil
}

engine, err := axiom.Open(
    definition,
    axiom.Act("ChargeCard", chargeCard),
)
```

Для внешних effects задавайте ключ идемпотентности и политику повторных попыток. Axiom записывает запланированные, завершённые и неуспешные activities в историю execution.

## Модель конкурентности

Axiom обеспечивает локальную для процесса линеаризуемость операций, отправленных через один экземпляр engine:

- обновления с **одинаковым execution ID** сериализуются;
- операции с **разными execution ID** могут выполняться параллельно;
- Pebble-транзакции не заменяют и не раскрывают общий объект store внутри engine;
- состояние и история фиксируются атомарно, если store поддерживает транзакции;
- типизированные целые числа остаются целыми после dispatch и повторного открытия Pebble.

Для координации между несколькими процессами используйте единый слой владения или маршрутизации для каждого execution ID либо реализуйте распределённое хранилище с эквивалентными транзакционными и конкурентными гарантиями.

## Replay, история и объяснение

```go
history, err := engine.Execution("order-42").History(ctx)
if err != nil {
    return err
}

replayed, err := axiom.ReplayFromHistory(engine.Module(), history)
if err != nil {
    return err
}

explanation, err := engine.Execution("order-42").Explain(ctx)
```

Replay проверяет идентичность модуля и детерминированно восстанавливает состояние runtime из записанной истории. Используйте ту же версию скомпилированного плана, которая создала историю.

## Базовые показатели производительности

Текущий CI-baseline измерен на общем GitHub-hosted runner `linux/amd64` с Go 1.26.5, четырьмя логическими CPU и concurrency 8. Эти значения подходят для грубого обнаружения регрессий, но не являются аппаратно-независимым SLA.

| Сценарий | p95 | p99 | Производительность |
|---|---:|---:|---:|
| Go-first Flow, разные execution | 3,841 мс | 4,788 мс | 9 028 ops/s |
| Go-first Flow, один конкурентный execution | 20,777 мс | 24,880 мс | 772 ops/s |
| Скомпилированный runtime, разные execution | 0,505 мс | 3,011 мс | 55 011 ops/s |
| Скомпилированный runtime, один конкурентный execution | 1,085 мс | 1,437 мс | 50 938 ops/s |
| Новый memory execution скомпилированного runtime | 0,800 мс | 4,058 мс | 40 239 ops/s |
| Pebble NoSync, новый durable execution | 3,904 мс | 5,061 мс | 8 773 ops/s |
| Pebble Sync, новый durable execution | 8,688 мс | 10,225 мс | 1 437 ops/s |
| Replay истории из 1 000 событий | 1,977 мс | 2,541 мс | 761 replay/s |

Лучшие показатели хвостовой задержки сейчас показывает скомпилированный runtime. Долгоживущий конкурентный Go-first Flow медленнее, потому что текущее memory-хранилище копирует и сериализует полную историю при каждом сохранении.

Полные значения p50, максимальной задержки, методика, покрытие тестами устойчивости и команды воспроизведения находятся в [`benchmarks/latest.md`](benchmarks/latest.md).

Локальный запуск percentile benchmark:

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

## Проверка устойчивости

Набор тестов покрывает:

- конкурентные обновления одного Go-first execution;
- конкурентные обновления одного execution скомпилированного runtime;
- независимые параллельные Pebble execution;
- rollback транзакции после ошибки Flow effect;
- сохранение типа целого числа в типизированных событиях;
- сохранение целых чисел после закрытия и повторного открытия Pebble;
- точное восстановление состояния через replay;
- soak-тест Flow: 16 workers и 8 000 операций;
- Go race detector для runtime и хранилищ;
- отдельный consumer module, импортирующий только публичные пакеты.

## Пакеты

| Пакет | Назначение |
|---|---|
| `github.com/Homiakus/axiom` | Канонический Plan, скомпилированный runtime, типизированный execution API и Go-first Flow |
| `github.com/Homiakus/axiom/model` | Декларативный, не требующий файлов, статически проверяемый Go-builder |
| `github.com/Homiakus/axiom/axm` | AXM parser и frontend для Plan |
| `github.com/Homiakus/axiom/table` | Frontend таблиц переходов TOML |
| `github.com/Homiakus/axiom/store/pebble` | Публичный пакет надёжного Pebble-хранилища |
| `github.com/Homiakus/axiom/cmd/axiomgen` | Необязательный генератор типизированных границ |
| `github.com/Homiakus/axiom/cmd/axiombench` | Benchmark harness для percentile-метрик и проверки устойчивости |

## Разработка

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
```

CI также собирает отдельный consumer module и проверяет, что приложение может использовать публичный API без импорта внутренних пакетов.

## Рекомендации по проектированию

- Используйте **Flow**, когда логика переходов является обычным кодом приложения и статический анализ не требуется.
- Используйте **model**, **AXM** или **TOML**, когда проверка, impact analysis, явные claims и объяснимость являются основными требованиями.
- Делайте effects идемпотентными и явно моделируйте их ключи.
- Используйте стабильные execution ID, связанные с защищаемым бизнес-агрегатом.
- Выбирайте Pebble Sync, когда зафиксированное состояние должно пережить потерю питания; NoSync допустим только при приемлемом риске потери последних записей.
- Фиксируйте версии планов для надёжных историй и выполняйте replay тем же скомпилированным модулем.
- Измеряйте строгие требования к задержке на выделенном оборудовании с фиксированными CPU, накопителем и версией Go.

## Лицензия

Apache-2.0. См. [`LICENSE`](LICENSE).
