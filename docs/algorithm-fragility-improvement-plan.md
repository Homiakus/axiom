# План повышения устойчивости алгоритмов Axiom

Статус: план, код не изменён  
Дата аудита: 2026-08-14  
Базовая ревизия: 0ad40aeb765da93e29f08d66983fd6c6400f363c  
Область: основной runtime, Pebble-хранилище и пакет adgo

Этот документ фиксирует доказанные точки хрупкости и порядок их устранения. Он не предлагает переписать систему целиком: каждый пункт начинается с минимального контрпримера, затем выбирается наименьший достаточный уровень вмешательства.

## 0. Границы и метод

- Анализ выполнен по исходному коду, тестам, документации и CI базовой ревизии.
- В базовой ревизии workflow test и module-checksum завершились успешно. Это подтверждает текущую регрессионную базу, но не опровергает приведённые ниже контрпримеры: соответствующие классы входов в ней отсутствуют.
- Записи со статусом CONFIRMED опираются на воспроизводимый статический контрпример или доказуемое нарушение контракта/инварианта.
- Гипотезы, которым нужна нагрузочная либо многопроцессная проверка, не объявлены дефектами и помечены TO VERIFY.
- Pull request #20 и #21 не входят в базовую ревизию. После их слияния затронутые runtime/timer-контракты нужно прогнать через этот план повторно.
- В этом коммите нет исправлений и новых тестов: только план работ.

Прогрессивный статус:

| Pass | Выполнено в аудите | Следующий результат |
|---|---|---|
| 1 — Discovery | Инвентаризация алгоритмов и hotspots | Выполнено |
| 2 — Contracts | Сопоставлены API, реализация и тесты для top-10 | Выполнено |
| 3 — Static analysis | Проверены boundary, order, state, time, numeric, recovery и complexity | Выполнено |
| 4 — Existing tests | Зафиксированы пробелы CI и тестов | Выполнено |
| 5 — Counterexamples | Получены минимальные статические контрпримеры | Выполнено для CONFIRMED |
| 6–8 — Generative, fault, mutation | Описаны таргетированные кампании | Запланировано |
| 9 — Hardening | Минимальные изменения перечислены ниже | Запланировано |
| 10 — Regression | Критерии завершения заданы для каждого пункта | Запланировано |

## 1. Executive summary

Кодовая база имеет сильную обычную тестовую базу и детерминированные tie-break правила для корректных конечных значений. Наиболее опасные разрывы находятся не в happy path, а на границе между логическим идентификатором и физическим ключом, между числовым API и JSON-персистентностью, а также между локальной синхронизацией и несколькими процессами.

Подтверждено четыре P0-направления:

1. Неинъективное преобразование идентификаторов в adgo создаёт коллизии, потерю inbox-событий и выход из каталога executions для идентификаторов . и ...
2. Компиляция плана и проверка plan delta игнорируют ошибку JSON-сериализации; NaN/Inf могут дать digest пустого байтового массива и обойти числовые ограничения.
3. Бюджет принимает отрицательные, нечисловые и переполняющиеся приращения; MemoryStore способен опубликовать новое состояние, а затем вернуть ошибку клонирования.
4. Pebble формирует диапазоны из сырых execution ID, поэтому execution a может прочитать history/tasks execution a/b; escape также неинъективен для / и %2f.

P1-риски: удаление живой lock-файла после фиксированных 30 секунд без fencing, NaN-зависимый выбор provider/task, скрытое wall-clock время, конфликт двух schedule runner и расхождение float-контракта между memory и Pebble.

Системный паттерн: внешне допустимая строка или float используется как часть identity, ordering, budget либо durable state без единой проверки домена. Небольшое изменение входа — один разделитель, NaN, переход через 30 секунд или перестановка двух кандидатов — меняет не только результат функции, но и persistent state.

## 2. PHASE 1 — ALGORITHM FRAGILITY MAP

### 2.1. Инвентаризация алгоритмов

| Алгоритм | Файл/символ | Назначение | Вход | Выход | Состояние | Зависимости | Критичность |
|---|---|---|---|---|---|---|---|
| Core dispatch/state transition | axiom.go, internal/runtime/engine.go | Применение событий и правил | execution ID, event, context | новое состояние, history, tasks | durable execution | Store, compiler, clock | Critical |
| Core expression evaluator | internal/runtime/eval.go | Вычисление выражений и сравнений | AST, runtime values | typed value/error | нет | lang/compiler types | High |
| Core retry/backoff | internal/runtime/retry_store.go | Классификация и повтор activity | task, error, policy | pending/failed task, due time | task/history/lease | Store, wall clock | Critical |
| Core concurrency lanes | internal/runtime/concurrency_store.go | first/latest admission | tasks, policy | enqueue/replace/reject | tasks and leases | Store, wall clock | High |
| Core memory store | internal/store/memory | CAS, history и task state | IDs, versions, records | persisted snapshot | in-memory maps | mutex, clone | Critical |
| Core Pebble store | internal/store/pebble | Durable key/value persistence | IDs, versions, records | encoded keys/values | Pebble keyspace | Pebble, JSON/Gob codec | Critical |
| AXM/table parsers | axm, table, internal/lang | Parsing и normalization | text/bytes | model/AST/errors | нет | lexer/parser | High |
| TRIZ normalizer/search | internal/triz | Нормализация и подбор принципов | text/model | normalized/ranked result | нет | Unicode/text rules | Medium |
| adgo plan compiler | adgo/compiler.go | Валидация DAG/циклов, digest, reachability | Definition | Plan/digest/errors | immutable Plan | JSON, SHA-256, graph walks | Critical |
| adgo engine/runtime state machine | adgo/engine.go, adgo/runtime.go | Scheduling, execution, replay, compensation | plan, events, results | execution transitions | Store, tasks, budget | clock, registry, stores | Critical |
| adgo utility scheduler | adgo/scheduler.go | Ranking и hard admission | Plan, Execution, candidates | ordered selected candidates | execution snapshot | wall clock, weights | Critical |
| adgo provider router | adgo/registry.go, adgo/router.go | Policy filter, scoring, fallback | provider set, health, policy | selected provider list | provider health | store, wall clock, floats | High |
| adgo file stores and locks | adgo/store.go, schedule.go, cache.go, admission.go, router_store.go | CAS-like persistence и lease | IDs, versions, TTL | files/locks/results | filesystem | mtime, rename, fsync | Critical |
| adgo child/fanout identity | adgo/child_workflow.go, adgo/subflow.go | Idempotent child execution | parent/node/item IDs | child execution ID | shared Store | safeName | Critical |
| adgo schedule runner | adgo/schedule.go | Periodic deterministic starts | schedule, now | executions and cursor | ScheduleStore, Engine | wall clock, CAS | Critical |
| adgo activity cache/admission | adgo/cache.go, adgo/admission.go | Dedup, leases, rate/admission | request, key, TTL | cached result/permit | memory/files | clock, hashes, locks | High |
| adgo repair/delta validation | adgo/repair.go | Проверка и digest изменения плана | base plan, proposal, policy | validated delta | нет | JSON, budget policy | Critical |
| Replay/diagnostics | adgo/replay.go, adgo/diagnostics.go | Проверка истории и инвариантов | execution/history | diagnostics | snapshot | floating comparisons | High |

### 2.2. Приоритизация RiskScore

Для ранжирования использована шкала 1–4 для Criticality, Input variability, State complexity, Hidden assumptions, Failure impact и Lack of tests. Произведение — относительный приоритет, а не вероятность отказа.

| Rank | Алгоритм | C | I | S | H | F | T | RiskScore | Причина выбора |
|---:|---|---:|---:|---:|---:|---:|---:|---:|---|
| 1 | Identity/path/key derivation в adgo | 4 | 4 | 4 | 4 | 4 | 4 | 4096 | Один символ меняет namespace или объединяет разные операции |
| 2 | Budget accounting + MemoryStore commit | 4 | 4 | 4 | 4 | 4 | 4 | 4096 | Ошибка результата способна обойти лимит и опубликовать невалидное состояние |
| 3 | Pebble keyspace encoding | 4 | 4 | 4 | 4 | 4 | 4 | 4096 | Prefix overlap раскрывает данные соседнего execution |
| 4 | Plan/delta validation and digest | 4 | 4 | 3 | 4 | 4 | 4 | 3072 | Разные невалидные планы получают один durable identity |
| 5 | File locking/CAS | 4 | 3 | 4 | 4 | 4 | 4 | 3072 | Переход через 30 секунд допускает двух writers |
| 6 | Schedule runner | 4 | 4 | 4 | 4 | 3 | 4 | 3072 | Нормальная конкуренция останавливает runner; time arithmetic не ограничена |
| 7 | Core non-finite float persistence | 4 | 4 | 3 | 4 | 4 | 4 | 3072 | Одинаковая программа зависит от выбранного backend |
| 8 | Hidden clock and retry semantics | 4 | 3 | 4 | 4 | 3 | 3 | 1728 | Fake clock не управляет всеми due/lease решениями |
| 9 | Router/scheduler scoring | 3 | 4 | 3 | 4 | 3 | 3 | 1296 | NaN превращает порядок регистрации в скрытый tie-break |
| 10 | adgo graph analysis | 3 | 4 | 2 | 3 | 3 | 4 | 864 | Длинная цепь провоцирует повторные полные сканирования |

### 2.3. Предполагаемые контракты top-10 и первые гипотезы

| Алгоритм | Предполагаемый контракт | Первая гипотеза хрупкости | Основание |
|---|---|---|---|
| Identity/key derivation | Разные логические идентификаторы не разделяют durable key; путь остаётся внутри namespace | Sanitization неинъективен и допускает . и .. | safeName заменяет все неподдерживаемые rune на _ и сохраняет точку |
| Budget + commit | Usage монотонно, конечно и неотрицательно; ошибка commit не меняет state | NaN/negative/overflow обходят limit; memory публикует до последнего clone | addBudget без проверок; assignment предшествует clone |
| Pebble keyspace | List/Get одного execution никогда не возвращает записи другого | Сырые ID создают вложенные prefix ranges | historyPrefix и taskPrefix конкатенируют ID и / |
| Plan/delta digest | У каждого принятого плана канонический digest; serialization error отклоняет план | NaN/Inf проходят validation, marshal error игнорируется | raw, _ := json.Marshal в двух критичных местах |
| File locking/CAS | Не более одного writer успешно фиксирует expected version | Stale timeout удаляет lock активного writer | mtime старше 30 секунд ведёт к os.Remove без owner/fencing |
| Schedule runner | Повтор/конкуренция безопасны; cursor продвигается ровно один раз | Победивший runner превращает второго в постоянный ErrConflict | mutate сравнивает старый fireAt на каждой повторной попытке |
| Core float persistence | Тип Float одинаково поддерживается всеми store/codec | Memory принимает NaN, JSON Pebble отклоняет | type check принимает number, JSON не кодирует NaN/Inf |
| Hidden clock/retry | Один clock определяет все due/deadline/lease решения | Часть helper-store читает time.Now напрямую | retry_store и concurrency_store обходят engine clock |
| Router/scheduler | Валидные значения имеют total order; tie-break не зависит от регистрации | NaN делает comparator ложным в обе стороны | math comparisons с NaN и stable sort |
| Graph analysis | Валидный реалистичный DAG компилируется в предсказуемом времени | Цепь создаёт повторные O(V) scans внутри обходов из каждой вершины | reachability и computeDescendants повторно сканируют nodes |

### 2.4. Fragility Index

Обозначения: B boundary, H hidden assumptions, O order, S state, T time, N numeric, F failure amplification, I missing invariants, P missing properties, R poor recovery, C complexity. Итог рассчитан по формуле из методики; индекс используется только для приоритизации выбранных hotspots.

| Алгоритм | B | H | O | S | T | N | F | I | P | R | C | Index | Статус |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Identity/key derivation | 4 | 4 | 2 | 4 | 0 | 0 | 4 | 4 | 4 | 4 | 1 | 49 | Critical |
| Plan/delta digest | 4 | 4 | 1 | 3 | 0 | 4 | 4 | 4 | 4 | 4 | 1 | 49 | Critical |
| Budget + MemoryStore atomicity | 4 | 4 | 0 | 4 | 1 | 4 | 4 | 4 | 4 | 4 | 1 | 50 | Critical |
| Pebble keyspace | 4 | 4 | 0 | 4 | 0 | 0 | 4 | 4 | 4 | 3 | 2 | 45 | Critical |
| File locking/CAS | 3 | 4 | 4 | 4 | 4 | 0 | 4 | 3 | 4 | 4 | 1 | 54 | Critical |
| Router/scheduler scoring | 4 | 4 | 3 | 3 | 2 | 4 | 3 | 3 | 4 | 2 | 1 | 50 | Critical |
| Schedule runner | 4 | 3 | 3 | 4 | 4 | 2 | 3 | 3 | 4 | 3 | 1 | 51 | Critical |
| Hidden clock/retry | 3 | 3 | 2 | 3 | 4 | 0 | 3 | 3 | 4 | 3 | 1 | 43 | Critical |
| Graph analysis | 2 | 3 | 0 | 2 | 0 | 0 | 3 | 2 | 3 | 2 | 4 | 31 | Critical |
| Core Float across stores | 4 | 4 | 0 | 3 | 0 | 4 | 3 | 4 | 4 | 3 | 1 | 44 | Critical |

### 2.5. Fragility heatmap

| Алгоритм | Boundary | Order | State | Time | Numeric | Failure |
|---|---|---|---|---|---|---|
| Identity/key derivation | 🔴 | 🟠 | 🔴 | 🟢 | 🟢 | 🔴 |
| Plan/delta digest | 🔴 | 🟡 | 🟠 | 🟢 | 🔴 | 🔴 |
| Budget + MemoryStore | 🔴 | 🟢 | 🔴 | 🟡 | 🔴 | 🔴 |
| Pebble keyspace | 🔴 | 🟢 | 🔴 | 🟢 | 🟢 | 🔴 |
| File locking/CAS | 🟠 | 🔴 | 🔴 | 🔴 | 🟢 | 🔴 |
| Router/scheduler | 🔴 | 🔴 | 🟠 | 🟠 | 🔴 | 🟠 |
| Schedule runner | 🔴 | 🔴 | 🔴 | 🔴 | 🟠 | 🟠 |
| Hidden clock/retry | 🟠 | 🟠 | 🟠 | 🔴 | 🟢 | 🟠 |
| Graph analysis | 🟡 | 🟢 | 🟠 | 🟢 | 🟢 | 🟠 |
| Core Float across stores | 🔴 | 🟢 | 🟠 | 🟢 | 🔴 | 🟠 |

## 3. Подтверждённые findings

### FRAG-001 — Неинъективные и небезопасные durable identifiers

- Severity: CRITICAL
- Confidence: HIGH
- Class: FRAG-CONTRACT, FRAG-STATE, FRAG-RECOVERY
- Location: adgo/store.go safeName/executionDir/PutInbox; adgo/child_workflow.go ChildExecutionID; adgo/subflow.go; adgo/schedule.go scheduledExecutionID; adgo/router_store.go path; adgo/runtime.go eventID
- Algorithm: преобразование логического identity в path/key/dedup ID
- Contract: разные логические IDs не должны указывать на один durable object; ID не должен менять корневой namespace.
- HYPOTHESIS: safeName и delimiter-concatenation создают коллизии.
- EVIDENCE: safeName оставляет точки и заменяет каждый иной rune на _. executionDir передаёт результат в filepath.Join. eventID хеширует Type + "|" + TargetNode + "|" + Payload без length framing.
- COUNTEREXAMPLE 1: safeName("a/b") = safeName("a?b") = "a_b".
- COUNTEREXAMPLE 2: safeName("..") = ".."; executionDir("..") разрешается в root store, а не root/executions/<id>.
- COUNTEREXAMPLE 3: Event{Type:"a|b", TargetNode:"c", Payload:p} и Event{Type:"a", TargetNode:"b|c", Payload:p} имеют один и тот же preimage и event ID.
- Expected: distinct identity или явная validation error до обращения к store.
- Actual: child/schedule/provider/inbox objects могут совпасть; второй inbox event считается уже существующим и молча теряется.
- Root cause: display sanitization используется как canonical identity; составные ключи не имеют framing.
- Blast radius: execution state, fanout children, schedules, provider health, inbox dedup и external idempotency.
- Existing tests: обычные ASCII IDs; тестов injectivity/containment для Unicode, separators, . и .. не найдено.
- Missing test: property encode(tuple) is injective within generated corpus; resolved path always stays under its namespace.
- Recommended fix: LEVEL 3 — единый versioned length-prefixed key codec или hash от canonical binary tuple; display name хранить отдельно.
- Verification: пункты HP-00, HP-01 и HP-05.
- RESULT: CONFIRMED статическим контрпримером.

### FRAG-002 — Digest fail-open при несерилизуемом плане

- Severity: CRITICAL
- Confidence: HIGH
- Class: FRAG-NUMERIC, FRAG-CONTRACT, FRAG-INVARIANT
- Location: adgo/compiler.go Compile; adgo/repair.go ValidatePlanDelta
- Algorithm: canonical plan/proposal digest
- Contract: принятому плану соответствует digest его канонического содержимого; ошибка сериализации отклоняет план.
- HYPOTHESIS: non-finite float проходит validation и превращается в digest пустого массива.
- EVIDENCE: оба алгоритма используют raw, _ := json.Marshal(...). encoding/json отклоняет NaN и Inf. Валидация не требует finiteness для EstimatedCost, ExpectedQualityGain, CriticalPathWeight, Epsilon, HardFloors и связанных весов.
- COUNTEREXAMPLE: Definition A с EstimatedCost=NaN и Definition B с ExpectedQualityGain=+Inf проходят существующие доменные проверки, json.Marshal возвращает nil,error, а оба digest равны SHA-256 пустого массива.
- Expected: deterministic validation error с полем и node ID.
- Actual: digest выглядит валидным и не отражает план; проверка RemainingBudget в plan delta также обходится, потому что сравнение NaN > budget ложно.
- Root cause: проигнорированная ошибка serialization плюс неполный числовой контракт.
- Blast radius: plan pinning, replay, repair/delta policy, dedup и auditability.
- Existing tests: стабильность digest на обычных значениях; NaN/Inf и marshal failure не покрыты.
- Missing test: table NaN, +Inf, -Inf и negative values по каждому числовому полю.
- Recommended fix: LEVEL 2 — fail-closed validation и возврат ошибки digest; не менять публичный контракт шире необходимого.
- Verification: пункты HP-00 и HP-02.
- RESULT: CONFIRMED доказуемым нарушением digest-инварианта.

### FRAG-003 — Бюджет можно уменьшить или сделать несравнимым

- Severity: CRITICAL
- Confidence: HIGH
- Class: FRAG-NUMERIC, FRAG-INVARIANT, FRAG-RECOVERY
- Location: adgo/runtime.go addBudget/checkBudget/applyActivityResultImpl; adgo/engine.go применение ActivityResult; adgo/replay.go
- Algorithm: cumulative budget accounting
- Contract: каждое usage-поле конечно и неотрицательно; cumulative usage не убывает и не переполняется; limit нельзя обойти результатом activity.
- HYPOTHESIS: ActivityResult принимает отрицательный/NaN usage без проверки.
- EVIDENCE: addBudget выполняет прямое сложение float, int и duration. checkBudget использует обычные comparisons. NaN делает сравнения ложными; отрицательное приращение уменьшает usage; integer overflow может сменить знак.
- COUNTEREXAMPLE: MaxCost=10, current Cost=9, activity returns BudgetUsage{Cost:NaN}; после addBudget Cost=NaN, а Cost >= MaxCost ложно на всех следующих проверках.
- Expected: activity result отклонён до изменения execution; состояние остаётся прежним.
- Actual: невалидное значение попадает в execution и отключает budget guard.
- Root cause: граница доверия ActivityHandler → Runtime не проверяет домен результата.
- Blast radius: cost/token/call limits, scheduling reservations, replay и JSON persistence.
- Existing tests: нормальное накопление и достижение лимита; generative monotonicity и overflow cases не найдены.
- Missing test: property usage_after >= usage_before and finite; failure leaves execution byte-for-byte equivalent кроме допустимой diagnostic history.
- Recommended fix: LEVEL 1/2 — централизованная validation ActivityResult и checked addition.
- Verification: пункты HP-00 и HP-03.
- RESULT: CONFIRMED минимальным числовым контрпримером.

### FRAG-004 — MemoryStore публикует state перед возможной ошибкой

- Severity: CRITICAL
- Confidence: HIGH
- Class: FRAG-STATE, FRAG-RECOVERY, FRAG-CONTRACT
- Location: adgo/store.go MemoryStore.Commit; adgo/schedule.go MemoryScheduleStore.Commit; adgo/cache.go MemoryActivityCache.Put
- Algorithm: atomic commit and copy isolation
- Contract: Commit с ошибкой не меняет durable state; caller-owned maps/results не изменяют store после Put/Commit.
- HYPOTHESIS: финальное клонирование может упасть после assignment.
- EVIDENCE: MemoryStore.Commit присваивает s.exec[id] = next, затем вызывает cloneExecution(next). JSON clone падает на NaN. MemoryScheduleStore повторяет тот же порядок. MemoryActivityCache хранит result без defensive clone.
- COUNTEREXAMPLE: mutate записывает NaN в execution data и возвращает nil. Commit сначала заменяет s.exec[id], затем cloneExecution возвращает error. Caller видит failure, но Load уже читает повреждённую версию либо тоже падает.
- Expected: error и прежние version/state.
- Actual: error после публикации state.
- Root cause: prepare/validate/clone не завершены до commit point.
- Blast radius: тестовый и production memory backend, schedule state, cache isolation; поведение расходится с file backend.
- Existing tests: CAS success/conflict; atomic-on-serialization-error и mutation-after-Put не найдены.
- Missing test: единый store contract, запускаемый против Memory/File и других реализаций.
- Recommended fix: LEVEL 2 — подготовить независимый result clone до assignment; публиковать ровно одной последней операцией.
- Verification: пункты HP-00 и HP-04.
- RESULT: CONFIRMED по порядку операций.

### FRAG-005 — Pebble prefix ranges пересекают executions

- Severity: CRITICAL
- Confidence: HIGH
- Class: FRAG-STATE, FRAG-CONTRACT, FRAG-SCALABILITY
- Location: internal/store/pebble/store.go execKey/historyPrefix/taskPrefix/taskStatusPrefix/taskDedupKey/escape
- Algorithm: physical key encoding and range scans
- Contract: keyspace одного execution изолирован для любого ID, разрешённого runtime.
- HYPOTHESIS: execution ID, являющийся prefix другого ID, расширяет range scan.
- EVIDENCE: historyPrefix(id) формирует hist/ + id + /. Контракт core Engine проверяет только непустой execution ID. escape заменяет / на %2f, но не экранирует уже существующий %2f.
- COUNTEREXAMPLE 1: historyPrefix("a") = "hist/a/"; записи execution "a/b" начинаются с "hist/a/b/" и попадают в range "a".
- COUNTEREXAMPLE 2: escape("/") = escape("%2f") = "%2f".
- Expected: ListHistory("a") и ListTasks("a") содержат только execution a.
- Actual: возможно чтение/обработка records execution a/b; task/dedup IDs могут совпасть.
- Root cause: raw delimiter-separated key components без injective encoding.
- Blast radius: history, task listing/status, dedup, task lookup и recovery.
- Existing tests: обычные IDs; differential prefix-isolation corpus не найден.
- Missing test: две execution с prefix-related IDs и distinct sentinel records.
- Recommended fix: LEVEL 3 — versioned component framing в Pebble key schema плюс совместимая миграция/dual-read.
- Verification: пункты HP-00 и HP-05.
- RESULT: CONFIRMED свойством lexicographic prefix range.

### FRAG-006 — Stale lock удаляется без fencing

- Severity: CRITICAL
- Confidence: HIGH для механизма, MEDIUM для частоты
- Class: FRAG-CONCURRENCY, FRAG-TIME, FRAG-RECOVERY
- Location: adgo/store.go withExecutionLock; adgo/schedule.go withLock; adgo/cache.go withLock; adgo/admission.go withLock; adgo/router_store.go withLock
- Algorithm: multi-process exclusion around file updates
- Contract: один logical critical section владеет mutable object до завершения; stale owner не может перезаписать нового.
- HYPOTHESIS: операция дольше 30 секунд позволяет второму процессу удалить живой lock.
- EVIDENCE: при time.Since(lock.ModTime()) > lockStaleAfter выполняется os.Remove; owner token, heartbeat и fencing version отсутствуют. writeCommit использует temp+rename для одного и того же version filename.
- COUNTEREXAMPLE: writer A держит lock 30.001 s из-за pause/fsync; writer B удаляет lock и входит; A и B оба пишут expected version N+1, последний rename выигрывает.
- Expected: только один writer успешно фиксирует expected version; другой получает conflict/stale owner.
- Actual: механизм допускает concurrent critical sections после одной временной границы.
- Root cause: lease expiry трактуется как доказательство смерти owner без fencing.
- Blast radius: execution commits, schedules, provider health, cache и admission state.
- Existing tests: thread-level locking внутри одного процесса; multi-process slow-writer/kill tests не найдены.
- Missing test: helper subprocess с pause до и после stale threshold, crash/restart и одинаковым expected version.
- Recommended fix: LEVEL 2/3 — immutable CAS через O_EXCL там, где есть version; для mutable records owner token/fencing или OS advisory lock с чётким recovery contract.
- Verification: пункты HP-06 и HP-13.
- RESULT: CONFIRMED как concurrency hole; динамическая частота TO VERIFY.

### FRAG-007 — NaN превращает порядок регистрации в алгоритм выбора

- Severity: HIGH
- Confidence: HIGH
- Class: FRAG-HEURISTIC, FRAG-NUMERIC, FRAG-ORDER
- Location: adgo/router.go normalizeRouterConfig/Resolve; adgo/registry.go Resolve; adgo/scheduler.go Select
- Algorithm: provider and task scoring
- Contract: невалидные метрики отклоняются; при равном конечном score tie-break детерминирован по ID/name.
- HYPOTHESIS: NaN не проходит существующие guards и ломает comparator.
- EVIDENCE: NaN делает условия <=, >, < ложными. Для score=NaN и обычного score comparator возвращает false в обе стороны; sort.SliceStable сохраняет исходный порядок.
- COUNTEREXAMPLE: provider A имеет Quality=NaN, provider B корректен. Регистрация [A,B] выбирает A, [B,A] выбирает B при одинаковой policy.
- Expected: explicit validation error или исключение невалидного provider до ranking.
- Actual: winner зависит от порядка регистрации, хотя finite tie-break по name реализован.
- Root cause: отсутствие finite/domain validation до weighted score.
- Blast radius: выбор внешнего provider, стоимость/качество, scheduling и budget reservation.
- Existing tests: finite scores и обычные ties; NaN/Inf, near-ties и weight sensitivity не найдены.
- Missing test: permutation invariance для любого набора валидных кандидатов; reject non-finite inputs.
- Recommended fix: LEVEL 1/2 — validation и total-order policy; веса вынести в документированный config только там, где уже есть настройка.
- Verification: пункты HP-07 и HP-13.
- RESULT: CONFIRMED статическим comparator-контрпримером.

### FRAG-008 — Clock contract расщеплён и schedule conflict не идемпотентен

- Severity: HIGH
- Confidence: HIGH
- Class: FRAG-TIME, FRAG-STATE, FRAG-CONCURRENCY, FRAG-RETRY
- Location: internal/runtime/retry_store.go; internal/runtime/concurrency_store.go; adgo/runtime.go; adgo/scheduler.go; adgo/schedule.go Tick/commitSchedule
- Algorithm: due time, lease expiry, deadline и periodic cursor
- Contract: тестовый/инъецированный clock управляет всеми решениями; повтор одного fireAt безопасен; нормальная CAS-конкуренция не останавливает runner.
- HYPOTHESIS: helpers читают wall clock напрямую, а второй runner зацикливает старый mutate и возвращает ErrConflict.
- EVIDENCE: retry_store и concurrency_store вызывают time.Now напрямую. Schedule Tick запоминает fireAt; при conflict commitSchedule повторяет closure, которая на уже продвинутом current.NextAt снова возвращает ErrConflict восемь раз.
- COUNTEREXAMPLE 1: fake engine clock достиг NextAttemptAt, wall clock helper ещё нет — task одновременно due и not due в разных слоях.
- COUNTEREXAMPLE 2: два runner читают один NextAt; первый commits, второй StartOrLoad безопасно возобновляет execution, но затем получает постоянный ErrConflict и Run завершается.
- Expected: единый now; already-advanced cursor трактуется как idempotent success/no-op.
- Actual: решение зависит от скрытого времени и interleaving.
- Root cause: wall clock не является полной dependency; retry closure не различает contention и нарушенный контракт.
- Blast radius: retries, first/latest concurrency, deadlines, timers, schedules, cache/admission leases.
- Existing tests: отдельные happy-path time cases; общая t−ε/t/t+ε matrix и two-runner state machine не найдены.
- Missing test: fake clock и model-based command sequences; process clock-skew cases.
- Recommended fix: LEVEL 2 — компактный internal clock seam и идемпотентная cursor transition.
- Verification: пункты HP-08 и HP-09.
- RESULT: CONFIRMED по control flow; допустимый clock-skew в production TO DEFINE.

### FRAG-009 — Graph compilation имеет adversarial complexity cliff

- Severity: MEDIUM
- Confidence: HIGH для сложности, TO VERIFY для практической границы
- Class: FRAG-COMPLEXITY, FRAG-SCALABILITY
- Location: adgo/compiler.go validation walk, reachability, computeDescendants
- Algorithm: reachability, parallel-writer validation и descendants
- Contract: валидный DAG реалистичного размера компилируется в предсказуемое время/память.
- HYPOTHESIS: длинная dependency chain приближает compilation к cubic work.
- EVIDENCE: walk сканирует все nodes для каждой посещённой вершины; reachability строит adjacency вложенным сканированием; computeDescendants запускает traversal от каждой вершины и для каждой dequeued вершины снова сканирует весь plan.
- COUNTEREXAMPLE: chain из N nodes, где каждая зависит от предыдущей. Результат descendants уже требует O(N²) space, но текущие repeated scans добавляют O(N³)-подобную работу.
- Expected: adjacency строится один раз; work соответствует размеру графа и неизбежному размеру результата.
- Actual: одни и те же dependency edges обнаруживаются повторными полными scans.
- Root cause: отсутствие общего adjacency/reverse-adjacency внутри compiler pass.
- Blast radius: startup/deploy latency и memory для больших generated plans.
- Existing tests: функциональные графы; adgo benchmarks по chain/star/dense не найдены.
- Missing test: benchmark series N, 2N, 4N и p95/alloc tracking.
- Recommended fix: LEVEL 2/3 после benchmark — переиспользовать adjacency; bitset добавлять только если измерение оправдает.
- Verification: пункт HP-10.
- RESULT: сложность CONFIRMED; практический cliff TO VERIFY benchmark.

### FRAG-010 — Float-контракт зависит от backend

- Severity: HIGH
- Confidence: HIGH
- Class: FRAG-NUMERIC, FRAG-DEPENDENCY, FRAG-CONTRACT
- Location: internal/runtime/types_check.go valueMatchesType; internal/runtime/clone.go; internal/store/pebble/codec.go
- Algorithm: runtime type validation and persistence codec
- Contract: значение, принятое как Float, либо одинаково сохраняется всеми поддержанными stores, либо одинаково отклоняется до commit.
- HYPOTHESIS: NaN/Inf принимаются memory path и отклоняются JSON Pebble.
- EVIDENCE: Float принимает любой number. Reflect-based memory clone сохраняет non-finite values. encoding/json возвращает ошибку для NaN/Inf.
- COUNTEREXAMPLE: правило вычисляет +Inf или принимает NaN в Float field; memory execution продолжается, JSON Pebble commit падает.
- Expected: единый documented outcome.
- Actual: семантика зависит от backend/codec.
- Root cause: типовой контракт шире durable codec contract.
- Blast radius: переносимость, replay, production-vs-test parity.
- Existing tests: IEEE comparison behavior покрыто; cross-store persistence matrix для non-finite values не найдена.
- Missing test: differential Memory, Pebble JSON и Pebble Gob.
- Recommended fix: LEVEL 4 только после решения контракта; предпочтительный минимальный вариант — finite-only для persisted Float. Если non-finite нужен предметно, нужен явный tagged encoding.
- Verification: пункт HP-11.
- RESULT: CONFIRMED сравнением type guard и codec.

### FRAG-011 — Retryability кодируется строковым префиксом

- Severity: MEDIUM
- Confidence: HIGH
- Class: FRAG-RETRY, FRAG-CONTRACT
- Location: internal/runtime/retry_store.go isRetryableActivityFailure
- Algorithm: failure classification
- Contract: wrapping, форматирование и локализация текста ошибки не меняют retry policy.
- HYPOTHESIS: изменение текста AX505 превращает retryable failure в terminal.
- EVIDENCE: классификация равна strings.HasPrefix(message, "AX505:") либо message == "AX505".
- COUNTEREXAMPLE: fmt.Errorf("activity failed: %w", AX505 error) сохраняет типовую причину, но строка больше не начинается с AX505.
- Expected: retryability следует typed/status contract.
- Actual: одно текстовое изменение меняет state transition.
- Root cause: machine contract извлекается из presentation string.
- Blast radius: transient activity failures и durable task state.
- Existing tests: конкретные строки; mutation/wrapping matrix не найдена.
- Missing test: errors.Is/errors.As, wrapping и неизменность classification при форматировании.
- Recommended fix: LEVEL 2 — протащить typed retryability/status; текст оставить только для diagnostics.
- Verification: пункт HP-12.
- RESULT: CONFIRMED минимальным строковым контрпримером.

## 4. Hidden assumptions register

| ID | Алгоритм/место | Неявное предположение | При нарушении | Проверка | Тест | Severity |
|---|---|---|---|---|---|---|
| A-001 | safeName | Разные IDs дают разные filenames | aliasing/lost update | Нет | Нет | Critical |
| A-002 | executionDir | ID не равен . или .. | выход из execution namespace | Нет | Нет | Critical |
| A-003 | eventID/task-derived IDs | Разделитель не встречается в компонентах | dedup collision | Нет | Нет | Critical |
| A-004 | Compile digest | canonical model всегда JSON-serializable | digest не отражает plan | Нет, error ignored | Нет | Critical |
| A-005 | ValidatePlanDelta | cost finite и nonnegative | budget bypass | Нет | Нет | Critical |
| A-006 | addBudget | increments finite/nonnegative и не overflow | usage уменьшается/NaN | Нет | Нет | Critical |
| A-007 | MemoryStore.Commit | финальный clone не может упасть | error after state change | Нет | Нет | Critical |
| A-008 | Pebble prefixes | execution ID не содержит separator и не prefix другого | cross-execution range | Нет | Нет | Critical |
| A-009 | escape | исходное %2f не встречается | key collision | Нет | Нет | High |
| A-010 | file locks | critical section всегда короче 30 s | concurrent writers | Частично TTL | Нет | Critical |
| A-011 | router/scheduler | все float finite | order-dependent choice | Нет | Нет | High |
| A-012 | scheduler | wall clock монотонен и согласован | deadline pressure jump | Нет | Частично | High |
| A-013 | retry helpers | time.Now совпадает с engine clock | inconsistent due state | Нет | Нет | High |
| A-014 | schedule runner | conflict означает, что тот же mutate надо повторить | permanent conflict/runner stop | Частично CAS retry | Нет | High |
| A-015 | schedule arithmetic | missed × Every помещается в Duration | wrapped NextAt | Нет | Нет | High |
| A-016 | graph compiler | plans невелики | latency/memory cliff | Нет | Нет | Medium |
| A-017 | Float persistence | все codecs поддерживают runtime number | backend divergence | Нет | Нет | High |
| A-018 | retry classification | error text начинается с стабильного code | wrong terminal transition | Частично | Частично | Medium |

## 5. Missing invariants

| ID | Инвариант | Где закрепить |
|---|---|---|
| I-001 | encodeIdentity(a) = encodeIdentity(b) только если a = b в поддержанном домене | identity codec property tests |
| I-002 | resolved durable path всегда является потомком ожидаемого namespace | file-store tests перед каждым I/O |
| I-003 | canonical digest вычисляется только из успешно сериализованного полного значения | Compile и ValidatePlanDelta |
| I-004 | все persisted float конечны либо имеют единый tagged representation | runtime boundary и store conformance |
| I-005 | BudgetUsage после успешного activity не меньше предыдущего и не превышает числовой диапазон | checked addBudget |
| I-006 | Commit вернул error ⇒ version и state не изменились | каждый Store/ScheduleStore |
| I-007 | изменение caller-owned result/map после Put не изменяет stored value | cache/store conformance |
| I-008 | ListHistory/ListTasks(executionID) не возвращает чужой executionID | Pebble differential tests |
| I-009 | для одной expected version успешно commits не более одного writer | file multi-process CAS |
| I-010 | comparator задаёт total order для всех принятых кандидатов | router/scheduler validation |
| I-011 | один logical fireAt создаёт не более одного execution и продвигает cursor ровно один раз | schedule model |
| I-012 | fake clock полностью определяет retry/lease/deadline tests | runtime temporal tests |
| I-013 | retryability не зависит от Error() formatting | typed failure tests |
| I-014 | graph compile growth не превышает согласованную complexity envelope | benchmark gate |

## 6. Counterexample corpus

Первый hardening-коммит должен сохранить эти случаи как regression tests до изменения production code.

| ID | Минимальный вход | Expected | Текущее поведение | Целевой test |
|---|---|---|---|---|
| CE-001 | execution ID ".." | reject или isolated key | path resolves to store root | TestFileStoreIdentityContainment |
| CE-002 | IDs "a/b" и "a?b" | разные objects | один safeName | TestIdentityInjectiveExamples |
| CE-003 | events ("a|b","c",p) и ("a","b|c",p) | разные event IDs | одинаковый preimage | TestEventIDComponentFraming |
| CE-004 | two child items "a/b", "a?b" | разные child executions | один child ID | TestChildExecutionIDDistinctItems |
| CE-005 | Definition с EstimatedCost=NaN | validation error | digest SHA-256(empty) | TestCompileRejectsNonFinite |
| CE-006 | PlanProposal с cost=NaN и RemainingBudget=1 | validation error | budget comparison bypass | TestPlanDeltaRejectsNonFinite |
| CE-007 | current Cost=9, MaxCost=10, increment NaN | reject and unchanged state | Cost becomes NaN | TestBudgetUsageMonotonic |
| CE-008 | Memory Commit mutate inserts NaN | error and old version | new state assigned before error | TestStoreAtomicOnCloneError |
| CE-009 | executions "a" и "a/b" с distinct history/task sentinels | isolated lists | prefix range overlaps | TestPebbleExecutionNamespaceIsolation |
| CE-010 | task IDs "/" и "%2f" | distinct task keys | same escape | TestPebbleKeyEncodingInjective |
| CE-011 | lock owner pause 30.001 s, second writer same version | one success | both enter critical section | TestFileStoreFencingSlowWriter |
| CE-012 | providers [NaN,A] и [A,NaN] | validation error, same decision | winner follows registration | TestRouterRejectsNonFinitePermutation |
| CE-013 | two runners on same fireAt | both remain healthy, one cursor advance | loser returns permanent conflict | TestScheduleRunnerConcurrentTick |
| CE-014 | fake clock past retry due, wall clock before due | one due result | layers disagree | TestRetryUsesEngineClock |
| CE-015 | Float=+Inf persisted in Memory and JSON Pebble | same explicit outcome | accept vs reject | TestStoreFloatContract |
| CE-016 | wrapped retryable AX505 error | remains retryable | classified terminal | TestRetryClassificationWrapping |

## 7. Missing tests by technique

| Technique | Приоритетная цель | Генератор/сценарий | Shrink/minimal result |
|---|---|---|---|
| Boundary | IDs, budget, deadline, lease, schedule | empty, ., .., separators; 0/1/−1; t−1ns/t/t+1ns | один ID или один transition |
| Property | key codec, budget, store CAS | arbitrary Unicode tuples; finite/non-finite floats; versions | два collided strings, одно invalid increment |
| Metamorphic | compiler/router/normalizer | node permutation; provider permutation; normalize twice | два nodes/providers |
| Fuzz | Definition, keys, codecs, parser | duplicate IDs, invalid floats, deep graph, Unicode | Go fuzz corpus |
| Differential | Memory/File/Pebble | одинаковая command sequence | shortest divergent sequence |
| Stateful | Engine/Schedule/Cache | create, commit, conflict, retry, expire, restart | minimal command trace |
| Fault injection | file stores | timeout, partial temp file, fsync error, killed owner | one failure point |
| Concurrency | locks, schedule, cache/admission | two processes, reordered commits, lease expiry | two actors, one object |
| Performance | graph compiler | chain, star, dense DAG at N/2N/4N | smallest N beyond envelope |
| Mutation | identity, comparisons, budget, retry | >↔>=, +↔−, &&↔||, ignored error | surviving mutant per critical symbol |

## 8. Hardening plan

Порядок обязателен: сначала failing regression, затем минимальный production change, затем общий generative/differential guard. Изменения durable key schema не объединять с unrelated refactoring.

### HP-00 — Зафиксировать corpus до исправлений

- Priority / level: P0 / LEVEL 0.
- Что делаем: добавляем CE-001…CE-016 как отдельные тесты; CONFIRMED cases должны падать на базовой реализации по ожидаемой причине. Для TO VERIFY cases тест сначала доказывает или отклоняет гипотезу.
- Где делаем: adgo/identity_test.go, adgo/compiler_numeric_test.go, adgo/budget_property_test.go, adgo/store_contract_test.go, adgo/schedule_stateful_test.go, internal/store/pebble/namespace_test.go, internal/runtime/retry_clock_test.go.
- Почему: сохраняем доказательство причины и не позволяем исправлению замаскировать её широкой переработкой.
- Как проверить: go test с точечным -run для каждого CE; затем go test ./...; в PR явно приложить expected failing output до fix и passing output после.
- Regression test: каждый CE остаётся постоянным, без skip и без зависимости от sleep.
- Done: любой следующий hardening PR ссылается минимум на один CE и меняет только соответствующий класс поведения.

### HP-01 — Ввести канонический injective identity codec

- Priority / level: P0 / LEVEL 3.
- Что делаем: отделяем display sanitization от identity. Составные IDs кодируем через versioned length framing или SHA-256 от canonical framed tuple; перед filesystem I/O проверяем containment. Пустой ID, . и .. получают явный contract. Не сокращаем hash до 64 bit для durable identity без отдельного collision contract.
- Где делаем: adgo/store.go safeName/executionDir/inbox paths; adgo/child_workflow.go ChildExecutionID; adgo/subflow.go; adgo/schedule.go path/lock/scheduledExecutionID; adgo/router_store.go; adgo/runtime.go eventID/taskID/renderIdempotency; adgo/cache.go DefaultActivityCacheKey. Маленький internal helper оправдан повторением одной доказанной ошибки в нескольких подсистемах.
- Почему: устраняются FRAG-CONTRACT/STATE/RECOVERY и CE-001…CE-004.
- Как проверить: table tests для separators, ., .., NUL, CRLF, Unicode normalization forms и delimiter-in-components; fuzz property distinct tuple → distinct encoded bytes в corpus; filepath.Rel никогда не начинается с ...
- Regression test: TestIdentityInjectiveExamples, FuzzIdentityCodec, TestAllFilePathsContained.
- Migration: добавить schema version; для существующих file layouts — read-old/write-new с обнаружением неоднозначных legacy names и явным migration report. Не выполнять silent merge.
- Done: все логические key builders используют один codec; поиск safeName не показывает identity usage; corpus и fuzz проходят.

### HP-02 — Сделать plan/delta validation fail-closed

- Priority / level: P0 / LEVEL 1–2.
- Что делаем: для каждого float поля определяем finite/nonnegative/range contract; json.Marshal errors возвращаем caller; digest вычисляем только после успешной canonical serialization. Ошибка содержит field path и node/provider ID.
- Где делаем: adgo/compiler.go validateDefinition/Compile; adgo/repair.go ValidatePlanDelta; shared local numeric validation helper только для повторяющихся field checks.
- Почему: устраняются FRAG-NUMERIC/CONTRACT/INVARIANT и CE-005/CE-006.
- Как проверить: table NaN/+Inf/−Inf/negative на EstimatedCost, ExpectedQualityGain, CriticalPathWeight, LoopBudget Epsilon/MaxCost, Gate HardFloors и proposal costs. Metamorphic property: permutation nodes/dependencies не меняет digest валидного Definition.
- Regression test: TestCompileRejectsNonFinite, TestPlanDeltaRejectsNonFinite, FuzzCompileCanonicalDigest.
- Done: ни одна ошибка marshal не игнорируется; invalid plan не имеет digest; обычные digest fixtures не меняются без объяснённой schema version.

### HP-03 — Закрепить монотонный и checked budget

- Priority / level: P0 / LEVEL 1–2.
- Что делаем: валидируем ActivityResult до commit; usage increments только finite, nonnegative и без integer/duration overflow. addBudget возвращает error и не модифицирует destination частично. Определяем точную семантику exactly-at-limit.
- Где делаем: adgo/runtime.go applyActivityResultImpl/checkBudget/addBudget; adgo/engine.go второй путь применения результата; adgo/replay.go monotonic checks; adgo/diagnostics.go.
- Почему: устраняются FRAG-NUMERIC/INVARIANT/RECOVERY и CE-007.
- Как проверить: property for any accepted increment usageAfter >= usageBefore; sum либо точен, либо возвращает typed overflow error; NaN/Inf/negative оставляют execution unchanged. Boundary matrix limit−ε, limit, limit+ε.
- Regression test: TestBudgetUsageMonotonic, FuzzBudgetAddition, TestBudgetExactlyAtLimit.
- Done: budget guard невозможно отключить NaN; overflow не wrap; runtime и replay используют один contract.

### HP-04 — Унифицировать atomicity и isolation Store implementations

- Priority / level: P0 / LEVEL 2.
- Что делаем: clone/validate/serialize полностью до commit point; result clone готовим до assignment; cache хранит defensive copy. Ошибка любого prepare step сохраняет прежнюю version/state.
- Где делаем: adgo/store.go MemoryStore; adgo/schedule.go MemoryScheduleStore; adgo/cache.go MemoryActivityCache; общий parameterized contract test для Memory/File реализаций.
- Почему: устраняются FRAG-STATE/RECOVERY и CE-008.
- Как проверить: faulting mutate/clone/serialization; caller mutates maps after Create/Put; concurrent CAS на одной expected version. Сравнить состояние до/после error.
- Regression test: TestStoreAtomicOnCloneError, TestScheduleStoreAtomicOnCloneError, TestCacheCopyIsolation.
- Done: все stores проходят один contract suite; error ⇒ no observable state change; success возвращает независимую copy.

### HP-05 — Версионировать и изолировать Pebble keyspace

- Priority / level: P0 / LEVEL 3.
- Что делаем: каждый key component кодируем инъективно и length-prefix; вводим новый schema prefix. Range строится только из полной encoded execution component. Task/dedup IDs используют тот же codec.
- Где делаем: internal/store/pebble/store.go все key builders и range helpers; migration/open logic рядом с Pebble initialization; codec tests в internal/store/pebble.
- Почему: устраняются FRAG-STATE/CONTRACT и CE-009/CE-010.
- Как проверить: differential sequence против MemoryStore и Pebble для IDs a, a/b, a%2fb, /, %2f, Unicode; invariant у каждого returned record ExecutionID равен requested ID; randomized tuple codec round-trip/injectivity.
- Regression test: TestPebbleExecutionNamespaceIsolation, TestPebbleKeyEncodingInjective, FuzzPebbleKeyComponents.
- Migration: read-only detector старой schema; backup/checkpoint; deterministic rewrite; count/hash records до/после; reopen и replay; rollback instruction. Нельзя просто сменить keys без migration.
- Done: old database либо безопасно мигрирует, либо открытие fail-closed с инструкцией; cross-execution scan невозможен.

### HP-06 — Добавить fencing в file-store concurrency

- Priority / level: P0 / LEVEL 2–3.
- Что делаем: для immutable version commits используем create-if-absent/O_EXCL как окончательный CAS. Для mutable schedule/cache/admission/provider records используем owner token и fencing generation либо OS advisory lock; stale cleanup не даёт старому owner права commit. Lease timeout конфигурируем и измеряем, но timeout сам по себе не считается fencing.
- Где делаем: пять withLock реализаций в adgo/store.go, schedule.go, cache.go, admission.go, router_store.go; atomicWrite/writeCommit paths.
- Почему: устраняется FRAG-CONCURRENCY/TIME/RECOVERY и CE-011.
- Как проверить: helper subprocess A pause > stale threshold, B attempts same version, затем A resumes; ровно один success. Повторить с kill A до temp write, после temp write и до directory fsync. Запускать с -race дополнительно, но не считать race detector доказательством multi-process safety.
- Regression test: TestFileStoreFencingSlowWriter и аналогичный table для schedule/cache/admission/provider.
- Done: stale owner не перезаписывает новый; orphan temp/lock recovery документирован; 1000 повторов не дают dual success.

### HP-07 — Валидировать scoring inputs и закрепить total order

- Priority / level: P1 / LEVEL 1–2.
- Что делаем: отклоняем non-finite quality/cost/privacy/weights и invalid duration/risk; comparator работает только на validated finite score. Документируем units и exact tie epsilon. Existing deterministic name/ID tie-break сохраняем.
- Где делаем: adgo/registry.go registration/Resolve; adgo/router.go normalizeRouterConfig/Resolve; adgo/scheduler.go constructor/Select; definition validation для node estimates.
- Почему: устраняются FRAG-HEURISTIC/NUMERIC/ORDER и CE-012.
- Как проверить: permutation property; exact tie и score difference вокруг 1e-9; weight sensitivity w ±1%, ±5%, ±10%; golden corpus показывает долю смены winner, а не только один пример.
- Regression test: TestRouterRejectsNonFinitePermutation, TestSchedulerTieBreak, FuzzProviderPermutation.
- Done: перестановка валидного provider/candidate set не меняет ordered result; invalid input всегда typed error, а не hidden ordering.

### HP-08 — Провести единый clock через temporal decisions

- Priority / level: P1 / LEVEL 2.
- Что делаем: используем маленький internal now function/clock seam во всех due, deadline, lease, cooldown, TTL и schedule decisions. Persisted timestamps остаются UTC wall time; elapsed duration внутри процесса использует monotonic component, где возможно. Документируем допустимый cross-process skew и fail-safe semantics.
- Где делаем: internal/runtime/retry_store.go и concurrency_store.go; adgo engine/runtime/scheduler/schedule/cache/admission/router и lock helpers.
- Почему: устраняется FRAG-TIME и CE-014; temporal tests становятся детерминированными.
- Как проверить: единая matrix t−1ns, t, t+1ns; jumps ±1s, ±1h, date/DST/timezone; fake clock без sleep; two-process skew model.
- Regression test: TestRetryUsesEngineClock, TestLeaseBoundary, TestDeadlineBoundary, TestClockJumpPolicy.
- Done: поиск time.Now в алгоритмических пакетах показывает только clock adapter/creation timestamps с объяснённым контрактом; temporal suite не flaky.

### HP-09 — Сделать schedule transition идемпотентным и checked

- Priority / level: P1 / LEVEL 2.
- Что делаем: при CAS conflict reload различает already-advanced cursor и реальный conflict; already-fired fireAt становится success/no-op. Расчёт missed/NextAt проверяет overflow и invalid Every/MaxCatchUp. Crash между StartOrLoad/Advance/cursor commit моделируется явно.
- Где делаем: adgo/schedule.go Register/Tick/commitSchedule/scheduledExecutionID; ScheduleStore contract tests.
- Почему: устраняются FRAG-TIME/CONCURRENCY/RETRY и CE-013.
- Как проверить: stateful model с командами register, tick, tick-concurrent, restart, disable, catch-up; два runner на одном fireAt; t−ε/t/t+ε; Every near duration bounds; 100 repeated same tick.
- Regression test: TestScheduleRunnerConcurrentTick, TestScheduleCrashAfterStart, FuzzScheduleCommands.
- Done: normal contention не завершает Run; на fireAt существует один execution ID; cursor монотонен и overflow невозможен.

### HP-10 — Убрать repeated graph scans после benchmark

- Priority / level: P2 / LEVEL 2–3.
- Что делаем: сначала добавляем benchmark chain/star/dense; затем строим adjacency и reverse adjacency один раз и переиспользуем validation/reachability/descendants. Bitsets или новый graph layer допустимы только если baseline докажет необходимость.
- Где делаем: adgo/compiler.go walk/reachability/computeDescendants/Tarjan adjacency; adgo/compiler_benchmark_test.go.
- Почему: устраняется FRAG-COMPLEXITY/SCALABILITY без архитектурного переписывания.
- Как проверить: N=100, 1k, 5k, 10k, series N/2N/4N; p50/p95, allocations и max RSS на pinned runner. Для chain, где результат descendants O(N²), рост после оптимизации должен соответствовать O(N²) envelope, а не repeated O(N³)-подобным scans.
- Regression test: BenchmarkCompileChain/Star/Dense и небольшой timeout guard на согласованном practical maximum.
- Done: зафиксирована поддерживаемая граница plan size; CI сравнивает normalized ratios/allocations, а не нестабильное одиночное wall time.

### HP-11 — Определить единый Float persistence contract

- Priority / level: P1 / LEVEL 4 с предварительным решением контракта.
- Что делаем: принять одно из двух явно документированных решений: finite-only для persisted Float или tagged representation NaN/±Inf во всех codecs. Предпочтительный минимальный вариант — finite-only validation до state mutation.
- Где делаем: docs/axiom-file-specification.md и runtime semantics; internal/runtime/types_check.go/value write boundary; internal/store/memory и internal/store/pebble codecs.
- Почему: устраняются FRAG-NUMERIC/DEPENDENCY и CE-015.
- Как проверить: differential table Memory, Pebble JSON, Pebble Gob для finite extremes, signed zero, NaN, ±Inf и arithmetic-produced Inf; одинаковые success/error codes.
- Regression test: TestStoreFloatContract и FuzzFloatPersistence.
- Done: backend selection не меняет accepted value domain; migration impact описан до merge.

### HP-12 — Заменить строковую retry classification на typed contract

- Priority / level: P1 / LEVEL 2.
- Что делаем: передаём retryable/failure code отдельным typed полем или error type через task transition. Error() используется только для diagnostics; совместимость со старой history имеет explicit decoder.
- Где делаем: internal/runtime/retry_store.go isRetryableActivityFailure и callers, task/history record types, codec compatibility tests.
- Почему: устраняется FRAG-RETRY/CONTRACT и CE-016.
- Как проверить: wrapping через fmt.Errorf, changed message, localized message и errors.Join не меняют classification; mutation AX505 punctuation не влияет на state.
- Regression test: TestRetryClassificationWrapping, TestLegacyRetryCodeDecode.
- Done: production state transition не ветвится по строковому prefix.

### HP-13 — Расширить CI только на доказанные hotspots

- Priority / level: P1 / LEVEL 0.
- Что делаем: добавляем race для adgo, short fuzz smoke для identity/compile/key codec, nightly longer fuzz, multi-process fault suite, targeted mutation и performance envelope. Minimized fuzz corpus коммитится как regression.
- Где делаем: .github/workflows/test.yml и отдельные scheduled workflows при необходимости; testdata/fuzz в соответствующих packages.
- Почему: текущий race job покрывает root/internal runtime/store, но не adgo; текущий CI fuzz запускает только parser и TRIZ normalizer по 5 секунд; performance job публикует отчёт без regression threshold.
- Как проверить: PR smoke завершается в согласованный бюджет времени; nightly job сохраняет seed/artifacts; intentional mutant в safeName/digest/budget/comparator убивается тестами; intentional graph regression нарушает envelope.
- Regression test: workflow self-test через временную controlled mutation в PR, затем mutation удаляется до merge.
- Done: required checks включают test/module-checksum, adgo-race и hotspot-smoke; nightly failures создают actionable artifact с seed/command.

### HP-14 — Зафиксировать контракты и rollout gates

- Priority / level: P1 / LEVEL 0/4.
- Что делаем: документируем допустимый ID/Float/time/budget domain, migration и rollback. Каждому durable format присваиваем schema version. Для main включаем required checks и запрет обхода migration test после реализации hardening.
- Где делаем: docs/runtime-semantics.md, docs/axiom-file-specification.md, adgo package docs, migration guide и repository rules.
- Почему: часть текущей хрупкости — разрыв Documentation → Types/API → Implementation → Tests.
- Как проверить: contract table имеет один ответ для каждого backend; external-consumer test проверяет публичные validation errors; dry-run migration на копии fixture DB даёт одинаковые record counts/digests.
- Regression test: compatibility fixtures предыдущей schema и public API examples.
- Done: ни один durable change не merge без backward-compatibility decision, migration proof и rollback note.

## 9. Порядок поставки и зависимости

| Волна | Состав | Зависимости | Merge gate |
|---|---|---|---|
| 0 — Evidence | HP-00 | Нет | Каждый CONFIRMED finding представлен failing test |
| 1 — Data safety | HP-02, HP-03, HP-04 | HP-00 | Fail-closed numeric path и atomic error semantics |
| 2 — Identity/storage | HP-01, HP-05 | HP-00, migration design | Injectivity, containment, cross-store isolation, rollback |
| 3 — Concurrency/time | HP-06, HP-08, HP-09, HP-12 | Store contracts | Multi-process/stateful tests, no sleep-based flakes |
| 4 — Decisions/scale | HP-07, HP-10, HP-11 | Numeric contract | Permutation properties, benchmark envelope, backend parity |
| 5 — Continuous assurance | HP-13, HP-14 | Stable targeted tests | Required checks, nightly campaigns, contract docs |

Рекомендуемый размер изменения: один finding или один общий invariant на PR. HP-01 и HP-05 допускают общий codec design, но migrations и production changes должны оставаться раздельно проверяемыми.

## 10. Verification matrix и Definition of Done

| Риск | Unit/boundary | Property/metamorphic | Stateful/concurrency | Fault/differential | CI gate |
|---|---|---|---|---|---|
| Identity | CE-001…004 | injectivity/containment fuzz | concurrent same/different IDs | legacy migration | hotspot fuzz |
| Digest/numeric | CE-005…007 | finite, permutation, monotonic | activity sequence | Memory/File parity | unit + fuzz |
| Atomicity | CE-008 | error ⇒ unchanged | concurrent CAS | serialization/fsync fail | store contract |
| Pebble keyspace | CE-009…010 | component round-trip | mixed executions | Memory/Pebble differential | store contract |
| Locks | threshold boundaries | owner-token invariant | two subprocesses | kill at write phases | multi-process |
| Ranking | NaN and exact ties | permutation/weight sensitivity | health updates | reference slow scorer | unit + fuzz |
| Time/schedule | t−ε/t/t+ε | clock-shift relations | two runners/restart | skew/crash injection | deterministic temporal |
| Complexity | small correctness | graph generator | n/a | old vs optimized result | benchmark envelope |
| Float backend | extremes/non-finite | encode/decode relation | execution replay | Memory/Pebble/Gob | compatibility |
| Retry | wrapping matrix | formatting invariance | retry sequence | legacy record decode | mutation |

Работа считается завершённой не по факту зелёного go test, а когда для каждого Critical algorithm:

1. Контракт входов, выходов и ошибок записан.
2. Invariants выполняются одинаково во всех backends.
3. Минимальный counterexample сохранён и больше не воспроизводится.
4. Boundary, permutation, state, time и retry semantics покрыты там, где применимы.
5. Ошибка dependency не оставляет частично опубликованный state.
6. Durable schema имеет migration, rollback и compatibility fixture.
7. Property/fuzz test сохраняет минимальный seed при failure.
8. Mutation критичного условия убивается тестом.
9. Practical size/time envelope измерен и защищён от резкого regression.
10. Дальнейшее углубление прекращается, если контракт однозначен, invariants доказаны, edge corpus проходит и следующая проверка не даёт новой информации.

## 11. Явно не рекомендуемые изменения

- Не переписывать runtime/state machine целиком: подтверждённые дефекты локализуются validation, key encoding, commit point, clock seam и comparator boundaries.
- Не вводить универсальную storage abstraction сверх существующих Store interfaces. Нужен общий contract suite и маленький codec, а не новый слой.
- Не менять scoring weights до sensitivity report; сами magic constants не являются дефектом без доказанного instability.
- Не включать partial-result/fallback автоматически для data-safety P0: при identity, digest и atomicity ошибках безопасный режим — fail closed.
- Не считать успешный race detector доказательством межпроцессного fencing.

Главный ожидаемый эффект плана: расширить область входов, interleavings, временных условий и backend-конфигураций, в которой Axiom сохраняет один и тот же явный контракт, не теряет identity и не публикует частичное состояние.
