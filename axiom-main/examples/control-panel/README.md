# Axiom Control Panel

Интерактивная TUI-программа для управления Axiom execution: запуск execution,
отправка сигналов, просмотр состояния и истории.

Это отдельная example-программа, а не часть библиотеки. Панель импортирует
`axiom/pkg/axiom` через локальный `replace axiom => ../..` и держит
TUI-зависимости (`bubbletea`, `bubbles`, `lipgloss`) в собственном `go.mod`,
чтобы корневой модуль Axiom оставался чистой библиотекой без UI-зависимостей.

## Запуск

```bash
# Из-за go.work в корне, нужно отключить workspace resolution:
cd examples/control-panel
GOWORK=off go run .
```

## Структура

```
examples/control-panel/
├── go.mod                          # Отдельный модуль: axiom-control-panel
├── control_panel_entrypoint.go     # Точка входа
└── internal/tui/
    ├── model.go                    # Bubble Tea модель
    ├── model_test.go
    ├── runtime.go                  # Управление Engine
    ├── runtime_test.go
    ├── validation.go               # Валидация .axm
    └── validation_test.go
```

## Проверка

```bash
cd examples/control-panel
GOWORK=off go build .
GOWORK=off go test ./...
```
