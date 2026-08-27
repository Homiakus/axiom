package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/Homiakus/axiom/internal/compiler"
	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/durabletime"
	"github.com/Homiakus/axiom/internal/syncx"
)

type FieldID = compiler.FieldID
type SignalID = compiler.SignalID
type RuleID = compiler.RuleID
type ActivityID = compiler.ActivityID
type AtomID uint32

type Status string

const (
	StatusStarted   Status = "Started"
	StatusRunning   Status = "Running"
	StatusWaiting   Status = "Waiting"
	StatusCompleted Status = "Completed"
	StatusFailed    Status = "Failed"
	StatusCanceled  Status = "Canceled"
)

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskRunning    TaskStatus = "running"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
	TaskSuperseded TaskStatus = "superseded"
)

type FactValue struct {
	True    bool
	Exposed map[string]any
}

type ValueKind string

const (
	ValueInvalid ValueKind = ""
	ValueNull    ValueKind = "null"
	ValueBool    ValueKind = "bool"
	ValueInt     ValueKind = "int"
	ValueFloat   ValueKind = "float"
	ValueString  ValueKind = "string"
	ValueAny     ValueKind = "any"
)

type Value struct {
	Kind ValueKind
	I64  int64
	F64  float64
	S    string
	B    bool
	Any  any
}

type ExecutionState struct {
	ActiveAtoms []uint64
	Present     []uint64
	BoolValues  []uint64
	DirtyFields []uint64
	Values      map[uint32]Value
	AtomValues  map[uint32]Value
	FactValues  map[uint32]map[string]Value
}

type Execution struct {
	ID              string
	Domain          string
	Status          Status
	Context         map[string]map[string]any
	Computed        map[string]any
	Facts           map[string]FactValue
	RuntimeState    ExecutionState
	ModuleHash      string
	CompilerVersion string
	PlanVersion     string
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type HistoryEntry struct {
	Seq       int
	Type      string
	Payload   map[string]any
	CreatedAt time.Time
}

type ActivityTask struct {
	ID             string
	ExecutionID    string
	RuleName       string
	ActivityName   string
	Input          map[string]any
	IdempotencyKey string
	Status         TaskStatus
	Attempt        int
	MaxAttempts    int
	LockedBy       string
	LockedUntil    time.Time
	NextAttemptAt  time.Time
	Result         map[string]any
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Store interface {
	CreateExecution(ctx context.Context, execution *Execution) error
	GetExecution(ctx context.Context, id string) (*Execution, error)
	SaveExecution(ctx context.Context, execution *Execution) error
	AppendHistory(ctx context.Context, executionID string, entryType string, payload map[string]any) error
	ListHistory(ctx context.Context, executionID string) ([]HistoryEntry, error)
	EnqueueTask(ctx context.Context, task *ActivityTask) error
	ListTasks(ctx context.Context, executionID string) ([]*ActivityTask, error)
	PollTask(ctx context.Context, executionID string) (*ActivityTask, error)
	PollTaskWithLease(ctx context.Context, executionID string, workerID string, leaseTTL time.Duration) (*ActivityTask, error)
	HeartbeatTask(ctx context.Context, taskID string, workerID string) error
	RecoverExpiredLeases(ctx context.Context, executionID string, leaseTTL time.Duration) (int, error)
	CompleteTask(ctx context.Context, taskID string, result map[string]any) error
	FailTask(ctx context.Context, taskID string, errorMessage string) error
	UpdateTask(ctx context.Context, task *ActivityTask) error
}

type StoreTransaction interface {
	Store
	Commit() error
	Rollback() error
}

type TransactionalStore interface {
	BeginTransaction(ctx context.Context) (StoreTransaction, error)
}

type Activity func(ctx context.Context, input map[string]any) (map[string]any, error)

type ActivityRegistry map[string]Activity

type DiagnosticError = diag.Error

type TraceLevel string

const (
	TraceAggregate TraceLevel = "aggregate"
	TraceFull      TraceLevel = "full"
	TraceMinimal   TraceLevel = "minimal"
)

type Engine struct {
	module             *compiler.Module
	store              Store
	activities         ActivityRegistry
	externalActivities map[string]struct{}
	maxSteps           int
	fast               *fastPlan
	strictFast         bool
	traceLevel         TraceLevel
	storeMu            sync.Mutex
	clock              Clock
	executionLocks     *syncx.KeyedLocker
}

func NewEngine(module *compiler.Module, store Store, activities ActivityRegistry) *Engine {
	if activities == nil {
		activities = ActivityRegistry{}
	}
	activities = applyActivityPolicies(module, activities)
	fast := compileFastPlan(module, false)
	engine := &Engine{
		module:             module,
		store:              store,
		activities:         activities,
		externalActivities: map[string]struct{}{},
		maxSteps:           1000,
		fast:               fast,
		traceLevel:         TraceAggregate,
		clock:              systemClock{},
		executionLocks:     syncx.NewKeyedLocker(),
	}
	// retryStore resolves semantic time through engine.now on every decision.
	// This keeps retry scheduling and due checks in the same clock domain even
	// when tests or simulation replace the Engine clock after construction.
	engine.store = newRetryStore(module, store, engine.now)
	return engine
}

func (e *Engine) Module() *compiler.Module {
	return e.module
}

// SetExternalActivities declares activity names that are owned by an external
// fenced worker rather than this Engine's inline activity runner. The set is
// copied so construction-time configuration remains immutable to callers.
func (e *Engine) SetExternalActivities(names map[string]struct{}) {
	if e == nil {
		return
	}
	e.externalActivities = make(map[string]struct{}, len(names))
	for name := range names {
		e.externalActivities[name] = struct{}{}
	}
}

func (e *Engine) isExternalActivity(name string) bool {
	if e == nil {
		return false
	}
	_, ok := e.externalActivities[name]
	return ok
}

func (e *Engine) EnableStrictFastRuntime() error {
	plan := compileFastPlan(e.module, true)
	if err := plan.strictError(); err != nil {
		return err
	}
	e.fast = plan
	e.strictFast = true
	return nil
}

func (e *Engine) SetTraceLevel(level TraceLevel) {
	if level == "" {
		level = TraceAggregate
	}
	e.traceLevel = level
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

func (e *Engine) SetClock(clock Clock) {
	if clock != nil {
		e.clock = clock
	}
}

func (e *Engine) now() time.Time {
	if e.clock == nil {
		return time.Now().UTC()
	}
	return e.clock.Now().UTC()
}

func (e *Engine) newTimer(d time.Duration) durabletime.Timer {
	if tc, ok := e.clock.(durabletime.Clock); ok {
		return tc.NewTimer(d)
	}
	if tc, ok := e.clock.(interface{ NewTimer(time.Duration) durabletime.Timer }); ok {
		return tc.NewTimer(d)
	}
	return durabletime.SystemClock{}.NewTimer(d)
}
