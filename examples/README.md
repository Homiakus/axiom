# Примеры Axiom

Примеры организованы не как случайный набор features, а как путь по уровням сложности. Для нового Go-проекта начинайте с `model/`.

## Какой пример открыть первым

| Каталог | Что показывает | Когда выбирать | Запуск |
|---|---|---|---|
| [`model/`](model/) | declarative Go model, typed fields/events, claims, policies, `ActTyped`, `Run` | **основной вариант для нового Go-кода** | `go run ./examples/model` |
| [`go-first/`](go-first/) | минимальный typed reducer `Flow` и typed effects | маленькая Go-only state machine без static model analysis | `go run ./examples/go-first` |
| [`order/`](order/) | production-oriented model + Pebble + `WithProductionMode` | нужен полный lifecycle с durable activity semantics | `go run ./examples/order` |
| [`axiom-files/`](axiom-files/) | внешний AXM file → `Plan` → runtime | definition должна жить отдельно от Go | `go run ./examples/axiom-files` |
| [`table/`](table/) | TOML decision table → `Plan` → runtime | процесс естественно редактировать как таблицу решений | `go run ./examples/table` |
| [`triz/`](triz/) | TRIZ normalization, diagnostics и source map | tooling/Studio и user-facing DSL | `go run ./examples/triz` |
| [`coffee-machine/`](coffee-machine/) | большой end-to-end доменный сценарий, деньги, ресурсы, activities и diagrams | нужен подробный advanced reference | `go run ./examples/coffee-machine` |

## Рекомендуемый порядок

```text
model
  ↓
order
  ↓
coffee-machine
```

Это основной путь для прикладного Go-разработчика.

Если process definition должна быть внешней:

```text
axiom-files (AXM) ─┐
                   ├─→ axiom.Plan → Engine → Run
table (TOML) ──────┘
```

`triz/` стоит отдельно: он показывает normalization/tooling boundary и source mapping, а не рекомендуемый application API по умолчанию.

## Что считается хорошим стилем в examples

- ошибки обрабатываются явно; `_ = err` и игнорирование ошибок не являются учебным паттерном;
- для application activities используется `ActTyped`, если payload имеет устойчивую Go-форму;
- для одного execution используется `run := engine.Execution(id)`;
- `model` — frontend по умолчанию, а не один из нескольких равноправных способов «наугад»;
- reusable `model.Key` применяется там, где поле встречается многократно;
- strict typed operators (`Equal`, `GreaterOrEqual`, `PlusField` и т. п.) предпочтительнее legacy `any` operators для нового кода;
- external effects остаются idempotent даже при durable retry;
- `WithProductionMode()` используется только вместе с transactional store.

## Проверка всех примеров

Из корня репозитория:

```bash
go test ./...
go run ./examples/model
go run ./examples/go-first
go run ./examples/order
go run ./examples/axiom-files
go run ./examples/table
go run ./examples/triz
go run ./examples/coffee-machine
```

CI запускает полный test suite, отдельный coffee-machine smoke test, race checks, vulnerability scan, fuzz smoke tests, внешний consumer module и performance job. При добавлении нового публичного example желательно включить его в автоматическую проверку, если он демонстрирует отдельный API path.

## AXM data files

В [`axiom-files/`](axiom-files/) также лежат `checkout.axm`, `claims.axm` и `reminder.axm`; они используются как compiler/runtime fixtures и документационные примеры. Все `.axm` файлы каталога компилируются тестами.
