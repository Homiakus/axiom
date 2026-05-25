# Функциональный анализ Axiom

## Объект главной функции

Главный объект воздействия — не код и не runtime, а:

```text
ментальная модель разработчика
```

Axiom должен преобразовать её:

```text
до:
  процесс держится в голове, коде, if/else, FSM, retry handlers

после:
  процесс виден как набор независимых правил поведения
```

## Система и надсистема

```text
Надсистема:
  Go ecosystem
  browser/editor
  filesystem/git
  CI/CD
  database
  device/payment/email APIs

Система:
  Axiom TRIZ DSL
  normalizer
  runtime
  codegen
  Rule Studio

Подсистемы:
  parser
  validator
  IR
  scheduler
  worker
  history
  replay
  simulator
```

## Полезные функции элементов

| Элемент | Полезная функция |
|---|---|
| `state` | хранит durable память процесса |
| `event` | вводит изменения из внешнего мира |
| `condition` | даёт имя важной истинности |
| `rule` | связывает условия, функцию и записи |
| `function` | выполняет внешнюю работу |
| `profile` | задаёт безопасность исполнения |
| `always` | запрещает опасные состояния |
| `history` | сохраняет причинность |
| `replay` | восстанавливает исполнение |
| `codegen` | связывает DSL и Go типобезопасно |
| `Rule Studio` | показывает поведение человеку |

## Вредные функции

| Элемент/явление | Вред |
|---|---|
| слишком много DSL-сущностей | когнитивная нагрузка |
| ручные policies в каждом месте | шум и дублирование |
| скрытая связь rules через state | ощущение магии |
| сложный expression language | трудно валидировать |
| action имеет доступ к runtime mutation | ломает replay и explainability |
| отсутствие why/why-not | недоверие к декларативной модели |
| слабая диагностика | ошибки переходят в runtime |

## Недостаточные функции

| Функция | Что не хватает |
|---|---|
| редактирование правил | визуального сценарного слоя |
| симуляция | mock outputs и propagation |
| safety | design checker по actuator paths |
| codegen | full round-trip DSL ↔ Go |
| migration | конвертер Axiom v0 → TRIZ layer |
| runtime observability | live trace, pending actions, replay viewer |

## Идеальный функциональный баланс

Пользовательский слой должен быть малым:

```text
state + event + rule + function + always
```

Runtime-слой должен быть сильным:

```text
IR + indexes + history + replay + queue + workers + validators
```
