# Axiom

Axiom - Go-платформа для описания, исполнения и анализа правил поведения в
`.axm` DSL. Проект объединяет runtime-библиотеку, кодогенератор, локальную Rule
Studio и документацию по целевому TRIZ-слою.

Цель Axiom - дать разработчику способ описывать durable workflows как набор
сигналов, состояния, условий, правил, activity и safety-claim'ов, а runtime берет
на себя компиляцию, проверку зависимостей, выполнение до fixpoint, сохранение
истории и восстановление после сбоев.

## Что Внутри

| Компонент | Путь | Статус | Назначение |
|---|---|---|---|
| Axiom runtime | [`axiom-main`](axiom-main) | работает | Go-библиотека, `.axm` parser/compiler, runtime engine, stores, replay, diagnostics |
| axiomgen | [`axiom-main/tools/axiomgen`](axiom-main/tools/axiomgen) | работает | генерация типобезопасной Go-границы из `.axm` |
| Rule Studio | [`axiom-studio`](axiom-studio) | MVP | локальный web-интерфейс для просмотра, симуляции и диагностики правил |
| TRIZ docs | [`axiom_triz_full_docs`](axiom_triz_full_docs) | design layer | документация целевого пользовательского слоя поверх Axiom v0 |

## Для Чего Это

Axiom полезен, когда бизнес- или доменная логика должна быть:

- декларативной: поведение читается как правила, а не как цепочка callback'ов;
- durable: история событий позволяет восстановить execution и объяснить решение;
- проверяемой: компилятор ловит неизвестные ссылки, циклы, неверные writes и проблемы activity-контрактов;
- типобезопасной на границе с Go: `axiomgen` генерирует structs, adapters и activity-интерфейсы;
- объяснимой: queries, claims, traces и Studio помогают понять, почему правило сработало или было заблокировано.

Типичные сценарии: workflow orchestration, правила безопасности, state machines,
доменные симуляторы, control systems, decision engines и продукты, где важно
разделить "что должно произойти" и "как именно вызвать внешний код".

## Модель Axiom v0

Исполняемый DSL сейчас находится в `axiom-main` и использует runtime-термины:

```text
domain -> signal -> context -> computed -> fact -> policy -> activity -> rule -> claim -> query
```

Коротко:

- `signal` - внешний или внутренний вход в execution;
- `context` - durable state;
- `computed` и `fact` - чистые вычисления и именованные условия;
- `policy` - timeout, retry, idempotency и audit-настройки для activity;
- `activity` - вызов Go-кода с проверенным input/output контрактом;
- `rule` - trigger, guards, activity calls и writes;
- `claim` - invariant безопасности;
- `query` - read-only представление состояния.

TRIZ-слой в [`axiom_triz_full_docs`](axiom_triz_full_docs) описывает более
читаемый пользовательский язык (`system`, `event`, `state`, `condition`,
`function`, `always`, `view`). Этот слой является целевой спецификацией и должен
нормализоваться в Axiom v0.

## Быстрый Старт

Требуется Go 1.26+.

```powershell
git clone https://github.com/Homiakus/axiom.git
cd axiom
```

Проверить runtime-библиотеку:

```powershell
cd axiom-main
go test ./...
go run ./cmd/axiom validate examples/axiom-files/welcome.axm
go run ./cmd/axiom run-welcome examples/axiom-files/welcome.axm
```

Запустить Rule Studio:

```powershell
cd ..\axiom-studio
go run . ..\axiom-main\examples\hydropilot\hydropilot.axm
```

Открыть в браузере:

```text
http://127.0.0.1:8080
```

## Минимальный Пример API

```go
package main

import (
	"context"

	"axiom/pkg/axiom"
)

func main() {
	ctx := context.Background()

	app, err := axiom.Load("examples/axiom-files/welcome.axm")
	if err != nil {
		panic(err)
	}

	engine, err := app.New(
		axiom.Act("SendWelcomeEmail", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
			return axiom.Output{"sent": true}, nil
		}),
	)
	if err != nil {
		panic(err)
	}

	if err := engine.Start(ctx, "exec-1", nil); err != nil {
		panic(err)
	}
	if err := engine.Signal(ctx, "exec-1", "UserRegistered", axiom.Input{
		"userId": "u1",
		"email":  "user@example.com",
	}); err != nil {
		panic(err)
	}
	if err := engine.RunUntilIdle(ctx, "exec-1"); err != nil {
		panic(err)
	}
}
```

Подробный API описан в [`axiom-main/README.md`](axiom-main/README.md).

## Кодогенерация

`axiomgen` генерирует Go-контракт из `.axm`: embedded source, hash, constants,
input/output structs, activity interface и adapter layer.

```powershell
cd axiom-main
go run ./tools/axiomgen --file examples/axiom-files/welcome.axm
```

Поведение при повторной генерации:

- `*_axiom.gen.go` перезаписывается полностью;
- `*_activities.go` создается один раз, а новые stubs аккуратно добавляются;
- изменения `.axm` становятся compile-time ошибками в Go, если контракт больше не совпадает.

Подробнее: [`axiom-main/docs/axiomgen.md`](axiom-main/docs/axiomgen.md).

## Rule Studio

Rule Studio - локальный Go web-server без frontend-фреймворка. Он помогает
читать и проверять модель:

- показывает правила, triggers, guards, reads/writes и safety hints;
- симулирует событие и показывает runnable/blocked/unknown rules;
- строит diagnostics и Markdown-отчет;
- содержит source editor и генерацию Go stubs;
- работает с `AXIOM_STUDIO_ADDR` для смены адреса.

```powershell
cd axiom-studio
$env:AXIOM_STUDIO_ADDR = "127.0.0.1:8090"
go run . ..\axiom-main\examples\hydropilot\hydropilot.axm
```

Подробнее: [`axiom-studio/README.md`](axiom-studio/README.md).

## Документация

| Документ | Что читать |
|---|---|
| [`axiom-main/docs/axiom-file-specification.md`](axiom-main/docs/axiom-file-specification.md) | спецификация текущего `.axm` DSL |
| [`axiom-main/docs/axiom-crfg.md`](axiom-main/docs/axiom-crfg.md) | модель Context-Reactive Flow Graph |
| [`axiom-main/docs/axiomgen.md`](axiom-main/docs/axiomgen.md) | кодогенератор и typed workflow |
| [`axiom_triz_full_docs/PROJECT_ANALYSIS.md`](axiom_triz_full_docs/PROJECT_ANALYSIS.md) | разбор текущего состояния и разрыва между v0 и TRIZ |
| [`axiom_triz_full_docs/README.md`](axiom_triz_full_docs/README.md) | карта TRIZ-документации |
| [`axiom-main/examples/hydropilot/hydropilot.axm`](axiom-main/examples/hydropilot/hydropilot.axm) | большой исполняемый пример |
| [`axiom-main/examples/triz/hydropilot_mini.axm`](axiom-main/examples/triz/hydropilot_mini.axm) | компактный пример целевого TRIZ-стиля |

## Структура Репозитория

```text
.
+-- axiom-main/
|   +-- pkg/axiom/              # публичный Go API
|   +-- internal/lang/          # parser и AST для .axm
|   +-- internal/compiler/      # AST -> compiled module
|   +-- internal/runtime/       # engine, FastVM, eval, workers, replay
|   +-- internal/store/         # memory и Pebble stores
|   +-- cmd/axiom/              # CLI/demo commands
|   +-- tools/axiomgen/         # code generator
|   +-- examples/               # .axm примеры и demo-приложения
|   +-- docs/                   # спецификация runtime DSL
+-- axiom-studio/               # локальная Rule Studio
+-- axiom_triz_full_docs/       # целевой TRIZ user-facing layer
+-- .gitignore
+-- LICENSE
+-- README.md
```

## Проверка

Команды, которыми стоит проверять изменения перед push:

```powershell
cd axiom-main
go test ./...

cd tools/axiomgen
go test ./...

cd ..\..\examples\control-panel
$env:GOWORK = "off"
go test ./...

cd ..\..\..\axiom-studio
go test ./...
```

Почему `GOWORK=off` для `examples/control-panel`: это отдельный Go module, а
workspace `axiom-main/go.work` включает только основной модуль и `tools/axiomgen`.

## Production Notes

- Memory store подходит для разработки и тестов.
- Pebble store предназначен для durable execution и crash recovery.
- `WithProductionMode()` включает strict fast runtime и требует transactional store.
- Activity output проверяется по DSL-контракту.
- Structured diagnostics возвращают коды `AXnnn`, file/line/entity и сообщение.
- Claims и replay нужны для объяснимости и расследования runtime-поведения.

## Лицензия

Проект распространяется по лицензии из [`LICENSE`](LICENSE).
