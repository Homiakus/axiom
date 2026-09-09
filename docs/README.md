# Документация Axiom

Документация разделена по задачам, чтобы корневой README оставался пригодным для первого знакомства, а подробности можно было находить по конкретной цели.

## Начало работы

- [Обзор и быстрый старт](../README.md)
- [Руководство по выбору frontend API](api-guide.md)
- [Классификация и уровни публичного API (5 tiers)](api-tiering.md)
- [Машиночитаемый реестр уровней API](api-tiering.json)
- [Интеграционные профили (Embedded, Durable Single Node, Distributed Production)](integration-profiles.md)
- [Полное эталонное production-приложение](production-reference-application.md)
- [Инвентарь депрекаций публичного API](deprecation-inventory.md)
- [Таксономия и классификация ошибок](error-taxonomy.md)
- [Каталог примеров](../examples/README.md)
- [Локальная разработка](../DEVELOPMENT.md)
- [Правила внесения изменений](../CONTRIBUTING.md)
- [Версионирование и совместимость](versioning.md)
- [Changelog](../CHANGELOG.md)
- [Release notes v0.1.0](releases/v0.1.0.md)
- [Политика безопасности](../SECURITY.md)

## Архитектура и runtime

- [Архитектура](../ARCHITECTURE.md)
- [Architecture FMEA и жизненный цикл инженерных рисков](architecture-fmea.md)
- [Машиночитаемый архитектурный risk register](architecture-risk-register.json)
- [High-leverage architecture audit — 2026-09-06](high-leverage-architecture-audit-2026-09-06.md)
- [Mathematical and cybernetic audit of compiled runtime correctness — 2026-09-08](runtime-mathematical-correctness-audit-2026-09-08.md)
- [Runtime-семантика и текущие ограничения](runtime-semantics.md)
- [PostgreSQL durable store: архитектура, таблицы, CAS и multi-host fencing](postgres-durable-store.md)
- [Durable Flow effects: outbox, recovery и exactly-once boundary](flow-durability.md)
- [Реестр общих персистентных примитивов](durable-primitives-inventory.md)
- [Инвентарь сериализованных поверхностей](serialized-surfaces.md)
- [Классификация вызовов времени и часов](clock-inventory.md)
- [Концептуальная модель CRFG](axiom-crfg.md)
- [Go-first architecture](go-first-architecture.md)

## Языки и инструменты

- [Спецификация AXM](axiom-file-specification.md)
- [Кодогенератор axiomgen](axiomgen.md)
- [Примеры AXM](../examples/axiom-files/README.md)
- [Примеры TOML](../examples/table/)
- [Примеры TRIZ normalization](../examples/triz/)

## Качество и релиз

- [Quality gate интеграционной полноты](quality-gate.md)
- [Регламенты аварийного реагирования и Runbooks](operational-runbooks.md)
- [Отказоустойчивость, метрики и health-пробы](observability-and-health.md)
- [Quality Loop и автоматизированные проверки](QUALITY_LOOP.md)
- [Актуальный usability audit](../reports/usability-audit-2026-08-07.md)
- [Актуальный benchmark baseline](../benchmarks/latest.md)
- [Pre-v1 release policy](versioning.md)
- [Changelog](../CHANGELOG.md)

## Статус документов

| Документ | Статус |
|---|---|
| `README.md` | Public API overview и quick start |
| `api-guide.md` | Рекомендуемый путь выбора frontend и runtime API |
| `api-tiering.md` | Руководство по 5 уровням публичного API: stable facade, extension SPI, advanced, internalization, deprecated |
| `api-tiering.json` | Машиночитаемая классификация всех 849 публичных символов API с маппингом на фичи |
| `integration-profiles.md` | Три канонических профиля интеграции (Embedded, Durable Single Node, Distributed Production) |
| `production-reference-application.md` | Полное эталонное production-приложение, объединяющее все 14 runtime-алгоритмов |
| `deprecation-inventory.md` | Инвентарь устаревших конструкторов и расписание pre-v1 депрекаций |
| `error-taxonomy.md` | Канонический контракт классификации ошибок (`diag.Error`, `errors.Is`, `FailureClass`) |
| `observability-and-health.md` | Ограничение кардинальности метрик (OPS-001) и разделение liveness/readiness проб (OPS-002) |
| `operational-runbooks.md` | Процедуры устранения сбоев, восстановления данных, утечек блокировок и отката версий (OPS-003) |
| `runtime-semantics.md` | Declarative Engine runtime contract и failure boundaries |
| `postgres-durable-store.md` | Первоклассное PostgreSQL хранилище: multi-host CAS, task leasing, fencing, inbox и миграции |
| `runtime-mathematical-correctness-audit-2026-09-08.md` | Evidence-аудит эквивалентности model/compiled runtime/replay: semantic digest, fast/reference differential testing, dependency closure, writes, fixpoint semantics и replay verification |
| `flow-durability.md` | Канонический контракт durable Flow outbox/recovery |
| `durable-primitives-inventory.md` | Анализ и статус общих примитивов Core vs ADGO |
| `serialized-surfaces.md` | Реестр 19 сериализованных поверхностей состояния |
| `clock-inventory.md` | Каноническая классификация вызовов `time.*` и `durabletime.Clock` |
| `architecture-fmea.md` | Каноническая методика FMEA, lifecycle `R-XXX` и правила связи рисков с `F-XXX`/`T-XXX` |
| `architecture-risk-register.json` | Машиночитаемый FMEA-регистр для CI-проверки RPN, состояний и ссылок на `MASTER_PLAN.md` |
| `algorithm-integration-matrix.json` | Машиночитаемая матрица интеграции алгоритмов: фича -> реализация -> API -> рантайм -> тесты -> примеры |
| `high-leverage-architecture-audit-2026-09-06.md` | Evidence-аудит точек максимального архитектурного leverage и хрупких семантических границ; не является параллельным roadmap |
| `quality-gate.md` | Quality gate интеграционной полноты (T-086): 8 обязательных измерений для каждой фичи |
| `versioning.md` | Политика pre-v1 совместимости, release gate и workflow публикации |
| `QUALITY_LOOP.md` | Регламент автоматического тестирования мутаций и краевых случаев |
| `CHANGELOG.md` | Текущий публичный release history / planned changes |
| `releases/v0.1.0.md` | Release notes первого versioned baseline |
| `ARCHITECTURE.md` | Подтверждён текущим code path; ограничения указаны явно |
| `DEVELOPMENT.md` | Локальная разработка и проверки, согласованные с `.github/workflows/ci.yml` |
| `axiom-file-specification.md` | Описывает реализованный parser/compiler/runtime contract v0.1.0 |
| `axiomgen.md` | Описывает текущий `cmd/axiomgen` |
| `go-first-architecture.md` | Краткий обзор; подробный выбор API вынесен в `api-guide.md` |
