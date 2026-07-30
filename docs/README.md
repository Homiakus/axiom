# Документация Axiom

Документация разделена по задачам, чтобы корневой README оставался кратким и пригодным для первого знакомства.

## Начало работы

- [Обзор и быстрый старт](../README.md)
- [Локальная разработка](../DEVELOPMENT.md)
- [Правила внесения изменений](../CONTRIBUTING.md)
- [Политика безопасности](../SECURITY.md)

## Архитектура и runtime

- [Архитектура](../ARCHITECTURE.md)
- [Runtime-семантика и текущие ограничения](runtime-semantics.md)
- [Концептуальная модель CRFG](axiom-crfg.md)
- [Go-first architecture](go-first-architecture.md)

## Языки и инструменты

- [Спецификация AXM](axiom-file-specification.md)
- [Кодогенератор axiomgen](axiomgen.md)
- [Примеры AXM](../examples/axiom-files/README.md)
- [Примеры TOML](../examples/table/)
- [Примеры TRIZ normalization](../examples/triz/)

## Производительность

- [Актуальный benchmark baseline](../benchmarks/latest.md)

## Статус документов

| Документ | Статус |
|---|---|
| `README.md` | Подтверждён public API и CI commands |
| `ARCHITECTURE.md` | Подтверждён текущим code path; ограничения указаны явно |
| `DEVELOPMENT.md` | Основан на `.github/workflows/test.yml` |
| `axiom-file-specification.md` | Описывает реализованное подмножество parser/compiler/runtime |
| `axiomgen.md` | Описывает текущий `cmd/axiomgen` |
| `go-first-architecture.md` | Краткий обзор; требует расширения либо объединения с `ARCHITECTURE.md` |
