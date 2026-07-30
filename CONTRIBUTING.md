# Внесение изменений в Axiom

## Принципы

1. Код и executable tests являются источником истины.
2. Изменение public API, DSL или runtime semantics сопровождается документацией и тестом.
3. Нельзя документировать parsed configuration как выполненную runtime guarantee.
4. External activity должна иметь явно описанную idempotency model.
5. Generated files не редактируются вручную, если они помечены `DO NOT EDIT`.

## Рабочий процесс

1. Создайте отдельную branch.
2. Добавьте минимальный failing test для исправляемого поведения.
3. Реализуйте изменение.
4. Обновите связанные документы и examples.
5. Выполните проверки из `DEVELOPMENT.md`.
6. В pull request опишите изменение contract, migration impact и ограничения.

## Изменения DSL

При изменении syntax или semantics проверьте:

- `internal/lang` parser и expression parser;
- `internal/compiler` validation и dependency indexes;
- slow и fast runtime paths;
- Go model renderer;
- TOML renderer;
- `axiomgen` type inference/code generation;
- examples;
- `docs/axiom-file-specification.md`;
- compatibility/replay implications и compiled hash.

Новую syntax нельзя считать поддерживаемой, пока она не имеет parser, compiler, runtime и test coverage.

## Изменения runtime

Документируйте:

- transaction boundary;
- history entries;
- failure state;
- retry and timeout behavior;
- idempotency and deduplication scope;
- concurrency and locking scope;
- replay compatibility.

## Изменения public API

- Не импортируйте `internal/*` из consumer-facing examples.
- Сохраняйте deprecated wrappers на согласованный migration period.
- Добавляйте package comments и doc comments для exported contracts, а не для очевидных implementation details.
- Проверяйте отдельный consumer module, как это делает CI.

## Документация

Команда в документации считается рабочей только если:

- path существует;
- flag существует;
- working directory указана;
- команда выполняется в CI либо покрыта отдельным docs test;
- destructive action содержит безопасные границы.

GitHub-ссылки должны быть обычными Markdown links. Obsidian wikilinks `[[...]]` не следует использовать в repository documentation.

## Pull request checklist

- [ ] `go mod tidy` не оставляет diff без причины.
- [ ] `go test ./...` проходит.
- [ ] Race tests критичных packages проходят.
- [ ] `go vet ./...` проходит.
- [ ] Coffee-machine example выполняется.
- [ ] Public API change имеет consumer-facing test.
- [ ] DSL change покрыт parser/compiler/runtime tests.
- [ ] Runtime behavior и history entries обновлены в документации.
- [ ] Новые или изменённые commands проверены.
- [ ] В diff нет secrets, tokens, персональных данных и локальных data directories.
