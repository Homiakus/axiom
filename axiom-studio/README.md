# Axiom Rule Studio — Go Production MVP

Локальный визуальный редактор и симулятор TRIZ-style Axiom rules. Работает как один Go-бинарник и отдаёт чистый HTML/CSS без frontend-фреймворков.

## Что внутри

- тёмная тема по умолчанию;
- адаптивная mobile-first вёрстка для телефонов и планшетов;
- карточки правил: trigger / allowed if / do / then / reads / writes;
- карточки actions/functions: callers, inputs, outputs, writes, safety hints;
- пошаговая симуляция события: runnable / blocked / unknown, planned actions, simulated writes, final state;
- production diagnostics по модели правил;
- встроенный source editor;
- экспорт Markdown-отчёта;
- генерация Go stubs для actions;
- endpoint `/healthz`;
- HTTP server timeouts и базовые security headers;
- конфиг адреса через `AXIOM_STUDIO_ADDR`.

## Запуск

```bash
go run . /path/to/hydropilot.ideal.rules.axm
```

или собранным бинарником:

```bash
./axiom-rule-studio-go /path/to/hydropilot.ideal.rules.axm
```

Открыть:

```text
http://127.0.0.1:8080
```

Другой адрес/порт:

```bash
AXIOM_STUDIO_ADDR=127.0.0.1:8090 ./axiom-rule-studio-go ./hydropilot.ideal.rules.axm
```

## Мобильный режим

На экранах до 820px интерфейс перестраивается в вертикальные секции:

- Project
- Rules
- Simulation
- Source

Верхние вкладки позволяют быстро переходить между секциями.

## Важное ограничение

Симулятор намеренно консервативный. Простые boolean/comparison expressions он вычисляет, а сложные выражения помечает как `UNKNOWN`, чтобы не создавать ложную уверенность.

## Структура

```text
main.go                  # server, parser, renderer, simulator
README.md                # this file
go.mod                   # no external dependencies
axiom-rule-studio-go     # built Linux binary
```
