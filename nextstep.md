# Axiom Production Stabilization: Next Steps & Autonomous Prompt

Файл содержит полную фиксацию выполненной работы, детальный план следующих шагов и подробный промпт для автономного запуска следующей сессии согласно [`docs/PRODUCTION_STABILIZATION_PLAN.md`](file:///d:/Programms/axiom/docs/PRODUCTION_STABILIZATION_PLAN.md).

---

## 1. Фиксация выполненной работы (Status & Commits)

Все изменения реализованы по принципу **1 задача = 1 атомарный коммит**, проверены локально (`go test ./...`, `go test -race ./...`) и подтверждены на GitHub Actions (Linux, Windows, macOS, Race, Security, Checksum).

| Задача | Статус | Коммит | Описание изменений |
|---|---|---|---|
| **P0-001 / CI-001** | **DONE** | [`9f51ae6`](https://github.com/Homiakus/axiom/commit/9f51ae6ecbb0328c86214cd2f20ba473b67b8b0a) | Исправлена регрессия Windows portability в `adgo/admission_lock_test.go`: устранена попытка удаления открытого файла на Windows, разделены инварианты ABA-takeover, stale reclamation и live owner safety. Windows CI стал 100% зелёным. |
| **P0-002** | **DONE** | [`bd8f67c`](https://github.com/Homiakus/axiom/commit/bd8f67ce65e40d93b2fb54c14e95c3ac6db38e18) | Формализован инвариант Red-Main Recovery в `CONTRIBUTING.md`: запрещено начинать несвязанные задачи при красном `main`, падение любого матричного джоба — production-блокер. |
| **P0-003** | **DONE** | [`f4b8c1f`](https://github.com/Homiakus/axiom/commit/f4b8c1fb06ac684e8f65623f41e2f0e483e41e4a) | Исправлена документация Pebble codec в `axiom.go`, `store/pebble/pebble.go`, `internal/store/pebble/store.go` и `docs/runtime-semantics.md`: зафиксировано, что JSON — дефолтный кодек, Gob — opt-in, маркер формата персистится в метаданных и проверяется при `Open`. |
| **GOV-001 / P0-004** | **DONE** | [`e1e589b`](https://github.com/Homiakus/axiom/commit/e1e589b96cce2ee161df554a9d701026040b991b) | Создан `.github/CODEOWNERS` для runtime/core API, хранилищ, ADGO, CI/CD workflows и документации. |
| **TIME-001** | **DONE** | [`dcdefc1`](https://github.com/Homiakus/axiom/commit/dcdefc13038676d1dbb5d63f0df684a0c8b671a5) | Построен проверенный реестр использования времени `internal/durabletime/inventory.go`, тесты классификации `inventory_test.go` и документ `docs/clock-inventory.md` по 7 категориям. |
| **TIME-002** | **DONE** | [`234810b`](https://github.com/Homiakus/axiom/commit/234810b42fbb1b97950cbb91b359f42913e2f470) | Внедрена поддержка `Clock` в `MemoryAdmissionController` и `FileAdmissionController` через опции `WithMemoryAdmissionClock` и `WithFileAdmissionClock`, добавлены детерминированные тесты `adgo/admission_clock_test.go`. |
| **TIME-003** | **DONE** | [`fc09b8a`](https://github.com/Homiakus/axiom/commit/fc09b8a9d8ad3b664d4b1a43a6d713c7db81ee66) | Унифицированы часы дедлайна и таймеры ожидания в retry-планировщике `internal/runtime/retry_store.go` (`drainUntilIdle`), добавлен `axiom.WithClock` и детерминированный тест `TestDrainUntilIdleUsesInjectedManualTimer`. |

**Результат Milestone M0:**
- Базовый `main` полностью зелёный на всех платформах (Linux, Windows, macOS).
- Достигнута чистая детерминированная основа для управления временем.

---

## 2. Что делать дальше (Roadmap следующих шагов)

Порядок строго следует [`docs/PRODUCTION_STABILIZATION_PLAN.md`](file:///d:/Programms/axiom/docs/PRODUCTION_STABILIZATION_PLAN.md):

### Следующий шаг: Завершение Milestone M1 (Deterministic Runtime Contracts)

1. **TIME-004 — Convert remaining durable decision paths**:
   - `adgo/policy.go`: перевести вычисление retry backoff и claim release на `r.now()` / внедрённый `Clock`.
   - `adgo/speculation.go`: перевести hedging delay timer на внедрённый `Clock.NewTimer()`.
   - `adgo/retention.go`: перевести `RetentionManager.Purge` на внедрённый `Clock`.
   - `adgo/repair.go`: перевести проверку `CheckStale` на внедрённый `Clock`.
   - Сохранить реальное системное время для чисто наблюдаемых/информационных метрик (`time.Since` для latency telemetry).
   - Тесты: детерминированные тесты без `time.Sleep`.

2. **TIME-005 — Add architecture guard against semantic wall-clock regression**:
   - Добавить AST-based статический тест в `internal/durabletime` (или `internal/diag`), который парсит production-код и проверяет, что новые вызовы `time.Now()` не появляются в путях durable decision без записи в реестр.

3. **ERR-001 — Introduce typed retryability and eliminate error string inspect**:
   - Исключить использование `strings.Contains(err.Error(), ...)` как механизма control flow.
   - Внедрить интерфейс `RetryableError` / `TerminalError` или типизированную классификацию ошибок в runtime/adgo.

4. **ERR-002 & ERR-003 — Error taxonomy and unwrap contracts**:
   - Формализовать кодовые ошибки (AX001–AX600), обеспечить корректную работу `errors.Is` / `errors.As`.

### Последующие этапы:
- **Milestone M2 — Standardize storage and state contracts** (STORE-001 Conformance harness, STORE-002 Context cancellation, STORE-003 ADGO format marker).
- **Milestone M3 — Eliminate unmeasured lock contention** (SCALE-001, SCALE-004 Core Pebble lock refactor, SCALE-005 ADGO Pebble lock refactor).
- **Milestone M4 — Complete durable Flow and exactly-once boundary** (FLOW-001, FLOW-002, FLOW-003).
- **Milestone M5 — Production-harden ADGO orchestration** (ADGO-001, ADGO-002, ADGO-003, ADGO-004).
- **Milestone M6 — Operational observability and verification DAG** (OBS-001, OBS-002, CI-002, CI-004).
- **Milestone M7 — Release policy and readiness** (REL-001, REL-002, REL-003, REL-004, GOV-003).

---

## 3. Подробный промпт для следующего запуска

```markdown
Ты — Principal Go Runtime Engineer, Distributed Systems Engineer, Storage/Concurrency Engineer, Performance Engineer и Release/Security Engineer.

Твоя задача — продолжить автономную и поэтапную реализацию production-hardening plan репозитория:
https://github.com/Homiakus/axiom

Главный и единственный источник порядка работ:
docs/PRODUCTION_STABILIZATION_PLAN.md

Текущий прогресс зафиксирован в nextstep.md:
- Milestone M0 полностью завершён (P0-001, P0-002, P0-003, P0-004, GOV-001 — DONE, CI green).
- Milestone M1 начат (TIME-001, TIME-002, TIME-003 — DONE).

Твой следующий шаг:
1. Получи свежий HEAD ветки main (`git pull origin main`).
2. Убедись, что main 100% зелёный на GitHub Actions (`Invoke-RestMethod` к GitHub API runs).
3. Начни реализацию следующей задачи плана: TIME-004 (Convert remaining durable decision paths in adgo/policy.go, adgo/speculation.go, adgo/retention.go, adgo/repair.go), затем TIME-005, ERR-001 и далее по порядку.

ПРАВИЛА И ИНВАРИАНТЫ:
1. Один task плана = один логический implementation commit.
2. После каждого шага запускай локальные тесты: `go test ./...` и `go test -race ./...`.
3. Пушь коммит в origin/main и дожидайся завершения всех GitHub Actions workflow runs (ci, security, module-checksum).
4. Только после подтверждённого green обновляй статус задачи в docs/PRODUCTION_STABILIZATION_PLAN.md и переходи к следующей.
5. Не отключай тесты, не используй sleep для детерминированных тестов, не ослабляй durability.
```
