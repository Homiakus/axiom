# Axiom TRIZ

Эта папка описывает новый пользовательский слой Axiom: человек пишет правила
поведения, а runtime сам строит зависимости, вызывает Go-функции, пишет историю,
восстанавливается после сбоев и объясняет решения.

Документация согласована с текущим проектом:

- `axiom-main` уже реализует Axiom v0: `domain`, `signal`, `context`,
  `computed`, `fact`, `policy`, `activity`, `rule`, `claim`, `query`;
- TRIZ-слой предлагает более читаемый язык поверх него:
  `system`, `event`, `state`, `condition`, `profile`, `function`, `always`,
  `view`;
- `axiom-studio` является локальным визуальным редактором и симулятором правил.

## Главная идея

```text
Пользователь описывает законы поведения.
Normalizer переводит их в строгий IR.
Runtime исполняет IR, сохраняет историю и объясняет результат.
```

## Карта терминов

| Пользовательский слой | Runtime / Axiom v0 | Смысл |
|---|---|---|
| `system` | `domain` | имя модели |
| `state` | `context` | durable state |
| `event` | `signal` | внешний или внутренний вход |
| `condition` | `computed` + `fact` | именованная чистая истинность |
| `function` | `activity` | управляемый вызов Go-кода |
| `profile` | `policy` | timeout, retry, idempotency, audit |
| `always` | `claim` | invariant безопасности |
| `view` | `query` | read-only projection |
| `rule when/do/then` | `rule on/when/require/run/write` | единица поведения |

## Как читать

1. [PROJECT_ANALYSIS.md](PROJECT_ANALYSIS.md) — краткое исследование проекта,
   текущего кода и разрыва между Axiom v0 и TRIZ-слоем.
2. [01-triz](01-triz) — почему нужен новый слой и что он скрывает.
3. [02-model](02-model) — пользовательская модель: state, event, rule,
   function, always.
4. [03-dsl](03-dsl) — синтаксис TRIZ DSL и правила стиля.
5. [04-runtime](04-runtime) — как TRIZ DSL нормализуется и исполняется.
6. [05-go](05-go) — граница с Go: codegen, контракт функций, тесты.
7. [06-studio](06-studio) — Rule Studio как редактор, симулятор и объяснитель.
8. [07-hydropilot](07-hydropilot) — доменный пример для гидропоники.
9. [08-production](08-production) — roadmap, validation, observability,
   security, migration.

## Что является source of truth

Сейчас исполняемый DSL находится в `axiom-main` и описан в:

- `axiom-main/docs/axiom-file-specification.md`;
- `axiom-main/docs/axiom-crfg.md`;
- `axiom-main/examples/hydropilot/hydropilot.axm`.

TRIZ DSL в этой папке — целевой пользовательский слой. Его нельзя напрямую
считать уже реализованным синтаксисом, пока не появился normalizer
`TRIZ DSL -> Axiom v0 IR`.

## Критерий успеха

Новый подход считается готовым, когда один и тот же сценарий можно:

- прочитать как поведение, без runtime-терминов;
- нормализовать в текущие сущности Axiom v0;
- сгенерировать типизированную Go-границу;
- симулировать в Studio;
- восстановить и объяснить по history.
