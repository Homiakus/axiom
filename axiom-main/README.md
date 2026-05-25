# Axiom

Axiom — подключаемая Go-библиотека для загрузки и исполнения `.axm` DSL-модулей.
DSL описывает durable execution model: signals, context, computed values, facts,
rules, activities, policies и claims. Библиотека компилирует `.axm` в
нормализованный модуль, запускает execution, диспетчеризует правила, планирует
activity-задачи и сохраняет историю для crash recovery и replay.

Корневой модуль не зависит от Bubble Tea, Lip Gloss или других UI-пакетов.

## Быстрый старт

```go
// Минимальный сценарий: загрузить .axm, зарегистрировать activity, запустить.
app, err := axiom.Load("module.axm")
engine, err := app.New(
    axiom.Act("SendWelcomeEmail", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
        return axiom.Output{"sent": true}, nil
    }),
)
engine.Start(ctx, "exec-1", nil)
engine.Signal(ctx, "exec-1", "UserRegistered", axiom.Input{"userId": "u1", "email": "user@example.com"})
engine.RunUntilIdle(ctx, "exec-1")
```

Для низкоуровневого сценария можно компилировать исходный текст напрямую:

```go
module, err := axiom.Compile(source, axiom.WithSourceName("checkout.axm"))
engine, err := axiom.New(module,
    axiom.Act("ChargeCard", chargeCard),
)
```

## Установка

```bash
go get axiom
```

Требуется Go 1.26+.

---

## Архитектура

Axiom — многослойная библиотека. Каждый слой решает одну задачу и не зависит
от верхних:

```
┌─────────────────────────────────────────────────┐
│  Публичный API  (pkg/axiom/)                     │
│  Load, Compile, New, Act, Start, Signal, Query   │
├─────────────────────────────────────────────────┤
│  Компилятор  (internal/compiler/)                │
│  Парсинг .axm → AST → symbol table → валидация   │
│  → индексы зависимостей → compiled Module        │
├─────────────────────────────────────────────────┤
│  Runtime  (internal/runtime/)                    │
│  Engine, FastVM, eval, rule-queue, worker,       │
│  transaction, replay                             │
├─────────────────────────────────────────────────┤
│  Store  (internal/store/)                        │
│  MemoryStore, PebbleStore (durable, ACID)        │
├─────────────────────────────────────────────────┤
│  Diagnostics  (internal/diag/)                   │
│  Структурированные ошибки AXnnn                  │
└─────────────────────────────────────────────────┘
```

### Слои подробно

| Слой | Путь | Ключевые файлы | Назначение |
|------|------|---------------|------------|
| API | `pkg/axiom/` | `axiom.go` | Публичные типы, Load/Compile/New, Act/Acts, WithStore/WithProductionMode |
| Парсер | `internal/lang/` | `parser.go`, `ast.go`, `expr.go` | Парсинг `.axm` → AST; лексический и синтаксический анализ |
| Компилятор | `internal/compiler/` | `module.go` | AST → compiled Module; symbol table, валидация ссылок, циклов, индексы |
| Runtime | `internal/runtime/` | `engine.go`, `fast_vm.go`, `fast_engine.go`, `eval.go`, `worker.go`, `replay.go`, `clone.go`, `types.go`, `value.go` | Engine lifecycle, FastVM (bitset-based evaluation), expression VM, worker pool, replay |
| Хранилище | `internal/store/` | `memory/store.go`, `pebble/store.go`, `pebble/transaction.go`, `pebble/codec.go` | MemoryStore (dev/test), PebbleStore (production, ACID-транзакции) |
| Диагностика | `internal/diag/` | `diag.go` | Error/Errors типы с кодами AXnnn |

---

## Публичный API

### Типы

```go
type Module = compiler.Module      // скомпилированный .axm модуль
type Engine = runtime.Engine       // движок исполнения
type Store = runtime.Store         // интерфейс хранилища
type Execution = runtime.Execution // состояние одного исполнения
type Input = map[string]any        // вход activity/signal
type Output = map[string]any       // результат activity
type Activity func(ctx context.Context, input Input) (Output, error)
type ActivityRegistry map[string]Activity
```

### Загрузка и компиляция

```go
// Load читает .axm с диска и компилирует.
app, err := axiom.Load("module.axm")

// Compile компилирует исходный текст.
module, err := axiom.Compile(source, axiom.WithSourceName("inline.axm"))

// CompileAndNew — компиляция + создание Engine за один вызов.
engine, err := axiom.CompileAndNew(source, axiom.Act("MyActivity", myFunc))

// MustLoad, MustCompile, MustCompileAndNew — panic на ошибке.
```

### Engine: создание

```go
// New собирает Engine из скомпилированного Module и опций.
engine, err := axiom.New(module,
    axiom.Act("SendEmail", sendEmail),
    axiom.WithTraceLevel(axiom.TraceFull),
)

// App.New — то же самое через App.
engine, err := app.New(axiom.Acts(registry))

// MustNew — panic на ошибке конфигурации.
```

### Engine: опции

| Опция | Назначение |
|-------|-----------|
| `Act(name, fn)` | Зарегистрировать одну activity |
| `Acts(registry)` | Зарегистрировать несколько activity |
| `WithActivity(name, fn)` | Алиас для Act |
| `WithActivities(registry)` | Алиас для Acts |
| `WithStore(store)` | Явное хранилище (MemoryStore по умолчанию) |
| `WithStrictFastRuntime()` | Запретить fallback на медленный путь |
| `WithProductionMode()` | Strict fast + обязательный TransactionalStore |
| `WithTraceLevel(level)` | TraceMinimal / TraceAggregate / TraceFull |

### Engine: lifecycle

```go
// Запустить execution с начальным контекстом.
engine.Start(ctx, "exec-1", map[string]any{
    "User": map[string]any{"id": "u1", "country": "US"},
})

// Отправить сигнал.
engine.Signal(ctx, "exec-1", "CheckoutRequested", nil)

// Выполнить все готовые правила до fixpoint.
engine.RunUntilIdle(ctx, "exec-1")

// Прочитать состояние или историю.
state, _ := engine.Query(ctx, "exec-1", "state")
history, _ := engine.Query(ctx, "exec-1", "history")
```

### Engine: низкоуровневые методы

```go
engine.Patch(ctx, "exec-1", axiom.Patch{"User.email": "new@example.com"})
engine.PollTask(ctx, "exec-1")            // взять pending activity task
engine.CompleteTask(ctx, task, result, nil) // завершить activity
engine.Cancel(ctx, "exec-1")              // отменить execution
```

### Хранилище

```go
// Memory store — по умолчанию, не требует настройки.
engine, _ := axiom.New(module) // использует MemoryStore

// Pebble store — durable, для продакшена.
store, err := axiom.OpenPebble("data/axiom",
    axiom.PebbleNoSync(),        // быстрее, менее durable
    axiom.PebbleSyncEvery(100),  // групповой fsync
    axiom.PebbleJSONCodec(),     // JSON вместо Gob
)
engine, _ := axiom.New(module, axiom.WithStore(store))
```

### Replay

```go
// Восстановить Execution из истории.
execution, err := axiom.ReplayFromHistory(module, history)
```

---

## Activity: регистрация и выполнение

Каждая activity из `.axm` с `effect != none` должна быть зарегистрирована в Go.
`axiom.New` проверяет соответствие до запуска execution:

```go
engine, err := axiom.New(module, axiom.Acts(axiom.ActivityRegistry{
    "CheckInventory": func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
        return axiom.Output{"status": "available", "unavailable": []any{}}, nil
    },
    "CalculateRisk": calculateRisk,
}))
```

Если activity не зарегистрирована или в Go зарегистрировано лишнее имя, библиотека
вернёт структурную ошибку конфигурации. После выполнения activity результат также
проверяется по DSL-блоку `output:`: отсутствующие поля и несовпадения типов
возвращаются как `AX503` / `AX504`.

---

## Кодогенерация с axiomgen

Чтобы не дублировать имена `activity` и форму payload вручную, можно сгенерировать
типизированную Go-обвязку из `.axm`:

```bash
# Интерактивный TUI-мастер
go run ./tools/axiomgen

# Неинтерактивный режим
go run ./tools/axiomgen --file examples/axiom-files/welcome.axm
```

axiomgen создаёт два файла:

| Файл | Содержимое | Поведение при перегенерации |
|------|-----------|---------------------------|
| `<domain>_axiom.gen.go` | Embedded `.axm`, хеш, константы activity, типизированные Input/Output structs, интерфейс `Activities`, адаптеры | **Всегда перезаписывается** |
| `<domain>_activities.go` | Пустая реализация-заглушка `XxxActivityImpl{}` | **Создаётся один раз**, дальше не трогается |

### Типизированный workflow

```go
// Шаг 1: сгенерировать обвязку
// $ go run ./tools/axiomgen --file hydropilot.axm

// Шаг 2: реализовать activity (в hydro_pilot_activities.go)
func (impl HydroPilotActivityImpl) SendUartRequest(
    ctx context.Context,
    input SendUartRequestInput,  // типы из .axm!
) (SendUartRequestOutput, error) {
    // Твой код — поля строго типизированы.
    return SendUartRequestOutput{
        CommandName: input.CommandName,
        Status: "ok",
    }, nil
}

// Шаг 3: использовать без строковых регистраций
engine, err := generated.NewHydroPilot(generated.HydroPilotActivityImpl{})
```

### Умный merge при изменении .axm

Если `.axm` изменился (добавлены новые activity), при повторном запуске axiomgen:

- `*_axiom.gen.go` — полностью перезаписывается (новые типы, новый интерфейс)
- `*_activities.go` — **существующие методы не трогаются**, новые методы-заглушки дописываются в конец файла

Это даёт compile-time safety: если изменились поля существующей activity, Go-компилятор
сразу покажет ошибку несоответствия типов.

### Diff-отчёт

При повторной генерации axiomgen показывает, что именно изменилось в `.axm`:

```
Changes in .axm:
  + activity NewActivity
    input.commandName: String
    output.status: String
  - activity OldActivity  (method stays in _activities.go)
  ~ activity SendUartRequest
    ~ input.payload: String → Int
    + output.newField: Bool
```

Подробнее — в [документации axiomgen](docs/axiomgen.md).

---

## Коды ошибок

Все ошибки приводятся к публичным типам `axiom.Error` и `axiom.Errors`:

```go
if err != nil {
    var diagnostics axiom.Errors
    if errors.As(err, &diagnostics) {
        for _, diagnostic := range diagnostics {
            log.Printf("%s %s:%d %s %s",
                diagnostic.Code,
                diagnostic.File,
                diagnostic.Line,
                diagnostic.Entity,
                diagnostic.Message,
            )
        }
    }
}
```

### Конфигурация (AX500–AX506)

| Код | Описание |
|-----|---------|
| AX500 | Ошибка конфигурации: неверная опция, nil-activity, пустое имя |
| AX501 | Activity объявлена в `.axm`, но не зарегистрирована в Go |
| AX502 | Activity зарегистрирована в Go, но не объявлена в `.axm` |
| AX503 | Go activity не вернула поле из DSL `output:` или несовпадение типов |
| AX504 | Go activity вернула поле неправильного типа |
| AX506 | Production mode требует TransactionalStore |

### Компиляция (AX000–AX399)

| Код | Описание |
|-----|---------|
| AX000 | Ошибка парсинга или компиляции |
| AX001 | Неразрешённая ссылка |
| AX002 | Дубликат объявления |
| AX003 | Дубликат поля |
| AX201 | Циклическая зависимость computed |
| AX202 | Циклическая зависимость fact |
| AX204 | `signal.*` вне signal-правила |
| AX301 | Неверная цель write |
| AX302 | Несуществующее поле output |
| AX303 | `rule.run` ссылается на не-activity |
| AX304 | Activity без policy |
| AX305 | Отсутствует idempotency key |
| AX306 | Catch target signal не существует |

### Runtime (AX400–AX499)

| Код | Описание |
|-----|---------|
| AX400 | Ошибка runtime |
| AX401 | Execution не найден |
| AX405 | Patch ссылается на неизвестное поле |
| AX406 | Patch: неверный тип значения |
| AX600 | Ошибка чтения файла модуля |

---

## Структура проекта

```
axiom-main/
├── pkg/axiom/           # Публичный API библиотеки
│   ├── axiom.go         # Основной API: Load, Compile, New, Act, типы
│   ├── bundle.go        # App, MustLoad и хелперы
│   └── *_test.go        # Интеграционные тесты
├── internal/
│   ├── lang/            # Лексический анализатор и парсер .axm → AST
│   │   ├── parser.go
│   │   ├── ast.go       # Типы AST: Module, SignalDecl, ActivityDecl, ...
│   │   └── expr.go      # Выражения и хелперы (ExprRefs и т.д.)
│   ├── compiler/        # Компилятор AST → compiled Module
│   │   └── module.go    # Compile, symbol table, валидация, индексы
│   ├── runtime/         # Runtime engine
│   │   ├── engine.go    # Engine: Start, Signal, RunUntilIdle, Query
│   │   ├── fast_vm.go   # FastVM: bitset-based rule evaluation
│   │   ├── fast_engine.go # Fast engine: атомарные операции
│   │   ├── eval.go      # Вычисление выражений
│   │   ├── worker.go    # Worker pool для activity
│   │   ├── replay.go    # Восстановление состояния из истории
│   │   ├── expr_vm.go   # Expression VM (opcode-based)
│   │   ├── rule_queue.go # Очередь правил
│   │   ├── transaction.go # Транзакционная обёртка
│   │   ├── types.go     # Типы: FieldID, AtomID, RuleID, ValueKind
│   │   ├── value.go     # Value union type
│   │   ├── types_check.go # Проверка типов
│   │   └── clone.go     # Глубокое копирование состояния
│   ├── store/
│   │   ├── memory/      # In-memory store (dev/test)
│   │   └── pebble/      # Pebble-backed durable store (production)
│   │       ├── store.go
│   │       ├── transaction.go
│   │       └── codec.go # Gob/JSON codec
│   ├── diag/            # Структурированные ошибки
│   │   └── diag.go
│   └── generated/       # Сгенерированная обвязка для hydropilot
├── cmd/axiom/           # CLI-демо для проверки API
│   └── main.go
├── tools/axiomgen/      # Кодогенератор (отдельный go.mod)
│   ├── main.go
│   └── internal/
│       ├── codegen/     # Генерация Go-кода из Module
│       ├── generate/    # Оркестрация: Preview, Run, smart merge, diff
│       └── tui/         # Bubble Tea TUI
├── examples/
│   ├── axiom-files/     # Примеры .axm файлов
│   │   ├── welcome.axm
│   │   ├── checkout.axm
│   │   ├── claims.axm
│   │   └── reminder.axm
│   ├── control-panel/   # TUI-панель управления (отдельный go.mod)
│   └── hydropilot/      # Сгенерированная обвязка hydropilot
├── docs/
│   ├── axiom-file-specification.md  # Спецификация .axm DSL
│   ├── axiom-crfg.md                # Модель CRFG (Context-Reactive Graph)
│   ├── axiomgen.md                  # Документация кодогенератора
│   └── ТЗ на улучшение.md           # Техническое задание на улучшения
├── go.mod               # Go module: axiom, Go 1.26
├── go.work              # Workspace: . + tools/axiomgen
└── README.md
```

---

## Проверка

```bash
# Сборка и линтинг
go build ./...
go vet ./...

# Тесты ядра
go test ./internal/... ./pkg/...

# CLI-демо
go run ./cmd/axiom validate examples/axiom-files/welcome.axm
go run ./cmd/axiom run-welcome examples/axiom-files/welcome.axm
go run ./cmd/axiom run-checkout examples/axiom-files/checkout.axm

# Кодогенератор
go run ./tools/axiomgen --file examples/axiom-files/welcome.axm
cd tools/axiomgen && go test ./...

# Control panel
cd examples/control-panel && GOWORK=off go build . && GOWORK=off go test ./...
```

Файлы в `examples/axiom-files` являются тестовыми и документационными входными
данными. Тест `pkg/axiom/example_files_test.go` компилирует каждый `.axm` файл,
чтобы примеры не расходились с реальным компилятором.
