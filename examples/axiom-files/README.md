# Примеры AXM

Каталог содержит `.axm` models для документации, compiler/runtime tests и ручных экспериментов.

## Файлы

| Файл | Domain | Назначение |
|---|---|---|
| `welcome.axm` | `Welcome` | signal, context, computed, fact, policy, activity, rules и claim |
| `checkout.axm` | `Checkout` | несколько activities, facts, claims и policies |
| `claims.axm` | `Claims` | инварианты и expression forms |
| `reminder.axm` | `Reminder` | timer trigger и повторный сценарий на уровне модели |

## Проверка compilation

Из корня репозитория:

```bash
go test ./...
```

Каждый `.axm` файл в каталоге компилируется тестом `examples/axiom-files/examples_test.go`.

## Генерация Go activity boundary

```bash
go run ./cmd/axiomgen \
  --file examples/axiom-files/welcome.axm \
  --out ./generated \
  --package generated
```

Generator не имеет интерактивного режима. Он печатает JSON result и может создать или обновить files в `--out`.

Подробнее: [документация axiomgen](../../docs/axiomgen.md).

## Правило изменения examples

При изменении AXM syntax или runtime semantics:

1. обновите соответствующий example;
2. добавьте или обновите test, который действительно читает этот file;
3. выполните `go test ./...`;
4. обновите `docs/axiom-file-specification.md` только после подтверждения parser/compiler/runtime support.
