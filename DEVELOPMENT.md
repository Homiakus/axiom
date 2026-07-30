# Локальная разработка Axiom

## Требования

- Git.
- Go 1.26.x или новее в пределах совместимости, заявленной в `go.mod`.

Docker, внешняя база данных, `.env` и миграции для стандартного цикла разработки не требуются.

## Получение исходного кода

```bash
git clone https://github.com/Homiakus/axiom.git
cd axiom
```

Все следующие команды выполняются из корня репозитория.

## Проверка окружения

```bash
go version
go env GOMOD
```

Ожидается, что `GOMOD` указывает на `axiom/go.mod`, а версия Go не ниже 1.26.

## Зависимости

```bash
go mod download
go mod tidy
git diff --exit-code -- go.mod go.sum
```

`go mod tidy` не должен изменять committed module metadata. Именно это проверяет CI.

## Основная проверка

```bash
go test ./...
go vet ./...
```

## Race detector

CI запускает race detector для корневого package и критичных runtime/store packages:

```bash
go test -race . ./internal/runtime/... ./internal/store/...
```

Полный `go test -race ./...` допустим для локальной расширенной проверки, но не является текущей CI-командой.

## Проверяемый пример

```bash
go run ./examples/coffee-machine
```

Пример использует временную директорию Pebble и удаляет её после завершения. Постоянные локальные данные в репозитории не создаются.

CI проверяет как минимум следующие строки результата:

```text
принято:    350,00 ₽
возвращено: 120,00 ₽
выручка:    230,00 ₽
касса:      230,00 ₽
кредит:     0,00 ₽
напитков:   2
```

## Публичный consumer test

CI создаёт отдельный временный Go-модуль и проверяет импорт только публичных packages:

- `github.com/Homiakus/axiom`;
- `github.com/Homiakus/axiom/axm`;
- `github.com/Homiakus/axiom/model`;
- `github.com/Homiakus/axiom/table`.

При изменении public API сначала запускайте:

```bash
go test ./...
```

а затем проверяйте, что новый API можно использовать без импорта `internal/*`.

## Кодогенератор

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

Поддерживаемые flags:

- `--file` — обязательный путь к `.axm` или `.toml`;
- `--out` — директория результата; по умолчанию директория source;
- `--package` — имя Go package; по умолчанию выводится из `--out`.

Команда не имеет интерактивного TUI и печатает JSON result в stdout.

Чтобы не оставлять generated files в рабочем дереве, используйте временную директорию:

```bash
tmp_dir="$(mktemp -d)"
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out "$tmp_dir" \
  --package generated
rm -rf "$tmp_dir"
```

На Windows PowerShell используйте каталог из `[System.IO.Path]::GetTempPath()` и удаляйте только созданную вами временную директорию.

## Benchmarks

Команда, совпадающая с CI:

```bash
mkdir -p artifacts
go run ./cmd/axiombench \
  -memory-ops 20000 \
  -pebble-ops 1000 \
  -replay-events 1000 \
  -replay-runs 200 \
  -concurrency 8 \
  -strict=true \
  -json artifacts/benchmark-results.json \
  -markdown artifacts/benchmark-results.md
```

Benchmark создаёт указанные JSON/Markdown files. Директорию `artifacts` можно безопасно удалить после проверки, если она не содержит других пользовательских файлов.

## Типы тестов в репозитории

- unit tests parser/compiler/runtime/store;
- comprehensive tests;
- fuzz tests parser/compiler/TRIZ normalization;
- race and concurrency tests;
- stress/soak tests;
- allocation and performance benchmarks;
- example execution and replay checks;
- code generator tests.

## Диагностика

### `go mod tidy` изменяет файлы

1. Убедитесь, что используется Go 1.26.x.
2. Выполните `go mod tidy` из корня.
3. Просмотрите `git diff -- go.mod go.sum`.
4. Не коммитьте изменения автоматически: выясните, изменился ли import graph намеренно.

### AX501: activity не зарегистрирована

Для каждой activity с effect, отличным от `none`, зарегистрируйте handler через `axiom.Act`, `axiom.ActTyped` или `axiom.Acts`.

### AX506: production mode требует transactional store

Передайте Pebble или custom store, реализующий `TransactionalStore`:

```go
engine, err := plan.New(
    axiom.WithStore(store),
    axiom.WithProductionMode(),
)
```

### Ошибка strict fast runtime

`WithProductionMode` включает strict fast runtime. Упростите неподдерживаемое expression либо используйте обычный runtime, если production safeguards не требуются. Не отключайте strict mode молча в production path.

### Тесты с Pebble оставили каталог

Официальный coffee-machine example и benchmark используют temporary directories. Если пользовательский тест пишет в фиксированный путь, удаляйте только каталог, созданный этим тестом, после закрытия store.

## Перед коммитом

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go test -race . ./internal/runtime/... ./internal/store/...
go vet ./...
go run ./examples/coffee-machine
```
