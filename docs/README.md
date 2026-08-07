# Документация Axiom

Документация разделена по задачам, чтобы корневой README оставался пригодным для первого знакомства, а подробности можно было находить по конкретной цели.

## Начало работы

- [Обзор и быстрый старт](../README.md)
- [Какой публичный API выбирать](api-guide.md)
- [Каталог примеров](../examples/README.md)
- [Локальная разработка](../DEVELOPMENT.md)
- [Правила внесения изменений](../CONTRIBUTING.md)
- [Версионирование и совместимость](versioning.md)
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
| `api-guide.md` | Рекомендуемый путь выбора frontend и runtime API |
| `versioning.md` | Политика pre-v1 совместимости и требования к релизу |
| `ARCHITECTURE.md` | Подтверждён текущим code path; ограничения указаны явно |
| `DEVELOPMENT.md` | Основан на `.github/workflows/test.yml` |
| `axiom-file-specification.md` | Описывает реализованное подмножество parser/compiler/runtime |
| `axiomgen.md` | Описывает текущий `cmd/axiomgen` |
| `go-first-architecture.md` | Краткий обзор; подробный выбор API вынесен в `api-guide.md` |
