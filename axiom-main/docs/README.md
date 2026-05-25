# Документация Axiom

## Основные документы

- [README.md](../README.md) — обзор проекта, быстрый старт, полное API, архитектура, коды ошибок
- [axiom-file-specification.md](axiom-file-specification.md) — полная спецификация `.axm` DSL: лексика, выражения, декларации (domain, signal, context, computed, fact, policy, activity, rule, claim, query), IR и валидация
- [axiom-crfg.md](axiom-crfg.md) — контекстно-реактивная модель исполнения (CRFG): сущности, runtime pipeline, execution lifecycle, история и replay, индексы, fixpoint, timers, policies, claims, explainability
- [axiomgen.md](axiomgen.md) — кодогенератор axiomgen: интерактивный TUI, CLI-флаги, типизированная обвязка, умный merge при изменении `.axm`, diff-отчёт

## Проектные документы

- [ТЗ на улучшение.md](ТЗ%20на%20улучшение.md) — техническое задание: целевая архитектура (ID-based state, bitset operations, expression VM), приоритеты P0/P1/P2
