package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type NodeKind string

const (
	NodeActivity     NodeKind = "activity"
	NodeDecision     NodeKind = "decision"
	NodeGate         NodeKind = "gate"
	NodeFork         NodeKind = "fork"
	NodeJoin         NodeKind = "join"
	NodeWait         NodeKind = "wait"
	NodeHuman        NodeKind = "human"
	NodeSubflow      NodeKind = "subflow"
	NodeCompensation NodeKind = "compensation"
)

type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomePass      Outcome = "pass"
	OutcomeRepair    Outcome = "repair"
	OutcomeFail      Outcome = "fail"
	OutcomeHuman     Outcome = "human"
	OutcomeRejected  Outcome = "rejected"
	OutcomeCanceled  Outcome = "canceled"
)

type RiskLevel int

const (
	RiskLow RiskLevel = iota
	RiskMedium
	RiskHigh
	RiskCritical
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	case RiskCritical:
		return "critical"
	default:
		return "unknown"
	}
}

type FailureClass string

const (
	FailureTransient           FailureClass = "transient"
	FailureRateLimit           FailureClass = "rate_limit"
	FailureInvalidInput        FailureClass = "invalid_input"
	FailureQuality             FailureClass = "quality"
	FailurePermanent           FailureClass = "permanent"
	FailureAmbiguousSideEffect FailureClass = "ambiguous_side_effect"
)

type ExecutionStatus string

const (
	StatusRunning      ExecutionStatus = "running"
	StatusWaiting      ExecutionStatus = "waiting"
	StatusHuman        ExecutionStatus = "awaiting_human"
	StatusCompensating ExecutionStatus = "compensating"
	StatusCompleted    ExecutionStatus = "completed"
	StatusFailed       ExecutionStatus = "failed"
	StatusCanceled     ExecutionStatus = "canceled"
	StatusDeadlocked   ExecutionStatus = "deadlocked"
)

type NodeStatus string

const (
	NodeDormant   NodeStatus = "dormant"
	NodePending   NodeStatus = "pending"
	NodeRunning   NodeStatus = "running"
	NodeWaiting   NodeStatus = "waiting"
	NodeCompleted NodeStatus = "completed"
	NodeSkipped   NodeStatus = "skipped"
	NodeFailed    NodeStatus = "failed"
)

type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
)

type JoinMode string

const (
	JoinAll    JoinMode = "all"
	JoinAny    JoinMode = "any"
	JoinNOfM   JoinMode = "n_of_m"
	JoinQuorum JoinMode = "quorum"
)

type RetryPolicy struct {
	MaxAttempts      int           `json:"maxAttempts"`
	BaseDelay        time.Duration `json:"baseDelay"`
	MaxDelay         time.Duration `json:"maxDelay"`
	MaxRetryDuration time.Duration `json:"maxRetryDuration"`
	JitterFraction   float64       `json:"jitterFraction"`
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 30 * time.Second, MaxRetryDuration: 5 * time.Minute, JitterFraction: 0.20}
}

type LoopBound struct {
	MaxIterations int           `json:"maxIterations"`
	MaxCost       float64       `json:"maxCost"`
	MaxDuration   time.Duration `json:"maxDuration"`
	Epsilon       float64       `json:"epsilon"`
}

type JoinSpec struct {
	Mode      JoinMode `json:"mode"`
	Threshold int      `json:"threshold,omitempty"`
}

type WaitSpec struct {
	EventType string        `json:"eventType,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
}

type HumanSpec struct {
	EventType string    `json:"eventType"`
	Risk      RiskLevel `json:"risk"`
}

type QualityGateSpec struct {
	HardFloors        map[string]float64 `json:"hardFloors,omitempty"`
	MaxCriticalErrors int                `json:"maxCriticalErrors,omitempty"`
	RepairFrom        []string           `json:"repairFrom,omitempty"`
}

type Transition struct {
	To      string  `json:"to"`
	Outcome Outcome `json:"outcome,omitempty"`
}

type Node struct {
	ID                  string           `json:"id"`
	Kind                NodeKind         `json:"kind"`
	Activity            string           `json:"activity,omitempty"`
	Capability          string           `json:"capability,omitempty"`
	DependsOn           []string         `json:"dependsOn,omitempty"`
	Next                []Transition     `json:"next,omitempty"`
	Requires            []string         `json:"requires,omitempty"`
	Produces            []string         `json:"produces,omitempty"`
	Writes              []string         `json:"writes,omitempty"`
	ResourceKeys        []string         `json:"resourceKeys,omitempty"`
	RequiredPermissions []string         `json:"requiredPermissions,omitempty"`
	Risk                RiskLevel        `json:"risk"`
	Timeout             time.Duration    `json:"timeout,omitempty"`
	Retry               RetryPolicy      `json:"retry,omitempty"`
	IdempotencyKey      string           `json:"idempotencyKey,omitempty"`
	ExternalEffect      bool             `json:"externalEffect,omitempty"`
	Irreversible        bool             `json:"irreversible,omitempty"`
	Compensation        string           `json:"compensation,omitempty"`
	Join                *JoinSpec        `json:"join,omitempty"`
	Wait                *WaitSpec        `json:"wait,omitempty"`
	Human               *HumanSpec       `json:"human,omitempty"`
	Gate                *QualityGateSpec `json:"gate,omitempty"`
	Loop                *LoopBound       `json:"loop,omitempty"`
	MaxFanout           int              `json:"maxFanout,omitempty"`
	EstimatedCost       float64          `json:"estimatedCost,omitempty"`
	EstimatedLatency    time.Duration    `json:"estimatedLatency,omitempty"`
	ExpectedQualityGain float64          `json:"expectedQualityGain,omitempty"`
	CriticalPathWeight  float64          `json:"criticalPathWeight,omitempty"`
}

type Definition struct {
	ID                 string            `json:"id"`
	Version            string            `json:"version"`
	Nodes              []Node            `json:"nodes"`
	InitialData        []string          `json:"initialData,omitempty"`
	AllowedPermissions []string          `json:"allowedPermissions,omitempty"`
	GlobalConcurrency  int               `json:"globalConcurrency,omitempty"`
	CapabilityLimits   map[string]int    `json:"capabilityLimits,omitempty"`
	ActivityLimits     map[string]int    `json:"activityLimits,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

type Plan struct {
	ID                 string
	Version            string
	Digest             string
	Nodes              map[string]Node
	Entry              []string
	InitialData        map[string]struct{}
	AllowedPermissions map[string]struct{}
	GlobalConcurrency  int
	CapabilityLimits   map[string]int
	ActivityLimits     map[string]int
	Metadata           map[string]string
	inbound            map[string][]string
	outbound           map[string][]Transition
	producers          map[string][]string
	descendants        map[string]map[string]struct{}
}

type ArtifactRef struct {
	URI       string `json:"uri"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType,omitempty"`
}

type QualityVector map[string]float64

type BudgetLimit struct {
	MaxCost           float64       `json:"maxCost,omitempty"`
	MaxTokens         int64         `json:"maxTokens,omitempty"`
	MaxDuration       time.Duration `json:"maxDuration,omitempty"`
	MaxLLMCalls       int           `json:"maxLLMCalls,omitempty"`
	MaxSearchQueries  int           `json:"maxSearchQueries,omitempty"`
	MaxBrowserFetches int           `json:"maxBrowserFetches,omitempty"`
}

type BudgetUsage struct {
	Cost           float64       `json:"cost"`
	Tokens         int64         `json:"tokens"`
	ActiveDuration time.Duration `json:"activeDuration"`
	LLMCalls       int           `json:"llmCalls"`
	SearchQueries  int           `json:"searchQueries"`
	BrowserFetches int           `json:"browserFetches"`
}

type NodeRuntime struct {
	Status         NodeStatus   `json:"status"`
	Outcome        Outcome      `json:"outcome,omitempty"`
	Attempts       int          `json:"attempts"`
	Iterations     int          `json:"iterations"`
	Activated      bool         `json:"activated"`
	NotBefore      time.Time    `json:"notBefore,omitempty"`
	StartedAt      time.Time    `json:"startedAt,omitempty"`
	FirstAttemptAt time.Time    `json:"firstAttemptAt,omitempty"`
	CompletedAt    time.Time    `json:"completedAt,omitempty"`
	LastError      string       `json:"lastError,omitempty"`
	LastFailure    FailureClass `json:"lastFailure,omitempty"`
	Signature      string       `json:"signature,omitempty"`
}

type TaskRuntime struct {
	ID             string     `json:"id"`
	NodeID         string     `json:"nodeId"`
	Activity       string     `json:"activity"`
	IdempotencyKey string     `json:"idempotencyKey"`
	Attempt        int        `json:"attempt"`
	Status         TaskStatus `json:"status"`
	WorkerID       string     `json:"workerId,omitempty"`
	LeaseUntil     time.Time  `json:"leaseUntil,omitempty"`
	StartedAt      time.Time  `json:"startedAt,omitempty"`
}

type QualitySnapshot struct {
	At      time.Time     `json:"at"`
	NodeID  string        `json:"nodeId"`
	Values  QualityVector `json:"values"`
	Utility float64       `json:"utility"`
}

type HistoryEntry struct {
	Seq     uint64         `json:"seq"`
	At      time.Time      `json:"at"`
	Type    string         `json:"type"`
	NodeID  string         `json:"nodeId,omitempty"`
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type CompensationEntry struct {
	NodeID         string `json:"nodeId"`
	Activity       string `json:"activity"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type PlanDeltaRecord struct {
	Digest    string    `json:"digest"`
	AppliedAt time.Time `json:"appliedAt"`
	Reason    string    `json:"reason,omitempty"`
}

type Execution struct {
	ID                string                     `json:"id"`
	PlanID            string                     `json:"planId"`
	PlanVersion       string                     `json:"planVersion"`
	PlanDigest        string                     `json:"planDigest"`
	Version           uint64                     `json:"version"`
	Status            ExecutionStatus            `json:"status"`
	Nodes             map[string]*NodeRuntime    `json:"nodes"`
	Data              map[string]json.RawMessage `json:"data,omitempty"`
	Artifacts         map[string]ArtifactRef     `json:"artifacts,omitempty"`
	Quality           QualityVector              `json:"quality,omitempty"`
	QualityHistory    []QualitySnapshot          `json:"qualityHistory,omitempty"`
	BudgetLimit       BudgetLimit                `json:"budgetLimit"`
	BudgetUsage       BudgetUsage                `json:"budgetUsage"`
	ActiveTasks       map[string]TaskRuntime     `json:"activeTasks,omitempty"`
	SeenEvents        map[string]bool            `json:"seenEvents,omitempty"`
	RevisionCounters  map[string]int             `json:"revisionCounters,omitempty"`
	StrategyBans      map[string]bool            `json:"strategyBans,omitempty"`
	Signatures        []string                   `json:"signatures,omitempty"`
	WaitingFor        map[string]string          `json:"waitingFor,omitempty"`
	ThrottleUntil     map[string]time.Time       `json:"throttleUntil,omitempty"`
	CompensationStack []CompensationEntry        `json:"compensationStack,omitempty"`
	PlanDeltas        []PlanDeltaRecord          `json:"planDeltas,omitempty"`
	CancelRequested   bool                       `json:"cancelRequested,omitempty"`
	Failure           string                     `json:"failure,omitempty"`
	History           []HistoryEntry             `json:"history,omitempty"`
	Metrics           Metrics                    `json:"metrics"`
	CreatedAt         time.Time                  `json:"createdAt"`
	UpdatedAt         time.Time                  `json:"updatedAt"`
}

type Event struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	TargetNode string          `json:"targetNode,omitempty"`
	At         time.Time       `json:"at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type ActivityRequest struct {
	ExecutionID    string
	NodeID         string
	Attempt        int
	IdempotencyKey string
	Data           map[string]json.RawMessage
	Artifacts      map[string]ArtifactRef
	Deadline       time.Time
}

type ActivityResult struct {
	Facts     map[string]any
	Artifacts map[string]ArtifactRef
	Quality   QualityVector
	Budget    BudgetUsage
	Outcome   Outcome
	Signature string
	Metrics   map[string]float64
}

type ActivityHandler func(context.Context, ActivityRequest) (ActivityResult, error)
type DecisionHandler func(context.Context, Snapshot) (Outcome, error)
type GateHandler func(context.Context, Snapshot) (GateResult, error)
type SubflowHandler func(context.Context, ActivityRequest) (ActivityResult, error)
type CompensationHandler func(context.Context, ActivityRequest) error

type GateResult struct {
	Outcome    Outcome
	Violations []Violation
	Quality    QualityVector
}

type Violation struct {
	Code        string
	Message     string
	Dimension   string
	Required    float64
	Observed    float64
	Critical    bool
	RepairFrom  []string
	MissingData []string
}

type Snapshot struct {
	ExecutionID string
	Status      ExecutionStatus
	Data        map[string]json.RawMessage
	Artifacts   map[string]ArtifactRef
	Quality     QualityVector
	BudgetLimit BudgetLimit
	BudgetUsage BudgetUsage
	Nodes       map[string]NodeRuntime
}

type FailureError struct {
	Class      FailureClass
	Err        error
	RetryAfter time.Duration
}

func (e *FailureError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Class)
}
func (e *FailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Fail(class FailureClass, err error) error { return &FailureError{Class: class, Err: err} }
func FailAfter(class FailureClass, err error, after time.Duration) error {
	return &FailureError{Class: class, Err: err, RetryAfter: after}
}
func RateLimited(after time.Duration, err error) error {
	return &FailureError{Class: FailureRateLimit, Err: err, RetryAfter: after}
}

var (
	ErrExecutionNotFound = errors.New("adgo: execution not found")
	ErrExecutionExists   = errors.New("adgo: execution already exists")
	ErrConflict          = errors.New("adgo: optimistic concurrency conflict")
	ErrDeadlock          = errors.New("adgo: deadlock")
	ErrBudgetExceeded    = errors.New("adgo: budget exceeded")
)

func Data[T any](e *Execution, key string) (T, bool, error) {
	var zero T
	raw, ok := e.Data[key]
	if !ok {
		return zero, false, nil
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, true, fmt.Errorf("decode data %q: %w", key, err)
	}
	return value, true, nil
}

func SetData(e *Execution, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode data %q: %w", key, err)
	}
	if e.Data == nil {
		e.Data = map[string]json.RawMessage{}
	}
	e.Data[key] = raw
	return nil
}
