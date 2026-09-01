package adgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoWork          = errors.New("adgo: no work available")
	ErrStaleTask       = errors.New("adgo: stale or fenced task")
	ErrExecutionPaused = errors.New("adgo: execution is paused")
)

// Engine is the production coordinator/worker facade over the deterministic
// Runtime kernel. Runtime remains useful for embedded single-process execution;
// Engine adds durable queues, external workers, heartbeats, fencing, fair
// polling, adaptive provider routing and a coordinator service loop.
type Engine struct {
	plan                *Plan
	store               Store
	registry            *Registry
	runtime             *Runtime
	scheduler           Scheduler
	router              *AdaptiveRouter
	leaseTTL            time.Duration
	pollInterval        time.Duration
	coordinatorInterval time.Duration
	maxLeaseRecoveries  int

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

type EngineOption func(*Engine)

func WithEngineScheduler(s Scheduler) EngineOption {
	return func(e *Engine) {
		if s != nil {
			e.scheduler = s
		}
	}
}

func WithEngineLeaseTTL(ttl time.Duration) EngineOption {
	return func(e *Engine) {
		if ttl > 0 {
			e.leaseTTL = ttl
		}
	}
}

func WithEnginePollInterval(interval time.Duration) EngineOption {
	return func(e *Engine) {
		if interval > 0 {
			e.pollInterval = interval
		}
	}
}

func WithCoordinatorInterval(interval time.Duration) EngineOption {
	return func(e *Engine) {
		if interval > 0 {
			e.coordinatorInterval = interval
		}
	}
}

func WithMaxLeaseRecoveries(max int) EngineOption {
	return func(e *Engine) {
		if max > 0 {
			e.maxLeaseRecoveries = max
		}
	}
}

func WithAdaptiveRouter(router *AdaptiveRouter) EngineOption {
	return func(e *Engine) {
		if router != nil {
			e.router = router
		}
	}
}

func NewEngine(plan *Plan, store Store, registry *Registry, opts ...EngineOption) (*Engine, error) {
	if plan == nil {
		return nil, fmt.Errorf("adgo: plan is required")
	}
	if store == nil {
		return nil, fmt.Errorf("adgo: store is required")
	}
	if registry == nil {
		registry = NewRegistry()
	}
	scheduler := DefaultScheduler()
	runtime, err := NewRuntime(plan, store, registry, WithScheduler(scheduler))
	if err != nil {
		return nil, err
	}
	e := &Engine{
		plan:                plan,
		store:               store,
		registry:            registry,
		runtime:             runtime,
		scheduler:           scheduler,
		router:              NewAdaptiveRouter(registry, DefaultRouterConfig()),
		leaseTTL:            30 * time.Second,
		pollInterval:        100 * time.Millisecond,
		coordinatorInterval: 50 * time.Millisecond,
		maxLeaseRecoveries:  5,
		locks:               map[string]*sync.Mutex{},
	}
	for _, opt := range opts {
		opt(e)
	}
	e.runtime.scheduler = e.scheduler
	e.runtime.leaseTTL = e.leaseTTL
	return e, nil
}

func (e *Engine) Runtime() *Runtime       { return e.runtime }
func (e *Engine) Router() *AdaptiveRouter { return e.router }
func (e *Engine) Store() Store            { return e.store }
func (e *Engine) Plan() *Plan             { return e.plan }

func (e *Engine) Start(ctx context.Context, id string, initial map[string]any, budget BudgetLimit) (*Execution, error) {
	return e.runtime.Start(ctx, id, initial, budget)
}

// StartOrLoad makes an execution id a workflow-level idempotency key. If an
// execution already exists under the same pinned plan, it is returned instead
// of being started twice.
func (e *Engine) StartOrLoad(ctx context.Context, id string, initial map[string]any, budget BudgetLimit) (*Execution, error) {
	execution, err := e.runtime.Start(ctx, id, initial, budget)
	if err == nil {
		return execution, nil
	}
	if !errors.Is(err, ErrExecutionExists) {
		return nil, err
	}
	execution, err = e.store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if execution.PlanID != e.plan.ID || execution.PlanDigest != e.plan.Digest {
		return nil, fmt.Errorf("adgo: execution id %q exists under a different plan", id)
	}
	return execution, nil
}

func (e *Engine) Get(ctx context.Context, id string) (*Execution, error) { return e.store.Load(ctx, id) }

// Advance performs coordinator-only work: ingest signals, recover leases, run
// deterministic nodes, derive the ready set and durably enqueue external work.
// No ActivityHandler is called by this method.
func (e *Engine) Advance(ctx context.Context, executionID string) (AdvanceResult, error) {
	unlock := e.lockExecution(executionID)
	defer unlock()

	cur, err := e.store.Load(ctx, executionID)
	if err != nil {
		return AdvanceResult{}, err
	}
	if err := e.verifyPinned(cur); err != nil {
		return AdvanceResult{}, err
	}
	if terminal(cur.Status) {
		return advanceResult(cur, false, nil, nil), nil
	}
	if executionPaused(cur) {
		return advanceResult(cur, false, nil, []string{"$execution"}), nil
	}

	progressed := false
	if changed, err := e.runtime.ingest(ctx, cur); err != nil {
		return AdvanceResult{}, err
	} else if changed {
		progressed = true
		cur, err = e.store.Load(ctx, executionID)
		if err != nil {
			return AdvanceResult{}, err
		}
	}
	if executionPaused(cur) {
		return advanceResult(cur, progressed, nil, []string{"$execution"}), nil
	}

	if changed, err := e.runtime.recoverExpired(ctx, cur); err != nil {
		return AdvanceResult{}, err
	} else if changed {
		progressed = true
		cur, err = e.store.Load(ctx, executionID)
		if err != nil {
			return AdvanceResult{}, err
		}
		if err := e.enforceRecoveryLimit(ctx, cur); err != nil {
			return AdvanceResult{}, err
		}
		cur, _ = e.store.Load(ctx, executionID)
	}

	if cur.CancelRequested {
		if err := e.runtime.compensate(ctx, cur, StatusCanceled, "cancellation requested"); err != nil {
			return AdvanceResult{}, err
		}
		cur, _ = e.store.Load(ctx, executionID)
		return advanceResult(cur, true, nil, nil), nil
	}
	if err := checkBudget(cur, e.now()); err != nil {
		if err := e.runtime.compensate(ctx, cur, StatusFailed, err.Error()); err != nil {
			return AdvanceResult{}, err
		}
		cur, _ = e.store.Load(ctx, executionID)
		return advanceResult(cur, true, nil, nil), nil
	}

	if changed, err := e.runtime.runInternal(ctx, cur); err != nil {
		return AdvanceResult{}, err
	} else if changed {
		progressed = true
		cur, err = e.store.Load(ctx, executionID)
		if err != nil {
			return AdvanceResult{}, err
		}
	}
	if terminal(cur.Status) || executionPaused(cur) {
		return advanceResult(cur, progressed, nil, waitingNodes(cur)), nil
	}

	// One coordinator super-step must make readiness and deadlock decisions from
	// the same semantic-time snapshot. Otherwise a NotBefore deadline can expire
	// between ready-set derivation and the pending-time check, making a runnable
	// retry look deadlocked for one irreversible commit.
	decisionNow := e.now()
	candidates, waiting, err := e.readyCandidatesAt(ctx, cur, decisionNow)
	if err != nil {
		return AdvanceResult{}, err
	}
	cur, err = e.store.Load(ctx, executionID)
	if err != nil {
		return AdvanceResult{}, err
	}
	candidates = e.filterAgainstActive(cur, candidates)
	selected := e.scheduler.Select(e.plan, cur, candidates)
	remaining := e.remainingConcurrency(cur)
	if remaining >= 0 && len(selected) > remaining {
		selected = selected[:remaining]
	}
	if len(selected) > 0 {
		queued, err := e.enqueue(ctx, executionID, selected)
		if err != nil {
			if errors.Is(err, errAdvanceStale) {
				cur, _ = e.store.Load(ctx, executionID)
				return advanceResult(cur, progressed, nil, waiting), nil
			}
			return AdvanceResult{}, err
		}
		cur, _ = e.store.Load(ctx, executionID)
		return advanceResult(cur, true, queued, waiting), nil
	}

	if len(cur.ActiveTasks) > 0 {
		return advanceResult(cur, progressed, activeTaskIDs(cur), waiting), nil
	}
	if goalsSatisfied(e.plan, cur) {
		cur, err = e.mutate(ctx, executionID, func(x *Execution) error {
			if terminal(x.Status) {
				return nil
			}
			x.Status = StatusCompleted
			x.Metrics.WallTime = time.Since(x.CreatedAt)
			appendHistory(x, "execution_completed", "", "all activated goals completed", nil)
			return nil
		})
		if err != nil {
			return AdvanceResult{}, err
		}
		return advanceResult(cur, true, nil, nil), nil
	}
	if len(waiting) > 0 || hasPendingTimeAt(cur, decisionNow) {
		if cur.Status != StatusWaiting && cur.Status != StatusHuman {
			cur, err = e.mutate(ctx, executionID, func(x *Execution) error {
				x.Status = StatusWaiting
				appendHistory(x, "execution_waiting", "", "waiting for timer, retry or external event", map[string]any{"nodes": waiting})
				return nil
			})
			if err != nil {
				return AdvanceResult{}, err
			}
			progressed = true
		}
		return advanceResult(cur, progressed, nil, waiting), nil
	}

	cur, err = e.mutate(ctx, executionID, func(x *Execution) error {
		x.Status = StatusDeadlocked
		x.Failure = deadlockReason(e.plan, x)
		appendHistory(x, "deadlock", "", x.Failure, nil)
		return nil
	})
	if err != nil {
		return AdvanceResult{}, err
	}
	return advanceResult(cur, true, nil, nil), ErrDeadlock
}

type AdvanceResult struct {
	ExecutionID string          `json:"executionId"`
	Status      ExecutionStatus `json:"status"`
	Progressed  bool            `json:"progressed"`
	QueuedTasks []string        `json:"queuedTasks,omitempty"`
	Waiting     []string        `json:"waiting,omitempty"`
}

func advanceResult(execution *Execution, progressed bool, queued, waiting []string) AdvanceResult {
	result := AdvanceResult{Progressed: progressed, QueuedTasks: queued, Waiting: waiting}
	if execution != nil {
		result.ExecutionID = execution.ID
		result.Status = execution.Status
	}
	return result
}

func (e *Engine) readyCandidates(ctx context.Context, execution *Execution) ([]Candidate, []string, error) {
	return e.readyCandidatesAt(ctx, execution, e.now())
}

func (e *Engine) readyCandidatesAt(ctx context.Context, execution *Execution, now time.Time) ([]Candidate, []string, error) {
	out := make([]Candidate, 0)
	waiting := make([]string, 0)
	ids := make([]string, 0, len(e.plan.Nodes))
	for id := range e.plan.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := e.plan.Nodes[id]
		if node.Kind != NodeActivity && node.Kind != NodeSubflow {
			continue
		}
		rt := execution.Nodes[id]
		if rt != nil && rt.Status == NodeWaiting {
			waiting = append(waiting, id)
			continue
		}
		if !isReady(e.plan, execution, node, rt, now) {
			continue
		}
		if execution.BudgetLimit.MaxCost > 0 && execution.BudgetUsage.Cost+reservedEstimatedCost(e.plan, execution)+node.EstimatedCost > execution.BudgetLimit.MaxCost {
			waiting = append(waiting, id)
			continue
		}
		if needsApproval(e.runtime, node, execution) {
			_, err := e.mutate(ctx, execution.ID, func(x *Execution) error {
				xrt := x.Nodes[id]
				if xrt == nil || xrt.Status != NodePending {
					return nil
				}
				xrt.Status = NodeWaiting
				x.Status = StatusHuman
				x.WaitingFor[id] = "Approve:" + id
				appendHistory(x, "approval_required", id, "risk policy requires approval", map[string]any{"risk": node.Risk.String()})
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
			waiting = append(waiting, id)
			continue
		}

		activity := node.Activity
		if node.Capability != "" {
			remaining := 0.0
			if execution.BudgetLimit.MaxCost > 0 {
				remaining = execution.BudgetLimit.MaxCost - execution.BudgetUsage.Cost - reservedEstimatedCost(e.plan, execution)
			}
			provider, err := e.router.Resolve(ctx, node.Capability, ProviderPolicy{
				MaxCost:             remaining,
				MaxRisk:             maxRiskPolicy(node.Risk),
				RequiredPermissions: node.RequiredPermissions,
				AllowFallback:       true,
			})
			if err != nil {
				return nil, nil, err
			}
			activity = provider.Activity
		}
		if activity == "" {
			return nil, nil, fmt.Errorf("adgo: no activity resolved for node %s", id)
		}
		if until := execution.ThrottleUntil[activity]; !until.IsZero() && until.After(now) {
			waiting = append(waiting, id)
			continue
		}
		out = append(out, Candidate{Node: node, Activity: activity})
	}
	return out, waiting, nil
}

func (e *Engine) filterAgainstActive(execution *Execution, candidates []Candidate) []Candidate {
	activityCount := map[string]int{}
	capabilityCount := map[string]int{}
	resources := map[string]struct{}{}
	for _, task := range execution.ActiveTasks {
		if task.Status != TaskPending && task.Status != TaskRunning {
			continue
		}
		activityCount[task.Activity]++
		if node, ok := e.plan.Nodes[task.NodeID]; ok {
			if node.Capability != "" {
				capabilityCount[node.Capability]++
			}
			for _, key := range node.ResourceKeys {
				resources[key] = struct{}{}
			}
		}
	}
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if max := e.plan.ActivityLimits[candidate.Activity]; max > 0 && activityCount[candidate.Activity] >= max {
			continue
		}
		if candidate.Node.Capability != "" {
			if max := e.plan.CapabilityLimits[candidate.Node.Capability]; max > 0 && capabilityCount[candidate.Node.Capability] >= max {
				continue
			}
		}
		conflict := false
		for _, key := range candidate.Node.ResourceKeys {
			if _, ok := resources[key]; ok {
				conflict = true
				break
			}
		}
		if !conflict {
			out = append(out, candidate)
		}
	}
	return out
}

func (e *Engine) remainingConcurrency(execution *Execution) int {
	if e.plan.GlobalConcurrency <= 0 {
		return len(e.plan.Nodes)
	}
	active := 0
	for _, task := range execution.ActiveTasks {
		if task.Status == TaskPending || task.Status == TaskRunning {
			active++
		}
	}
	remaining := e.plan.GlobalConcurrency - active
	if remaining < 0 {
		return 0
	}
	return remaining
}

var errAdvanceStale = errors.New("adgo: advance state changed concurrently")

func (e *Engine) enqueue(ctx context.Context, executionID string, selected []Candidate) ([]string, error) {
	now := e.now()
	queued := make([]string, 0, len(selected))
	_, err := e.mutate(ctx, executionID, func(x *Execution) error {
		for _, candidate := range selected {
			node := candidate.Node
			rt := x.Nodes[node.ID]
			if !isReady(e.plan, x, node, rt, now) {
				return errAdvanceStale
			}
		}
		for _, candidate := range selected {
			node := candidate.Node
			rt := x.Nodes[node.ID]
			attempt := rt.Attempts + 1
			key := renderIdempotency(node.IdempotencyKey, x, node, attempt)
			id := taskID(x.ID, node.ID, attempt)
			task := TaskRuntime{ID: id, NodeID: node.ID, Activity: candidate.Activity, IdempotencyKey: key, Attempt: attempt, Status: TaskPending}
			rt.Status = NodeRunning
			rt.StartedAt = now
			if rt.FirstAttemptAt.IsZero() {
				rt.FirstAttemptAt = now
			}
			rt.Attempts = attempt
			x.ActiveTasks[id] = task
			x.Status = StatusRunning
			queued = append(queued, id)
			appendHistory(x, "activity_enqueued", node.ID, "activity durably queued", map[string]any{"activity": candidate.Activity, "attempt": attempt, "task": id, "idempotencyKey": key})
		}
		return nil
	})
	return queued, err
}

// WorkerSpec declares what a worker process may execute. Multiple Engine
// processes can share the same durable store; fencing guarantees that only the
// current lease holder may commit a task result.
type WorkerSpec struct {
	ID           string
	Activities   []string
	Concurrency  int
	LeaseTTL     time.Duration
	PollInterval time.Duration
	MaxRisk      *RiskLevel
}

type WorkToken struct {
	ExecutionID string `json:"executionId"`
	TaskID      string `json:"taskId"`
	WorkerID    string `json:"workerId"`
	Attempt     int    `json:"attempt"`
}

type WorkItem struct {
	Token      WorkToken       `json:"token"`
	Node       Node            `json:"node"`
	Activity   string          `json:"activity"`
	Provider   string          `json:"provider,omitempty"`
	Request    ActivityRequest `json:"-"`
	LeaseUntil time.Time       `json:"leaseUntil"`
	EnqueuedAt time.Time       `json:"enqueuedAt,omitempty"`
	Score      float64         `json:"score"`
}

func (e *Engine) Poll(ctx context.Context, spec WorkerSpec) (*WorkItem, error) {
	spec = e.normalizeWorker(spec)
	catalog, ok := e.store.(ExecutionCatalog)
	if !ok {
		return nil, fmt.Errorf("adgo: production polling requires ExecutionCatalog")
	}
	ids, err := catalog.ListExecutionIDs(ctx)
	if err != nil {
		return nil, err
	}
	type pending struct {
		execution *Execution
		task      TaskRuntime
		node      Node
		score     float64
	}
	list := make([]pending, 0)
	now := e.now()
	for _, id := range ids {
		execution, err := e.store.Load(ctx, id)
		if err != nil {
			if errors.Is(err, ErrExecutionNotFound) {
				continue
			}
			return nil, err
		}
		if execution.PlanID != e.plan.ID || execution.PlanDigest != e.plan.Digest || terminal(execution.Status) || executionPaused(execution) {
			continue
		}
		for _, task := range execution.ActiveTasks {
			if task.Status != TaskPending {
				continue
			}
			node, ok := e.plan.Nodes[task.NodeID]
			if !ok || !workerAccepts(spec, node, task.Activity) {
				continue
			}
			age := now.Sub(execution.CreatedAt).Minutes()
			score := node.CriticalPathWeight + 3*node.ExpectedQualityGain - node.EstimatedCost - 0.05*node.EstimatedLatency.Seconds() + 0.001*age
			list = append(list, pending{execution: execution, task: task, node: node, score: score})
		}
	}
	if len(list) == 0 {
		return nil, ErrNoWork
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score == list[j].score {
			if list[i].execution.CreatedAt.Equal(list[j].execution.CreatedAt) {
				if list[i].execution.ID == list[j].execution.ID {
					return list[i].task.ID < list[j].task.ID
				}
				return list[i].execution.ID < list[j].execution.ID
			}
			return list[i].execution.CreatedAt.Before(list[j].execution.CreatedAt)
		}
		return list[i].score > list[j].score
	})
	for _, candidate := range list {
		item, err := e.claim(ctx, candidate.execution.ID, candidate.task.ID, spec, candidate.score)
		if err == nil {
			return item, nil
		}
		if errors.Is(err, ErrStaleTask) || errors.Is(err, ErrConflict) {
			continue
		}
		return nil, err
	}
	return nil, ErrNoWork
}

func (e *Engine) claim(ctx context.Context, executionID, taskID string, spec WorkerSpec, score float64) (*WorkItem, error) {
	now := e.now()
	var claimed TaskRuntime
	var node Node
	result, err := e.mutate(ctx, executionID, func(x *Execution) error {
		task, ok := x.ActiveTasks[taskID]
		if !ok || task.Status != TaskPending {
			return ErrStaleTask
		}
		var exists bool
		node, exists = e.plan.Nodes[task.NodeID]
		if !exists || !workerAccepts(spec, node, task.Activity) {
			return ErrStaleTask
		}
		task.Status = TaskRunning
		task.WorkerID = spec.ID
		task.StartedAt = now
		task.LeaseUntil = now.Add(spec.LeaseTTL)
		x.ActiveTasks[taskID] = task
		x.Status = StatusRunning
		claimed = task
		appendHistory(x, "task_claimed", task.NodeID, "worker claimed durable task", map[string]any{"task": task.ID, "worker": spec.ID, "leaseUntil": task.LeaseUntil})
		return nil
	})
	if err != nil {
		return nil, err
	}
	deadline := time.Time{}
	if node.Timeout > 0 {
		deadline = now.Add(node.Timeout)
	}
	provider := ""
	if node.Capability != "" {
		if p, ok := e.router.ProviderForActivity(node.Capability, claimed.Activity); ok {
			provider = p.Name
		}
	}
	return &WorkItem{
		Token:      WorkToken{ExecutionID: executionID, TaskID: claimed.ID, WorkerID: spec.ID, Attempt: claimed.Attempt},
		Node:       node,
		Activity:   claimed.Activity,
		Provider:   provider,
		Request:    ActivityRequest{ExecutionID: executionID, NodeID: node.ID, Attempt: claimed.Attempt, IdempotencyKey: claimed.IdempotencyKey, Data: cloneRawMap(result.Data), Artifacts: cloneArtifactMap(result.Artifacts), Deadline: deadline},
		LeaseUntil: claimed.LeaseUntil,
		EnqueuedAt: result.Nodes[node.ID].StartedAt,
		Score:      score,
	}, nil
}

func (e *Engine) Heartbeat(ctx context.Context, token WorkToken, details map[string]any) error {
	now := e.now()
	_, err := e.mutate(ctx, token.ExecutionID, func(x *Execution) error {
		task, err := validateClaim(x, token, now)
		if err != nil {
			return err
		}
		task.LeaseUntil = now.Add(e.leaseTTL)
		x.ActiveTasks[token.TaskID] = task
		if len(details) > 0 {
			appendHistory(x, "activity_heartbeat", task.NodeID, "worker heartbeat", map[string]any{"task": task.ID, "worker": token.WorkerID, "details": details})
		}
		return nil
	})
	return err
}

func (e *Engine) Complete(ctx context.Context, token WorkToken, result ActivityResult, duration time.Duration) (*Execution, error) {
	now := e.now()
	var task TaskRuntime
	var node Node
	next, err := e.mutate(ctx, token.ExecutionID, func(x *Execution) error {
		var claimErr error
		task, claimErr = validateClaim(x, token, now)
		if claimErr != nil {
			return claimErr
		}
		var ok bool
		node, ok = e.plan.Nodes[task.NodeID]
		if !ok {
			return ErrStaleTask
		}
		rt := x.Nodes[node.ID]
		if rt == nil || rt.Status != NodeRunning {
			return ErrStaleTask
		}
		rt.Status = NodeCompleted
		rt.Outcome = result.Outcome
		if rt.Outcome == "" {
			rt.Outcome = OutcomeCompleted
		}
		rt.CompletedAt = now
		rt.LastError = ""
		rt.LastFailure = ""
		rt.Signature = result.Signature
		delete(x.ActiveTasks, task.ID)
		for key, value := range result.Facts {
			if err := SetData(x, key, value); err != nil {
				return err
			}
		}
		for key, value := range result.Artifacts {
			x.Artifacts[key] = value
		}
		before := QualityUtility(x.Quality)
		mergeQuality(x, result.Quality)
		after := QualityUtility(x.Quality)
		if after > before {
			x.Metrics.QualityGain += after - before
		}
		if err := addBudget(&x.BudgetUsage, result.Budget); err != nil {
			return err
		}
		x.Metrics.Cost = x.BudgetUsage.Cost
		x.Metrics.Tokens = x.BudgetUsage.Tokens
		x.Metrics.Activities++
		x.Metrics.ActiveComputeTime += duration
		if !task.StartedAt.IsZero() && !rt.StartedAt.IsZero() && task.StartedAt.After(rt.StartedAt) {
			x.Metrics.QueueTime += task.StartedAt.Sub(rt.StartedAt)
		}
		if result.Signature != "" {
			x.Signatures = append(x.Signatures, result.Signature)
			if len(x.Signatures) > 12 {
				x.Signatures = x.Signatures[len(x.Signatures)-12:]
			}
			if DetectOscillation(x.Signatures) {
				x.StrategyBans[node.ID] = true
				appendHistory(x, "oscillation_detected", node.ID, "repeating semantic signature detected", nil)
			}
		}
		if node.Compensation != "" {
			x.CompensationStack = append(x.CompensationStack, CompensationEntry{NodeID: node.ID, Activity: node.Compensation, IdempotencyKey: "comp:" + task.IdempotencyKey})
		}
		recordQuality(x, node.ID)
		activateNext(e.plan, x, node.ID, rt.Outcome)
		x.Status = StatusRunning
		appendHistory(x, "activity_completed", node.ID, "worker activity completed", map[string]any{"activity": task.Activity, "attempt": task.Attempt, "task": task.ID, "worker": token.WorkerID})
		return nil
	})
	if node.Capability != "" && task.Activity != "" {
		provider := ""
		if p, ok := e.router.ProviderForActivity(node.Capability, task.Activity); ok {
			provider = p.Name
		}
		e.router.Report(node.Capability, provider, duration, &result, err)
	}
	return next, err
}

func (e *Engine) Fail(ctx context.Context, token WorkToken, activityErr error, duration time.Duration) (*Execution, error) {
	if activityErr == nil {
		activityErr = errors.New("activity failed")
	}
	now := e.now()
	cur, err := e.store.Load(ctx, token.ExecutionID)
	if err != nil {
		return nil, err
	}
	task, err := validateClaim(cur, token, now)
	if err != nil {
		return nil, err
	}
	node, ok := e.plan.Nodes[task.NodeID]
	if !ok {
		return nil, ErrStaleTask
	}
	class := e.runtime.classify(activityErr)
	if errors.Is(activityErr, context.DeadlineExceeded) {
		class = FailureTransient
	}
	var failure *FailureError
	retryAfter := time.Duration(0)
	if errors.As(activityErr, &failure) {
		retryAfter = failure.RetryAfter
	}
	policy := normalizeRetry(node.Retry)
	canRetry := (class == FailureTransient || class == FailureRateLimit) && task.Attempt < policy.MaxAttempts
	if canRetry {
		delay := retryAfter
		if delay <= 0 {
			delay = backoff(policy, task.Attempt, cur.ID+node.ID)
		}
		rt := cur.Nodes[node.ID]
		if rt != nil && !rt.FirstAttemptAt.IsZero() && policy.MaxRetryDuration > 0 && now.Sub(rt.FirstAttemptAt)+delay > policy.MaxRetryDuration {
			canRetry = false
		}
		if canRetry {
			next, commitErr := e.mutate(ctx, token.ExecutionID, func(x *Execution) error {
				claimed, claimErr := validateClaim(x, token, now)
				if claimErr != nil {
					return claimErr
				}
				rt := x.Nodes[node.ID]
				rt.Status = NodePending
				rt.NotBefore = now.Add(delay)
				rt.LastError = activityErr.Error()
				rt.LastFailure = class
				delete(x.ActiveTasks, claimed.ID)
				x.Metrics.Retries++
				x.Metrics.ActiveComputeTime += duration
				if errors.Is(activityErr, context.DeadlineExceeded) {
					x.Metrics.Timeouts++
				}
				if class == FailureRateLimit {
					x.ThrottleUntil[claimed.Activity] = now.Add(delay)
				}
				x.Status = StatusRunning
				appendHistory(x, "activity_retry", node.ID, "worker retry scheduled", map[string]any{"class": class, "after": delay.String(), "attempt": claimed.Attempt, "task": claimed.ID})
				return nil
			})
			e.reportProviderFailure(node, task.Activity, duration, activityErr)
			return next, commitErr
		}
	}

	if class == FailureQuality || class == FailureInvalidInput {
		next, commitErr := e.mutate(ctx, token.ExecutionID, func(x *Execution) error {
			claimed, claimErr := validateClaim(x, token, now)
			if claimErr != nil {
				return claimErr
			}
			delete(x.ActiveTasks, claimed.ID)
			x.Nodes[node.ID].LastError = activityErr.Error()
			x.Nodes[node.ID].LastFailure = class
			violation := Violation{Code: string(class), Message: activityErr.Error(), RepairFrom: []string{node.ID}}
			repair, planErr := e.runtime.repair.PlanRepair(e.plan, x, node.ID, []Violation{violation})
			if planErr != nil {
				repair = RepairPlan{GateNode: node.ID, Roots: []string{node.ID}, AffectedNodes: []string{node.ID}, Reason: "activity-local repair", ExpectedCost: node.EstimatedCost, Risk: node.Risk}
			}
			if applyErr := ApplyRepairWithClock(e.plan, x, repair, e.now()); applyErr != nil {
				x.Status = StatusHuman
				x.Nodes[node.ID].Status = NodeWaiting
				x.WaitingFor[node.ID] = "HumanRepairDecision"
				appendHistory(x, "repair_escalated", node.ID, applyErr.Error(), nil)
			}
			return nil
		})
		e.reportProviderFailure(node, task.Activity, duration, activityErr)
		return next, commitErr
	}

	if class == FailureAmbiguousSideEffect {
		next, commitErr := e.mutate(ctx, token.ExecutionID, func(x *Execution) error {
			claimed, claimErr := validateClaim(x, token, now)
			if claimErr != nil {
				return claimErr
			}
			delete(x.ActiveTasks, claimed.ID)
			x.Nodes[node.ID].Status = NodeWaiting
			x.Nodes[node.ID].LastError = activityErr.Error()
			x.Nodes[node.ID].LastFailure = class
			x.Status = StatusHuman
			x.WaitingFor[node.ID] = "Reconcile:" + node.ID
			appendHistory(x, "ambiguous_side_effect", node.ID, "operator/provider reconciliation required", map[string]any{"idempotencyKey": claimed.IdempotencyKey})
			return nil
		})
		e.reportProviderFailure(node, task.Activity, duration, activityErr)
		return next, commitErr
	}

	failed, commitErr := e.mutate(ctx, token.ExecutionID, func(x *Execution) error {
		claimed, claimErr := validateClaim(x, token, now)
		if claimErr != nil {
			return claimErr
		}
		delete(x.ActiveTasks, claimed.ID)
		x.Nodes[node.ID].Status = NodeFailed
		x.Nodes[node.ID].LastError = activityErr.Error()
		x.Nodes[node.ID].LastFailure = class
		x.Status = StatusFailed
		x.Failure = activityErr.Error()
		x.Metrics.ActiveComputeTime += duration
		appendHistory(x, "activity_failed", node.ID, activityErr.Error(), map[string]any{"class": class, "attempt": claimed.Attempt, "task": claimed.ID})
		return nil
	})
	e.reportProviderFailure(node, task.Activity, duration, activityErr)
	if commitErr != nil {
		return nil, commitErr
	}
	if len(failed.CompensationStack) > 0 {
		if err := e.runtime.compensate(ctx, failed, StatusFailed, failed.Failure); err != nil {
			return nil, err
		}
		return e.store.Load(ctx, token.ExecutionID)
	}
	return failed, nil
}

func (e *Engine) reportProviderFailure(node Node, activity string, duration time.Duration, err error) {
	if node.Capability == "" {
		return
	}
	provider := ""
	if p, ok := e.router.ProviderForActivity(node.Capability, activity); ok {
		provider = p.Name
	}
	e.router.Report(node.Capability, provider, duration, nil, err)
}

func validateClaim(execution *Execution, token WorkToken, now time.Time) (TaskRuntime, error) {
	task, ok := execution.ActiveTasks[token.TaskID]
	if !ok || task.Status != TaskRunning || task.WorkerID != token.WorkerID || task.Attempt != token.Attempt {
		return TaskRuntime{}, ErrStaleTask
	}
	if !task.LeaseUntil.IsZero() && !task.LeaseUntil.After(now) {
		return TaskRuntime{}, ErrStaleTask
	}
	return task, nil
}

func (e *Engine) normalizeWorker(spec WorkerSpec) WorkerSpec {
	if strings.TrimSpace(spec.ID) == "" {
		spec.ID = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}
	if spec.Concurrency <= 0 {
		spec.Concurrency = 1
	}
	if spec.LeaseTTL <= 0 {
		spec.LeaseTTL = e.leaseTTL
	}
	if spec.PollInterval <= 0 {
		spec.PollInterval = e.pollInterval
	}
	return spec
}

func workerAccepts(spec WorkerSpec, node Node, activity string) bool {
	if spec.MaxRisk != nil && node.Risk > *spec.MaxRisk {
		return false
	}
	if len(spec.Activities) == 0 {
		return true
	}
	for _, allowed := range spec.Activities {
		if allowed == activity {
			return true
		}
	}
	return false
}

type heartbeatContextKey struct{}
type heartbeatFunc func(map[string]any) error

// ActivityHeartbeat allows a long-running ActivityHandler to publish explicit
// lease heartbeats/progress without depending on Engine internals.
func ActivityHeartbeat(ctx context.Context, details map[string]any) error {
	fn, _ := ctx.Value(heartbeatContextKey{}).(heartbeatFunc)
	if fn == nil {
		return errors.New("adgo: activity heartbeat is unavailable in this context")
	}
	return fn(details)
}

// RunWorker polls durable tasks and executes registered handlers. It uses an
// automatic lease heartbeat in addition to explicit ActivityHeartbeat calls.
func (e *Engine) RunWorker(ctx context.Context, spec WorkerSpec) error {
	spec = e.normalizeWorker(spec)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, spec.Concurrency)
	var wg sync.WaitGroup
	for i := 0; i < spec.Concurrency; i++ {
		wg.Add(1)
		local := spec
		if spec.Concurrency > 1 {
			local.ID = fmt.Sprintf("%s/%d", spec.ID, i+1)
		}
		go func() {
			defer wg.Done()
			errCh <- e.workerLoop(workerCtx, local)
		}()
	}
	go func() {
		wg.Wait()
		close(errCh)
	}()
	for err := range errCh {
		if err != nil && !errors.Is(err, context.Canceled) {
			cancel()
			return err
		}
	}
	return ctx.Err()
}

func (e *Engine) workerLoop(ctx context.Context, spec WorkerSpec) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := e.Poll(ctx, spec)
		if errors.Is(err, ErrNoWork) {
			if err := sleepContext(ctx, spec.PollInterval); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := e.executeWorkItem(ctx, spec, item); err != nil && !errors.Is(err, ErrStaleTask) {
			return err
		}
		_, err = e.Advance(ctx, item.Token.ExecutionID)
		if err != nil && !errors.Is(err, ErrDeadlock) {
			return err
		}
	}
}

func (e *Engine) executeWorkItem(ctx context.Context, spec WorkerSpec, item *WorkItem) error {
	started := time.Now()
	callCtx := ctx
	cancel := func() {}
	if !item.Request.Deadline.IsZero() {
		callCtx, cancel = context.WithDeadline(ctx, item.Request.Deadline)
	}
	defer cancel()
	callCtx = context.WithValue(callCtx, heartbeatContextKey{}, heartbeatFunc(func(details map[string]any) error {
		return e.Heartbeat(callCtx, item.Token, details)
	}))

	heartbeatEvery := spec.LeaseTTL / 3
	if heartbeatEvery <= 0 {
		heartbeatEvery = time.Second
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(callCtx)
	var heartbeatWG sync.WaitGroup
	heartbeatWG.Add(1)
	go func() {
		defer heartbeatWG.Done()
		ticker := time.NewTicker(heartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := e.Heartbeat(heartbeatCtx, item.Token, nil); err != nil {
					return
				}
			}
		}
	}()

	var result ActivityResult
	var err error
	if item.Node.Kind == NodeSubflow {
		handler, ok := e.registry.subflow(item.Activity)
		if !ok {
			err = fmt.Errorf("adgo: subflow handler %q is not registered", item.Activity)
		} else {
			result, err = handler(callCtx, item.Request)
		}
	} else {
		handler, ok := e.registry.activity(item.Activity)
		if !ok {
			err = fmt.Errorf("adgo: activity handler %q is not registered", item.Activity)
		} else {
			result, err = handler(callCtx, item.Request)
		}
	}
	stopHeartbeat()
	heartbeatWG.Wait()
	duration := time.Since(started)
	if err != nil {
		_, failErr := e.Fail(ctx, item.Token, err, duration)
		return failErr
	}
	_, err = e.Complete(ctx, item.Token, result, duration)
	return err
}

// RunCoordinator continuously advances all executions pinned to this Engine's
// plan. Waiting workflows consume no worker goroutine; they are reconsidered on
// durable signals, timer expiry or retry deadlines.
func (e *Engine) RunCoordinator(ctx context.Context) error {
	catalog, ok := e.store.(ExecutionCatalog)
	if !ok {
		return fmt.Errorf("adgo: coordinator requires ExecutionCatalog")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := catalog.ListExecutionIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			execution, err := e.store.Load(ctx, id)
			if err != nil {
				if errors.Is(err, ErrExecutionNotFound) {
					continue
				}
				return err
			}
			if execution.PlanID != e.plan.ID || execution.PlanDigest != e.plan.Digest || terminal(execution.Status) {
				continue
			}
			if _, err := e.Advance(ctx, id); err != nil && !errors.Is(err, ErrDeadlock) {
				return err
			}
		}
		if err := sleepContext(ctx, e.coordinatorInterval); err != nil {
			return err
		}
	}
}

// Serve runs one coordinator and any number of worker pools in the same process.
// The same store may also be shared by additional Engine processes, enabling
// horizontal worker scaling without changing workflow definitions.
func (e *Engine) Serve(ctx context.Context, workers ...WorkerSpec) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(workers)+1)
	go func() { errCh <- e.RunCoordinator(serveCtx) }()
	for _, spec := range workers {
		spec := spec
		go func() { errCh <- e.RunWorker(serveCtx, spec) }()
	}
	err := <-errCh
	cancel()
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (e *Engine) verifyPinned(execution *Execution) error {
	if execution.PlanID != e.plan.ID || execution.PlanDigest != e.plan.Digest {
		return fmt.Errorf("adgo: pinned plan mismatch for execution %s", execution.ID)
	}
	return nil
}

func (e *Engine) mutate(ctx context.Context, executionID string, mutate func(*Execution) error) (*Execution, error) {
	for attempt := 0; attempt < 8; attempt++ {
		cur, err := e.store.Load(ctx, executionID)
		if err != nil {
			return nil, err
		}
		next, err := e.store.Commit(ctx, executionID, cur.Version, mutate)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return next, err
	}
	return nil, ErrConflict
}

func (e *Engine) lockExecution(id string) func() {
	e.locksMu.Lock()
	lock := e.locks[id]
	if lock == nil {
		lock = &sync.Mutex{}
		e.locks[id] = lock
	}
	e.locksMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (e *Engine) enforceRecoveryLimit(ctx context.Context, execution *Execution) error {
	if e.maxLeaseRecoveries <= 0 {
		return nil
	}
	counts := map[string]int{}
	for _, entry := range execution.History {
		if entry.Type == "lease_recovered" && entry.NodeID != "" {
			counts[entry.NodeID]++
		}
	}
	for nodeID, count := range counts {
		if count <= e.maxLeaseRecoveries {
			continue
		}
		_, err := e.mutate(ctx, execution.ID, func(x *Execution) error {
			rt := x.Nodes[nodeID]
			if rt == nil {
				return nil
			}
			rt.Status = NodeWaiting
			x.Status = StatusHuman
			x.WaitingFor[nodeID] = "OperatorRecoveryDecision"
			appendHistory(x, "recovery_quarantined", nodeID, "lease recovery threshold exceeded", map[string]any{"recoveries": count, "limit": e.maxLeaseRecoveries})
			return nil
		})
		return err
	}
	return nil
}

func executionPaused(execution *Execution) bool {
	return execution != nil && execution.WaitingFor["$execution"] == "ResumeRequested"
}

func waitingNodes(execution *Execution) []string {
	out := make([]string, 0, len(execution.WaitingFor))
	for id := range execution.WaitingFor {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func activeTaskIDs(execution *Execution) []string {
	out := make([]string, 0, len(execution.ActiveTasks))
	for id := range execution.ActiveTasks {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func reservedEstimatedCost(plan *Plan, execution *Execution) float64 {
	reserved := 0.0
	for _, task := range execution.ActiveTasks {
		if task.Status != TaskPending && task.Status != TaskRunning {
			continue
		}
		if node, ok := plan.Nodes[task.NodeID]; ok {
			reserved += node.EstimatedCost
		}
	}
	return reserved
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		duration = time.Millisecond
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
