# Coffee machine — advanced end-to-end reference

Этот пример показывает большую доменную модель кофейного автомата: деньги в целых копейках, остатки ингредиентов, физические activities, idempotency, policies, claims, runtime state/history и генерацию диаграмм.

Для первого знакомства с API начинайте не отсюда, а с [`../model/`](../model/) и [`../order/`](../order/). `coffee-machine` намеренно большой: его задача — показать, как части Axiom взаимодействуют в одном насыщенном сценарии.

## Запуск

Из корня репозитория:

```bash
go run ./examples/coffee-machine
```

CI проверяет не только exit code, но и ключевые итоговые показатели: принятые/возвращённые деньги, выручку, кассу, нулевой кредит и число выданных напитков.

## Что изучать в примере

- хранение денег целыми минимальными единицами вместо `float64`;
- typed Go events и state;
- resource guards перед физической операцией;
- внешние activities и idempotency keys;
- retry/timeout/concurrency policy;
- claims для денежных и ресурсных инвариантов;
- итоговый state и history;
- диаграммы и визуализацию модели.

Сгенерированные/поддерживаемые материалы диаграмм описаны в [`DIAGRAMS.md`](DIAGRAMS.md).

## О стиле API

Этот файл также сохраняет часть compatibility-style выражений (`Int("...")`, `GT/GTE`, `Add/Sub`) из ранних итераций библиотеки. Они поддерживаются и корректны, но для **нового** большого application code предпочтителен стиль из `examples/model` и `examples/order`: reusable `model.Key`, `StateField`/`EventField` и строгие typed helpers (`Equal`, `GreaterOrEqual`, `PlusField` и т. п.).

Так разделяется назначение примеров: `model/order` задают современный рекомендуемый DX, а `coffee-machine` остаётся широким regression/reference scenario и одновременно проверяет обратную совместимость публичного builder API.
