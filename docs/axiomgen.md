# axiomgen

`axiomgen` генерирует типизированные Go activity boundaries из AXM или TOML source.

Текущая реализация находится в `cmd/axiomgen`. Команда не имеет интерактивного TUI и требует flag `--file`.

## Запуск

Из корня репозитория:

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

Минимальная команда:

```bash
go run ./cmd/axiomgen --file examples/axiom-files/welcome.axm
```

Если `--out` не указан, используется директория source file. Если `--package` не указан, package name выводится из output directory.

## Flags

| Flag | Обязателен | Назначение |
|---|---:|---|
| `--file` | Да | Путь к `.axm` или `.toml` source |
| `--out` | Нет | Output directory |
| `--package` | Нет | Имя generated Go package |

Неизвестный flag завершает команду с usage error.

## Поддерживаемые source formats

- `.toml` выбирает TOML frontend;
- остальные пути обрабатываются через AXM/TRIZ-capable compiler path.

Для TOML source semantic AXM diff не строится, но deterministic regeneration сохраняется.

## Результат команды

В stdout печатается JSON object:

```json
{
  "domain": "Welcome",
  "hash": "...",
  "out": "generated",
  "package": "generated",
  "files": [],
  "written": [],
  "skipped": []
}
```

`files` содержит planned action для каждого файла:

- `create` — создать;
- `overwrite` — заменить generated file;
- `skip` — не перезаписывать user-owned file.

## Generated и user-owned files

Code generator возвращает набор files с признаком `Once`.

- Обычный generated file перезаписывается при повторном запуске.
- File с `Once: true` не перезаписывается целиком.
- Для существующего activity implementation file generator пытается добавить только отсутствующие method stubs через Go AST merge.

Всегда проверяйте JSON result и `git diff` перед коммитом generated code.

## AXM diff

Для AXM generator пытается извлечь embedded previous source из существующего generated file и построить semantic diff. Diff может содержать added, removed и changed activities/fields.

Для первой генерации или если previous embedded source не найден, diff отсутствует.

## Безопасная проверка

Linux/macOS:

```bash
tmp_dir="$(mktemp -d)"
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out "$tmp_dir" \
  --package generated
find "$tmp_dir" -maxdepth 1 -type f -print
rm -rf "$tmp_dir"
```

Удаляйте только temporary directory, созданную этой командой.

## Тесты

```bash
go test ./cmd/axiomgen/...
```

Полная проверка repository также включает:

```bash
go test ./...
```

## Ограничения документации

Точный набор и schema generated files определяется `cmd/axiomgen/internal/codegen`. При изменении generator необходимо одновременно обновлять:

- этот документ;
- codegen tests;
- examples;
- consumer-facing generated-code example.
