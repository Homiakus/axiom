# Axiom Durable Serialized Surfaces Inventory (STORE-004)

Этот документ фиксирует полную инвентаризацию всех сериализуемых и персистентных структур состояния в Axiom (`axiom` core, `adgo`, `flow`, `pebble`, `filestore`).

Инвентаризация является машиночитаемой и поддерживается в коде пакетом [`internal/durableserial`](file:///d:/Programms/axiom/internal/durableserial/inventory.go).

---

## 1. Категории поверхностей состояния

1. **`core_execution`** — состояние execution core runtime в Pebble и Memory хранилищах.
2. **`core_task_history`** — история событий, Activity Tasks и дедупликационные индексы.
3. **`adgo_execution`** — персистентные снимки версий и latest-указатели в ADGO FileStore и PebbleStore.
4. **`adgo_inbox`** — дедуплицированные очереди входящих событий workflow.
5. **`adgo_locking`** — структурированные файловые и семафорные блокировки владения с heartbeat.
6. **`flow_state_outbox`** — типизированное состояние Flow, история переходов и durable outbox intents.
7. **`schedule_router`** — расписания запусков (Schedule) и метрики здоровья воркеров (Router Health).
8. **`admission_control`** — снимки токен-бакетов и лимитов параллелизма (Admission Controller).
9. **`retention_repair`** — политики хранения снимков и планы восстановления целостности.
10. **`artifact_manifest`** — скомпилированные манифесты планов AXM и PlanDigest.

---

## 2. Реестр сериализуемых структур

| ID | Название | Пакет | Хранилище / Ключ | Кодек / Формат | Схема / Версия | Обещание совместимости | Фикстура |
|---|---|---|---|---|---|---|---|
| **CORE-PEBBLE-EXECUTION** | Core Pebble Execution State | `internal/store/pebble` | Pebble `exec/<id>` | JSON (default) / Gob (opt-in) | `Execution.Version` (uint64), `meta/axiom-store-schema` (`"1"`) | Format-pinned fail-fast | `testdata/compat/core_pebble_execution.json` |
| **CORE-PEBBLE-HISTORY** | Core Pebble History Log | `internal/store/pebble` | Pebble `hist/<id>/<seq>` | JSON / Gob | `HistoryEntry.Seq` (int), `"1"` | Immutable append-only | `testdata/compat/core_pebble_history.json` |
| **CORE-PEBBLE-TASK** | Core Pebble Activity Task | `internal/store/pebble` | Pebble `task/<id>/<task_id>` | JSON / Gob | `ActivityTask.Attempt` (int), `"1"` | Format-pinned fail-fast | `testdata/compat/core_pebble_task.json` |
| **CORE-PEBBLE-TASK-DEDUP** | Core Pebble Task Dedup Index | `internal/store/pebble` | Pebble `tdedup/<id>/...` | JSON string (task ID) | Reference ID | Format-pinned fail-fast | `testdata/compat/core_pebble_task_dedup.json` |
| **ADGO-FILESTORE-COMMIT** | ADGO FileStore Execution Commit | `adgo` | File `commits/<ver_20d>.json` | JSON | `Execution.Version`, `PlanDigest` | Atomic temp+fsync+rename | `testdata/compat/adgo_filestore_commit.json` |
| **ADGO-FILESTORE-INBOX** | ADGO FileStore Inbox Event | `adgo` | File `inbox/<event_id>.json` | JSON | `Event.ID`, `Event.At` | Atomic file write | `testdata/compat/adgo_filestore_inbox.json` |
| **ADGO-FILE-LOCK** | ADGO Ownership Lock Record | `adgo` | File `locks/<id>.lock` | JSON (fileLockRecord) / Unix timestamp | `Owner`, `AcquiredAt`, `HeartbeatAt` | Atomic file write + legacy fallback | `testdata/compat/adgo_file_lock.json` |
| **ADGO-PEBBLE-LATEST** | ADGO Pebble Latest State Pointer | `adgo` | Pebble `adgo/e/<hash>/latest` | JSON | `Execution.Version`, `meta/adgo-store-schema` (`"1"`) | Optimistic CAS versioning | `testdata/compat/adgo_pebble_latest.json` |
| **ADGO-PEBBLE-VERSION** | ADGO Pebble Version Snapshot | `adgo` | Pebble `adgo/e/<hash>/v/<ver_20d>` | JSON | `Execution.Version`, `meta/adgo-store-schema` (`"1"`) | Immutable append-only snapshots | `testdata/compat/adgo_pebble_version.json` |
| **ADGO-PEBBLE-INBOX** | ADGO Pebble Inbox Event | `adgo` | Pebble `adgo/e/<hash>/inbox/<hash>` | JSON | `Event.ID`, `Event.At` | Deduplicated sorted queue | `testdata/compat/adgo_pebble_inbox.json` |
| **ADGO-PEBBLE-CATALOG** | ADGO Pebble Catalog Index | `adgo` | Pebble `adgo/c/<hash>` | Raw UTF-8 string | Execution ID string | Discovery index | `testdata/compat/adgo_pebble_catalog.txt` |
| **FLOW-PEBBLE-STATE** | Flow Pebble State Record | `axiom` | Pebble `flow/state/<flow>/<id>` | Raw Binary (`[]byte`) | State byte slice | Synchronous durability batch | `testdata/compat/flow_pebble_state.bin` |
| **FLOW-PEBBLE-HISTORY** | Flow Pebble History Record | `axiom` | Pebble `flow/hist/<flow>/<id>/<seq>` | JSON (`FlowHistoryEntry`) | `Sequence` (int), `Type` | Immutable append-only history | `testdata/compat/flow_pebble_history.json` |
| **FLOW-OUTBOX-INTENT** | Flow Outbox Durable Effect Intent | `axiom` | Flow history (`durable_effect_intent`) | JSON (`durableEffectIntentRecord`) | `EffectName`, `DeduplicationID` | Exactly-once outbox boundary | `testdata/compat/flow_outbox_intent.json` |
| **ADGO-SCHEDULE-STORE** | ADGO Schedule Store Record | `adgo` | Memory/Durable `Schedule` | JSON | `Schedule.Version` (uint64) | Optimistic CAS versioning | `testdata/compat/adgo_schedule.json` |
| **ADGO-ROUTER-HEALTH** | ADGO Router Health State | `adgo` | Memory/Durable `WorkerHealth` | JSON | `WorkerID`, `CooldownUntil` | Circuit breaker & metrics | `testdata/compat/adgo_router_health.json` |
| **ADGO-ADMISSION-STATE** | ADGO Admission Controller State | `adgo` | Memory/File `admissionSnapshot` | JSON / struct | `PermitsInUse`, `TokenBalance` | Rate limit & token bucket | `testdata/compat/adgo_admission_state.json` |
| **ADGO-RETENTION-REPAIR** | ADGO Retention & Repair Metadata | `adgo` | Memory/Config `RetentionPolicy` | JSON | `RetainVersions`, `MaxAge` | Pruning & repair planning | `testdata/compat/adgo_retention_repair.json` |
| **AXM-PLAN-MANIFEST** | AXM Compiled Plan Manifest | `model` / `compiler` | Byte stream / JSON | JSON / AST | `CompilerVersion`, `ModuleHash` | Deterministic digest verification | `testdata/compat/axm_plan_manifest.json` |

---

## 3. Политика миграции и валидации форматов

1. **Fail-Fast при инициализации:** каждый персистентный бэкенд (`PebbleStore`, `FileStore`, `FlowStore`) валидирует маркер схемы и формата при вызове `Open`. Несовместимые или будущие версии немедленно возвращают ошибку до модификации пользовательских данных.
2. **Изоляция маркеров:** маркеры Core Pebble (`meta/axiom-store-schema`) и ADGO Pebble (`meta/adgo-store-schema`) строго разделены и не пересекаются.
3. **Безопасная адаптация legacy-хранилищ:** базы данных без маркеров валидируют структуру существующих данных на отсутствие повреждений перед записью маркера текущей версии.
4. **Непрерываемость локальных атомарных коммитов:** запись метаданных и версий выполняется синхронно (`Sync`) и неделимо.
