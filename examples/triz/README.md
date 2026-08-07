# TRIZ frontend

`hydropilot_mini.axm` использует user-facing TRIZ syntax (`system ...`), а не canonical AXM v0 syntax (`domain ...`). Axiom нормализует его в canonical representation перед compilation.

```text
TRIZ source
    ↓
axiom.NormalizeTRIZ
    ├─ normalized AXM
    ├─ source map
    ├─ diagnostics
    └─ compiled Module
```

Этот frontend полезен для tooling/редакторов, где важно сохранить связь между исходным DSL и canonical AXM.

## Запуск

Из корня репозитория:

```bash
go run ./examples/triz
```

Пример печатает имя domain, число diagnostics/source-map entries и нормализованный AXM source.

## Какой API использовать

- `axiom.NormalizeTRIZ` — когда нужны normalized source, diagnostics и source map для IDE/Studio/tooling.
- `axiom.CompileAny` — когда нужен только compiled `Module` и вход может быть AXM или TRIZ.
- `axiom.CompilePlan` / declarative frontends — предпочтительнее для обычного application runtime, где canonical `Plan` является главным контрактом.

Не используйте `Must*` варианты для пользовательского ввода: ошибки DSL должны возвращаться вызывающему коду как diagnostics, а не превращаться в panic.
