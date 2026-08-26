# Clock and Time Usage Inventory in Axiom

Этот документ содержит классификацию всех вызовов `time.*` (`time.Now`, `time.NewTimer`, `time.NewTicker`, `time.After`, `time.Since`, `time.Sleep`) в production-пакетах репозитория Axiom согласно задаче **TIME-001**.

Канонический реестр и автоматические тесты соответствия находятся в [`internal/durabletime/inventory.go`](file:///d:/Programms/axiom/internal/durabletime/inventory.go).

---

## 1. Категории использования времени

1. **`durable_decision` (Durable Decision Time)**:
   - Время, влияющее на детерминированные переходы state machine, проверку допустимости выполнения, retry eligibility и отбор задач.
   - **Инвариант:** Обязано использовать внедряемый `Clock` (`internal/durabletime.Clock`), прямой вызов `time.Now()` в production-коде запрещён.

2. **`lease_fencing` (Lease & Fencing Time)**:
   - Вычисление срока действия блокировок, heartbeat-интервалов и fencing tokens.

3. **`retry_schedule_deadline` (Retry & Schedule Deadline Time)**:
   - Расчёт экспоненциального/фиксированного backoff, таймеры задержки планировщика и speculation hedge delay.

4. **`persisted_event_timestamp` (Persisted Event Timestamp)**:
   - Информационные метки времени (`CreatedAt`, `UpdatedAt`, `CompletedAt`, `HistoryEntry.At`), сохраняемые исключительно для аудита, телеметрии и логов.

5. **`observability_elapsed` (Observability & Benchmark Elapsed Time)**:
   - Измерение задержки и времени выполнения операций (`time.Since(...)` для метрик Prometheus, логов и трассировок).

6. **`os_freshness_boundary` (OS / Filesystem Freshness Boundary)**:
   - Проверка `mtime` файлов блокировок на локальной файловой системе (`adgo/file_lock.go`) для безопасного восстановления после аварийного завершения процесса (crash recovery).

7. **`test_only_wait` (Test Simulation & Waits)**:
   - Ожидания в тестовых сценариях и интеграционных harness.

---

## 2. Матрица классификации ключевых модулей

| Модуль / Файл | Функция | Вызов | Категория | Требует Clock Injection? | Обоснование |
|---|---|---|---|---|---|
| `adgo/admission.go` | `MemoryAdmissionController.Acquire` | `time.Now` | `lease_fencing` | **Да (TIME-002)** | Проверка истечения лимитов в памяти |
| `adgo/admission.go` | `MemoryAdmissionController.refill` | `time.Now` | `lease_fencing` | **Да (TIME-002)** | Пополнение токенов rate limiter |
| `adgo/admission_file.go` | `FileAdmissionController.Acquire` | `time.Now` | `os_freshness_boundary` | Нет | Deadline ожидания файлового лока |
| `adgo/file_lock.go` | `withOwnedFileLock` | `time.Now` | `os_freshness_boundary` | Нет | Запись timestamp владельца в lockfile |
| `adgo/file_lock.go` | `removeStaleFileLock` | `time.Since` | `os_freshness_boundary` | Нет | Сравнение с `mtime` файла на диске |
| `adgo/file_lock_heartbeat.go` | `startFileLockHeartbeat` | `time.NewTicker` | `lease_fencing` | Нет | Фоновый цикл обновления `mtime` |
| `adgo/policy.go` | `PolicyDecider.Evaluate` | `time.Now` | `retry_schedule_deadline` | **Да (TIME-003)** | Оценка backoff deadline |
| `adgo/speculation.go` | `HedgeExecutor.Execute` | `time.NewTimer` | `retry_schedule_deadline` | **Да (TIME-003)** | Таймер задержки спекулятивного вызова |
| `adgo/retention.go` | `RetentionManager.Purge` | `time.Now` | `durable_decision` | **Да** | Порог устаревания данных |
| `adgo/repair.go` | `DependencyRepairPlanner.CheckStale` | `time.Now` | `lease_fencing` | **Да** | Детектор зависших lease |
| `adgo/runtime.go` | `Runtime.ExecuteNode` | `time.Since` | `observability_elapsed` | Нет | Метрика длительности ноды |
| `adgo/runtime.go` | `Runtime.appendHistory` | `time.Now` | `persisted_event_timestamp` | Нет | Информационный timestamp события |
| `internal/runtime/types.go` | `systemClock.Now` | `time.Now` | `durable_decision` | **Да** | Системный адаптер `Clock` |
| `internal/runtime/retry_store.go` | `DrainDueTasks` | `time.NewTimer` | `retry_schedule_deadline` | **Да (TIME-003)** | Таймер сна retry-планировщика |
| `internal/runtime/worker.go` | `Worker.Run` | `time.NewTicker` | `retry_schedule_deadline` | Нет | Интервал поллинга очереди |
| `internal/store/pebble/transaction.go` | `Transaction.Commit` | `time.Now` | `persisted_event_timestamp` | Нет | Запись `UpdatedAt` на задаче |
| `internal/store/pebble/transaction.go` | `Transaction.AcquireLease` | `time.Now` | `lease_fencing` | Нет | Запись `LockedUntil` |
| `flow_outbox.go` | `Outbox.Enqueue` | `time.Now` | `persisted_event_timestamp` | Нет | `CreatedAt` outbox-записи |
