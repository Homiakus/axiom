# Project Analysis

## Что есть в проекте

Проект состоит из трех частей:

| Часть | Назначение |
|---|---|
| `axiom-main` | Go-библиотека, парсер `.axm`, компилятор, runtime, store, codegen и примеры |
| `axiom-studio` | локальный web-интерфейс для просмотра, симуляции и диагностики правил |
| `axiom_triz_full_docs` | целевая документация нового TRIZ-подхода |

`axiom-main` уже достаточно зрелый: есть публичный API, AST, compiler module,
dependency indexes, runtime engine, replay, memory/Pebble store, diagnostics,
examples и `axiomgen`.

## Текущий исполняемый язык

Реализованный язык использует runtime-термины:

```text
domain -> signal -> context -> computed -> fact -> policy -> activity
       -> rule -> claim -> query
```

Это хорошо для компилятора и runtime, но тяжело для пользователя: приходится
думать о том, что является fact, что computed, где policy, где activity, как
связать output с write.

## Новый подход

TRIZ-слой оставляет только понятия поведения:

```text
system
state
event
condition
function
profile
rule
always
view
```

Пользователь пишет:

```axiom
rule DosePHDown when:
  Solution.needPHDown
  CanDose
do critical:
  result = DosePHDown(event.zone, Solution.phDoseAmount)
then:
  set Solution.needPHDown = false
```

Normalizer разворачивает это в Axiom v0:

```text
condition -> computed/fact
function  -> activity
profile   -> policy
always    -> claim
view      -> query
do/then   -> run/write
```

## Главный разрыв

Документация раньше смешивала два уровня:

- говорила как будто TRIZ DSL уже является текущим исполняемым `.axm`;
- не фиксировала, где заканчивается пользовательский слой и начинается runtime;
- повторяла одни и те же идеи в разных разделах;
- HydroPilot Mini показывал идеальную модель, но не объяснял связь с реальным
  `examples/hydropilot/hydropilot.axm`.

Исправленная документация явно разделяет:

| Уровень | Статус |
|---|---|
| Axiom v0 DSL | реализован в `axiom-main` |
| TRIZ DSL | целевой пользовательский слой |
| Normalizer | нужен для перехода TRIZ DSL -> Axiom v0 |
| Rule Studio | локальный UI, сейчас отдельный MVP |
| HydroPilot v0 | реализованный большой пример |
| HydroPilot Mini | читаемый эталон нового стиля |

## Что нужно реализовать дальше

Приоритетный путь:

1. Зафиксировать TRIZ DSL как user-facing specification.
2. Написать parser/normalizer для `system/state/event/...`.
3. Генерировать Axiom v0 module или сразу normalized IR.
4. Дать Studio режим сравнения: source, normalized view, diagnostics.
5. Связать `axiomgen` с TRIZ DSL, чтобы Go-контракт оставался типобезопасным.
6. Мигрировать HydroPilot: сначала как Mini-модель, затем как полная модель.

## Документная политика

В этой папке больше не нужно повторять полную спецификацию Axiom v0. Она уже
есть в `axiom-main/docs`. Здесь важны:

- читаемая пользовательская модель;
- карта в runtime-термины;
- ограничения normalizer;
- примеры безопасного поведения;
- требования к Studio и production.
