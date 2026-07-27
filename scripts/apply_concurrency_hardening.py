from pathlib import Path


def replace(path: str, old: str, new: str, count: int = 1) -> None:
    file = Path(path)
    text = file.read_text()
    actual = text.count(old)
    if actual != count:
        raise SystemExit(f"{path}: expected {count} matches, found {actual}: {old[:80]!r}")
    file.write_text(text.replace(old, new, count))


Path("internal/runtime/transaction.go").write_text(r'''package runtime

import "context"

func (e *Engine) withStoreTransaction(ctx context.Context, fn func(*Engine) error) error {
	transactional, ok := e.store.(TransactionalStore)
	if !ok {
		return fn(e)
	}
	e.storeMu.Lock()
	defer e.storeMu.Unlock()
	tx, err := transactional.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	working := &Engine{
		module:         e.module,
		store:          tx,
		activities:     e.activities,
		maxSteps:       e.maxSteps,
		fast:           e.fast,
		strictFast:     e.strictFast,
		traceLevel:     e.traceLevel,
		clock:          e.clock,
		executionLocks: e.executionLocks,
	}
	if err := fn(working); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
''')

replace(
    "internal/runtime/types.go",
    '"github.com/Homiakus/axiom/internal/diag"\n',
    '"github.com/Homiakus/axiom/internal/diag"\n\t"github.com/Homiakus/axiom/internal/syncx"\n',
)
replace(
    "internal/runtime/types.go",
    '''\tstoreMu    sync.Mutex
\tclock      Clock
''',
    '''\tstoreMu        sync.Mutex
\tclock          Clock
\texecutionLocks *syncx.KeyedLocker
''',
)
replace(
    "internal/runtime/types.go",
    '''\t\ttraceLevel: TraceAggregate,
\t\tclock:      systemClock{},
''',
    '''\t\ttraceLevel:     TraceAggregate,
\t\tclock:          systemClock{},
\t\texecutionLocks: syncx.NewKeyedLocker(),
''',
)

for old, new in [
    ('''return e.withStoreTransaction(ctx, func() error {
\t\treturn e.start(ctx, executionID, initialContext)
\t})''', '''return e.withStoreTransaction(ctx, func(working *Engine) error {
\t\treturn working.start(ctx, executionID, initialContext)
\t})'''),
    ('''return e.withStoreTransaction(ctx, func() error {
\t\treturn e.signal(ctx, executionID, signalName, payload)
\t})''', '''return e.withStoreTransaction(ctx, func(working *Engine) error {
\t\treturn working.signal(ctx, executionID, signalName, payload)
\t})'''),
    ('''return e.withStoreTransaction(ctx, func() error {
\t\treturn e.patch(ctx, executionID, patch)
\t})''', '''return e.withStoreTransaction(ctx, func(working *Engine) error {
\t\treturn working.patch(ctx, executionID, patch)
\t})'''),
    ('''if err := e.withStoreTransaction(ctx, func() error {
\t\t\treturn e.completeActivity(ctx, executionID, task, result, runErr)
\t\t}); err != nil {''', '''if err := e.withStoreTransaction(ctx, func(working *Engine) error {
\t\t\treturn working.completeActivity(ctx, executionID, task, result, runErr)
\t\t}); err != nil {'''),
]:
    replace("internal/runtime/engine.go", old, new)

replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) ID() string { return r.id }
''',
    '''func (r *Run) ID() string { return r.id }

func (r *Run) lock() (func(), error) {
\tif r == nil || r.engine == nil {
\t\treturn nil, fmt.Errorf("axiom: execution handle is not initialized")
\t}
\tif r.id == "" {
\t\treturn nil, fmt.Errorf("axiom: execution id is required")
\t}
\tif r.engine.executionLocks == nil {
\t\treturn nil, fmt.Errorf("axiom: execution lock registry is not initialized")
\t}
\treturn r.engine.executionLocks.Lock(r.id), nil
}
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''\tif r == nil || r.engine == nil {
\t\treturn fmt.Errorf("axiom: execution handle is not initialized")
\t}
\tif r.id == "" {
\t\treturn fmt.Errorf("axiom: execution id is required")
\t}
\tname, payload, err := eventPayload(event)
''',
    '''\tunlock, err := r.lock()
\tif err != nil {
\t\treturn err
\t}
\tdefer unlock()
\tname, payload, err := eventPayload(event)
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) Signal(ctx context.Context, name string, payload map[string]any) error {
\tif r == nil || r.engine == nil {
\t\treturn fmt.Errorf("axiom: execution handle is not initialized")
\t}
\tif _, err := r.engine.store.GetExecution(ctx, r.id); err != nil {
''',
    '''func (r *Run) Signal(ctx context.Context, name string, payload map[string]any) error {
\tunlock, err := r.lock()
\tif err != nil {
\t\treturn err
\t}
\tdefer unlock()
\tif _, err := r.engine.store.GetExecution(ctx, r.id); err != nil {
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) State(ctx context.Context, target any) error {
\tif target == nil {
''',
    '''func (r *Run) State(ctx context.Context, target any) error {
\tunlock, err := r.lock()
\tif err != nil {
\t\treturn err
\t}
\tdefer unlock()
\tif target == nil {
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''\tresult, err := r.engine.Query(ctx, r.id, "state")
''',
    '''\tresult, err := r.engine.Query(ctx, r.id, "state")
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) Status(ctx context.Context) (Status, error) {
\texecution, err := r.engine.store.GetExecution(ctx, r.id)
''',
    '''func (r *Run) Status(ctx context.Context) (Status, error) {
\tunlock, err := r.lock()
\tif err != nil {
\t\treturn "", err
\t}
\tdefer unlock()
\texecution, err := r.engine.store.GetExecution(ctx, r.id)
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) History(ctx context.Context) ([]HistoryEntry, error) {
\treturn r.engine.store.ListHistory(ctx, r.id)
}
''',
    '''func (r *Run) History(ctx context.Context) ([]HistoryEntry, error) {
\tunlock, err := r.lock()
\tif err != nil {
\t\treturn nil, err
\t}
\tdefer unlock()
\treturn r.engine.store.ListHistory(ctx, r.id)
}
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) PendingActivities(ctx context.Context) ([]ActivityTask, error) {
\tresult, err := r.engine.Query(ctx, r.id, "pendingActivities")
''',
    '''func (r *Run) PendingActivities(ctx context.Context) ([]ActivityTask, error) {
\tunlock, err := r.lock()
\tif err != nil {
\t\treturn nil, err
\t}
\tdefer unlock()
\tresult, err := r.engine.Query(ctx, r.id, "pendingActivities")
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) Explain(ctx context.Context) (*Explanation, error) {
\texecution, err := r.engine.store.GetExecution(ctx, r.id)
''',
    '''func (r *Run) Explain(ctx context.Context) (*Explanation, error) {
\tunlock, err := r.lock()
\tif err != nil {
\t\treturn nil, err
\t}
\tdefer unlock()
\texecution, err := r.engine.store.GetExecution(ctx, r.id)
''',
)
replace(
    "internal/runtime/execution_api.go",
    '''func (r *Run) Cancel(ctx context.Context) error {
\treturn r.engine.withStoreTransaction(ctx, func() error {
\t\texecution, err := r.engine.store.GetExecution(ctx, r.id)
''',
    '''func (r *Run) Cancel(ctx context.Context) error {
\tunlock, err := r.lock()
\tif err != nil {
\t\treturn err
\t}
\tdefer unlock()
\treturn r.engine.withStoreTransaction(ctx, func(working *Engine) error {
\t\texecution, err := working.store.GetExecution(ctx, r.id)
''',
)
replace("internal/runtime/execution_api.go", 'if err := r.engine.store.AppendHistory(ctx, r.id, "ExecutionCanceled", nil); err != nil {', 'if err := working.store.AppendHistory(ctx, r.id, "ExecutionCanceled", nil); err != nil {')
replace("internal/runtime/execution_api.go", 'return r.engine.store.SaveExecution(ctx, execution)', 'return working.store.SaveExecution(ctx, execution)')

replace(
    "flow.go",
    '"time"\n)',
    '"time"\n\n\t"github.com/Homiakus/axiom/internal/syncx"\n)',
)
replace(
    "flow.go",
    '''type FlowEngine[S any] struct {
\tflow  *Flow[S]
\tstore FlowStore
}
''',
    '''type FlowEngine[S any] struct {
\tflow  *Flow[S]
\tstore FlowStore
\tlocks *syncx.KeyedLocker
}
''',
)
replace(
    "flow.go",
    '''return &FlowEngine[S]{flow: flow, store: config.store}, nil
''',
    '''return &FlowEngine[S]{flow: flow, store: config.store, locks: syncx.NewKeyedLocker()}, nil
''',
)
replace(
    "flow.go",
    '''\thandler, normalized, err := e.engine.flow.handlerFor(event)
''',
    '''\tunlock := e.engine.locks.Lock(e.id)
\tdefer unlock()
\thandler, normalized, err := e.engine.flow.handlerFor(event)
''',
)
replace(
    "flow.go",
    '''func (e *FlowExecution[S]) State(ctx context.Context) (S, error) {
\tstate, _, err := e.load(ctx)
''',
    '''func (e *FlowExecution[S]) State(ctx context.Context) (S, error) {
\tunlock := e.engine.locks.Lock(e.id)
\tdefer unlock()
\tstate, _, err := e.load(ctx)
''',
)
replace(
    "flow.go",
    '''func (e *FlowExecution[S]) History(ctx context.Context) ([]FlowHistoryEntry, error) {
\t_, history, _, err := e.engine.store.Load(ctx, e.engine.flow.name, e.id)
''',
    '''func (e *FlowExecution[S]) History(ctx context.Context) ([]FlowHistoryEntry, error) {
\tunlock := e.engine.locks.Lock(e.id)
\tdefer unlock()
\t_, history, _, err := e.engine.store.Load(ctx, e.engine.flow.name, e.id)
''',
)

print("concurrency hardening applied")
