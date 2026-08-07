package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

type RuntimeOption func(*Runtime)

func WithScheduler(s Scheduler) RuntimeOption {
	return func(r *Runtime) {
		if s != nil {
			r.scheduler = s
		}
	}
}
func WithRepairPlanner(p RepairPlanner) RuntimeOption {
	return func(r *Runtime) {
		if p != nil {
			r.repair = p
		}
	}
}
func WithLeaseTTL(ttl time.Duration) RuntimeOption {
	return func(r *Runtime) {
		if ttl > 0 {
			r.leaseTTL = ttl
		}
	}
}
func WithWorkerID(id string) RuntimeOption {
	return func(r *Runtime) {
		if strings.TrimSpace(id) != "" {
			r.workerID = id
		}
	}
}
func WithApprovalThreshold(risk RiskLevel) RuntimeOption {
	return func(r *Runtime) { r.approvalThreshold = risk }
}
func WithFailureClassifier(fn func(error) FailureClass) RuntimeOption {
	return func(r *Runtime) {
		if fn != nil {
			r.classify = fn
		}
	}
}

type scheduledActivity struct {
	c    Candidate
	task TaskRuntime
	req  ActivityRequest
}
type activityExecResult struct {
	s        scheduledActivity
	res      ActivityResult
	err      error
	duration time.Duration
}

type Runtime struct {
	plan              *Plan
	store             Store
	registry          *Registry
	scheduler         Scheduler
	repair            RepairPlanner
	leaseTTL          time.Duration
	workerID          string
	approvalThreshold RiskLevel
	classify          func(error) FailureClass
	mu                sync.Mutex
}

type StepResult struct {
	ExecutionID string
	Status      ExecutionStatus
	Progressed  bool
	Executed    []string
	Waiting     []string
}

func NewRuntime(plan *Plan, store Store, registry *Registry, opts ...RuntimeOption) (*Runtime, error) {
	if plan == nil {
		return nil, fmt.Errorf("adgo: plan is required")
	}
	if store == nil {
		return nil, fmt.Errorf("adgo: store is required")
	}
	if registry == nil {
		registry = NewRegistry()
	}
	r := &Runtime{plan: plan, store: store, registry: registry, scheduler: DefaultScheduler(), repair: DependencyRepairPlanner{}, leaseTTL: 30 * time.Second, workerID: fmt.Sprintf("worker-%d", time.Now().UnixNano()), approvalThreshold: RiskHigh, classify: DefaultClassify}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

func (r *Runtime) Start(ctx context.Context, id string, initial map[string]any, budget BudgetLimit) (*Execution, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("adgo: execution id is required")
	}
	now := time.Now().UTC()
	e := &Execution{ID: id, PlanID: r.plan.ID, PlanVersion: r.plan.Version, PlanDigest: r.plan.Digest, Version: 1, Status: StatusRunning, Nodes: map[string]*NodeRuntime{}, Data: map[string]json.RawMessage{}, Artifacts: map[string]ArtifactRef{}, Quality: QualityVector{}, BudgetLimit: budget, ActiveTasks: map[string]TaskRuntime{}, SeenEvents: map[string]bool{}, RevisionCounters: map[string]int{}, StrategyBans: map[string]bool{}, WaitingFor: map[string]string{}, ThrottleUntil: map[string]time.Time{}, CreatedAt: now, UpdatedAt: now}
	transitionIncoming := map[string]bool{}
	for _, n := range r.plan.Nodes {
		for _, tr := range n.Next {
			transitionIncoming[tr.To] = true
		}
	}
	for id, n := range r.plan.Nodes {
		activated := !transitionIncoming[id]
		status := NodeDormant
		if activated {
			status = NodePending
		}
		e.Nodes[id] = &NodeRuntime{Status: status, Activated: activated}
		_ = n
	}
	for k, v := range initial {
		if err := SetData(e, k, v); err != nil {
			return nil, err
		}
	}
	appendHistory(e, "execution_started", "", "execution created", map[string]any{"planDigest": r.plan.Digest, "planVersion": r.plan.Version})
	if err := r.store.Create(ctx, e); err != nil {
		return nil, err
	}
	return r.store.Load(ctx, id)
}

func (r *Runtime) Signal(ctx context.Context, executionID string, event Event) error {
	if event.ID == "" {
		event.ID = eventID(event)
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	return r.store.PutInbox(ctx, executionID, event)
}

func (r *Runtime) Run(ctx context.Context, executionID string) (*Execution, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := r.Step(ctx, executionID)
		if err != nil {
			return nil, err
		}
		e, err := r.store.Load(ctx, executionID)
		if err != nil {
			return nil, err
		}
		switch e.Status {
		case StatusCompleted, StatusFailed, StatusCanceled, StatusDeadlocked:
			return e, nil
		}
		if !result.Progressed {
			return e, nil
		}
	}
}

func (r *Runtime) Step(ctx context.Context, executionID string) (StepResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.store.Load(ctx, executionID)
	if err != nil {
		return StepResult{}, err
	}
	if e.PlanDigest != r.plan.Digest || e.PlanID != r.plan.ID {
		return StepResult{}, fmt.Errorf("adgo: pinned plan mismatch for execution %s", executionID)
	}
	if terminal(e.Status) {
		return StepResult{ExecutionID: executionID, Status: e.Status}, nil
	}
	progressed := false

	if p, err := r.ingest(ctx, e); err != nil {
		return StepResult{}, err
	} else if p {
		progressed = true
		e, _ = r.store.Load(ctx, executionID)
	}
	if p, err := r.recoverExpired(ctx, e); err != nil {
		return StepResult{}, err
	} else if p {
		progressed = true
		e, _ = r.store.Load(ctx, executionID)
	}
	if e.CancelRequested {
		if err := r.compensate(ctx, e, StatusCanceled, "cancellation requested"); err != nil {
			return StepResult{}, err
		}
		e, _ = r.store.Load(ctx, executionID)
		return StepResult{ExecutionID: executionID, Status: e.Status, Progressed: true}, nil
	}
	if err := checkBudget(e, time.Now().UTC()); err != nil {
		if err := r.compensate(ctx, e, StatusFailed, err.Error()); err != nil {
			return StepResult{}, err
		}
		e, _ = r.store.Load(ctx, executionID)
		return StepResult{ExecutionID: executionID, Status: e.Status, Progressed: true}, nil
	}

	internalProgress, err := r.runInternal(ctx, e)
	if err != nil {
		return StepResult{}, err
	}
	if internalProgress {
		progressed = true
		e, _ = r.store.Load(ctx, executionID)
	}
	if terminal(e.Status) {
		return StepResult{ExecutionID: executionID, Status: e.Status, Progressed: progressed}, nil
	}

	candidates, waiting, err := r.readyCandidates(ctx, e)
	if err != nil {
		return StepResult{}, err
	}
	e, _ = r.store.Load(ctx, executionID)
	selected := r.scheduler.Select(r.plan, e, candidates)
	if len(selected) > 0 {
		executed, err := r.executeCandidates(ctx, e, selected)
		if err != nil {
			return StepResult{}, err
		}
		progressed = true
		e, _ = r.store.Load(ctx, executionID)
		if terminal(e.Status) {
			return StepResult{ExecutionID: executionID, Status: e.Status, Progressed: true, Executed: executed}, nil
		}
		return StepResult{ExecutionID: executionID, Status: e.Status, Progressed: true, Executed: executed, Waiting: waiting}, nil
	}

	if goalsSatisfied(r.plan, e) {
		_, err = r.commit(ctx, e, func(x *Execution) error {
			x.Status = StatusCompleted
			x.Metrics.WallTime = time.Since(x.CreatedAt)
			appendHistory(x, "execution_completed", "", "all activated goals completed", nil)
			return nil
		})
		if err != nil {
			return StepResult{}, err
		}
		return StepResult{ExecutionID: executionID, Status: StatusCompleted, Progressed: true}, nil
	}
	if len(waiting) > 0 || hasPendingTime(e) {
		if e.Status != StatusWaiting && e.Status != StatusHuman {
			_, err = r.commit(ctx, e, func(x *Execution) error {
				x.Status = StatusWaiting
				appendHistory(x, "execution_waiting", "", "waiting for timer or external event", map[string]any{"nodes": waiting})
				return nil
			})
			if err != nil {
				return StepResult{}, err
			}
			progressed = true
		}
		e, _ = r.store.Load(ctx, executionID)
		return StepResult{ExecutionID: executionID, Status: e.Status, Progressed: progressed, Waiting: waiting}, nil
	}
	_, err = r.commit(ctx, e, func(x *Execution) error {
		x.Status = StatusDeadlocked
		x.Failure = deadlockReason(r.plan, x)
		appendHistory(x, "deadlock", "", x.Failure, nil)
		return nil
	})
	if err != nil {
		return StepResult{}, err
	}
	return StepResult{ExecutionID: executionID, Status: StatusDeadlocked, Progressed: true}, ErrDeadlock
}

func (r *Runtime) ingest(ctx context.Context, e *Execution) (bool, error) {
	events, err := r.store.ListInbox(ctx, e.ID)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return false, nil
	}
	ids := []string{}
	changed := false
	next, err := r.commit(ctx, e, func(x *Execution) error {
		for _, ev := range events {
			ids = append(ids, ev.ID)
			if x.SeenEvents[ev.ID] {
				continue
			}
			x.SeenEvents[ev.ID] = true
			changed = true
			appendHistory(x, "event_ingested", ev.TargetNode, ev.Type, map[string]any{"eventId": ev.ID})
			switch ev.Type {
			case "CancelRequested":
				x.CancelRequested = true
			case "BudgetChanged":
				var b BudgetLimit
				if len(ev.Payload) > 0 {
					if err := json.Unmarshal(ev.Payload, &b); err != nil {
						return fmt.Errorf("decode BudgetChanged: %w", err)
					}
					x.BudgetLimit = b
				}
			default:
				target := ev.TargetNode
				if target == "" {
					for nodeID, expected := range x.WaitingFor {
						if expected == ev.Type {
							target = nodeID
							break
						}
					}
				}
				if target != "" {
					rt := x.Nodes[target]
					if rt != nil && rt.Status == NodeWaiting {
						expected := x.WaitingFor[target]
						approvalEvent := "Approve:" + target
						if expected == approvalEvent && (ev.Type == approvalEvent || ev.Type == "Approved") {
							if err := SetData(x, "approval:"+target, true); err != nil {
								return err
							}
							rt.Status = NodePending
							rt.NotBefore = time.Time{}
							delete(x.WaitingFor, target)
							x.Metrics.HumanInterventions++
							appendHistory(x, "activity_approved", target, "human approved high-risk activity", map[string]any{"event": ev.Type})
						} else if expected == "Reconcile:"+target && (ev.Type == expected || ev.Type == "Reconciled") {
							rt.Status = NodePending
							rt.NotBefore = time.Time{}
							delete(x.WaitingFor, target)
							appendHistory(x, "side_effect_reconciled", target, "side effect reconciliation requested retry", map[string]any{"event": ev.Type})
						} else if expected == "" || expected == ev.Type {
							rt.Status = NodeCompleted
							rt.Outcome = OutcomeCompleted
							rt.CompletedAt = time.Now().UTC()
							delete(x.WaitingFor, target)
							if r.plan.Nodes[target].Kind == NodeHuman {
								x.Metrics.HumanInterventions++
							}
							activateNext(r.plan, x, target, OutcomeCompleted)
							appendHistory(x, "wait_resumed", target, "external event resumed node", map[string]any{"event": ev.Type})
						}
					}
				}
			}
		}
		if changed && (x.Status == StatusWaiting || x.Status == StatusHuman) {
			x.Status = StatusRunning
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if len(ids) > 0 {
		if err := r.store.AckInbox(ctx, e.ID, ids); err != nil {
			return false, err
		}
	}
	_ = next
	return changed, nil
}

func (r *Runtime) recoverExpired(ctx context.Context, e *Execution) (bool, error) {
	now := time.Now().UTC()
	expired := []string{}
	for id, t := range e.ActiveTasks {
		if t.Status == TaskRunning && !t.LeaseUntil.IsZero() && !t.LeaseUntil.After(now) {
			expired = append(expired, id)
		}
	}
	if len(expired) == 0 {
		return false, nil
	}
	_, err := r.commit(ctx, e, func(x *Execution) error {
		for _, id := range expired {
			t := x.ActiveTasks[id]
			rt := x.Nodes[t.NodeID]
			if rt != nil {
				rt.Status = NodePending
				rt.NotBefore = time.Time{}
				rt.LastError = "worker lease expired; recovered"
			}
			delete(x.ActiveTasks, id)
			x.Metrics.RecoveryEvents++
			appendHistory(x, "lease_recovered", t.NodeID, "expired activity lease returned to pending", map[string]any{"task": id})
		}
		return nil
	})
	return err == nil, err
}

func (r *Runtime) runInternal(ctx context.Context, e *Execution) (bool, error) {
	progressed := false
	if p, err := r.resumeDueTimers(ctx, e); err != nil {
		return false, err
	} else if p {
		progressed = true
	}
	for {
		cur, err := r.store.Load(ctx, e.ID)
		if err != nil {
			return progressed, err
		}
		ready := r.readyInternal(cur)
		if len(ready) == 0 {
			return progressed, nil
		}
		sort.Strings(ready)
		id := ready[0]
		n := r.plan.Nodes[id]
		rt := cur.Nodes[id]
		switch n.Kind {
		case NodeFork, NodeJoin:
			_, err = r.commit(ctx, cur, func(x *Execution) error { return completeNode(r.plan, x, id, OutcomeCompleted, ActivityResult{}) })
		case NodeDecision:
			h, ok := r.registry.decision(n.Activity)
			if !ok {
				return progressed, fmt.Errorf("adgo: decision handler %q is not registered", n.Activity)
			}
			out, callErr := h(ctx, snapshot(cur))
			if callErr != nil {
				return progressed, callErr
			}
			_, err = r.commit(ctx, cur, func(x *Execution) error { return completeNode(r.plan, x, id, out, ActivityResult{}) })
		case NodeGate:
			var gr GateResult
			if n.Activity != "" {
				h, ok := r.registry.gate(n.Activity)
				if !ok {
					return progressed, fmt.Errorf("adgo: gate handler %q is not registered", n.Activity)
				}
				gr, err = h(ctx, snapshot(cur))
				if err != nil {
					return progressed, err
				}
			} else {
				gr = genericGate(n, cur)
			}
			_, err = r.commit(ctx, cur, func(x *Execution) error {
				mergeQuality(x, gr.Quality)
				recordQuality(x, id)
				if gr.Outcome == OutcomeRepair {
					rp, err := r.repair.PlanRepair(r.plan, x, id, gr.Violations)
					if err != nil {
						return err
					}
					if err := ApplyRepair(r.plan, x, rp); err != nil {
						x.Status = StatusHuman
						x.WaitingFor[id] = "HumanRepairDecision"
						x.Nodes[id].Status = NodeWaiting
						appendHistory(x, "repair_escalated", id, err.Error(), nil)
						return nil
					}
					return nil
				}
				return completeNode(r.plan, x, id, gr.Outcome, ActivityResult{})
			})
		case NodeWait:
			if n.Wait.Duration > 0 {
				if rt.NotBefore.IsZero() {
					_, err = r.commit(ctx, cur, func(x *Execution) error {
						x.Nodes[id].Status = NodeWaiting
						x.Nodes[id].NotBefore = time.Now().UTC().Add(n.Wait.Duration)
						x.WaitingFor[id] = "timer"
						appendHistory(x, "timer_wait", id, "timer scheduled", map[string]any{"until": x.Nodes[id].NotBefore})
						return nil
					})
				} else if !rt.NotBefore.After(time.Now().UTC()) {
					_, err = r.commit(ctx, cur, func(x *Execution) error {
						delete(x.WaitingFor, id)
						return completeNode(r.plan, x, id, OutcomeCompleted, ActivityResult{})
					})
				} else {
					return progressed, nil
				}
			} else if n.Wait.EventType != "" {
				if rt.Status != NodeWaiting {
					_, err = r.commit(ctx, cur, func(x *Execution) error {
						x.Nodes[id].Status = NodeWaiting
						x.WaitingFor[id] = n.Wait.EventType
						appendHistory(x, "event_wait", id, "waiting for external event", map[string]any{"event": n.Wait.EventType})
						return nil
					})
				} else {
					return progressed, nil
				}
			}
		case NodeHuman:
			if rt.Status != NodeWaiting {
				_, err = r.commit(ctx, cur, func(x *Execution) error {
					x.Nodes[id].Status = NodeWaiting
					x.Status = StatusHuman
					x.WaitingFor[id] = n.Human.EventType
					appendHistory(x, "human_wait", id, "human decision required", map[string]any{"event": n.Human.EventType, "risk": n.Human.Risk.String()})
					return nil
				})
			} else {
				return progressed, nil
			}
		default:
			return progressed, nil
		}
		if err != nil {
			return progressed, err
		}
		progressed = true
	}
}

func (r *Runtime) readyInternal(e *Execution) []string {
	out := []string{}
	now := time.Now().UTC()
	for id, n := range r.plan.Nodes {
		if n.Kind == NodeActivity || n.Kind == NodeSubflow || n.Kind == NodeCompensation {
			continue
		}
		rt := e.Nodes[id]
		if !isReady(r.plan, e, n, rt, now) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (r *Runtime) readyCandidates(ctx context.Context, e *Execution) ([]Candidate, []string, error) {
	now := time.Now().UTC()
	var out []Candidate
	waiting := []string{}
	for id, n := range r.plan.Nodes {
		if n.Kind != NodeActivity && n.Kind != NodeSubflow {
			continue
		}
		rt := e.Nodes[id]
		if rt != nil && rt.Status == NodeWaiting {
			waiting = append(waiting, id)
			continue
		}
		if !isReady(r.plan, e, n, rt, now) {
			continue
		}
		if e.BudgetLimit.MaxCost > 0 && e.BudgetUsage.Cost+n.EstimatedCost > e.BudgetLimit.MaxCost {
			continue
		}
		if needsApproval(r, n, e) {
			_, err := r.commit(ctx, e, func(x *Execution) error {
				x.Nodes[id].Status = NodeWaiting
				x.Status = StatusHuman
				x.WaitingFor[id] = "Approve:" + id
				appendHistory(x, "approval_required", id, "risk policy requires approval", map[string]any{"risk": n.Risk.String()})
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
			waiting = append(waiting, id)
			continue
		}
		activity := n.Activity
		if n.Capability != "" {
			remaining := 0.0
			if e.BudgetLimit.MaxCost > 0 {
				remaining = e.BudgetLimit.MaxCost - e.BudgetUsage.Cost
			}
			provider, err := r.registry.Resolve(ctx, n.Capability, ProviderPolicy{MaxCost: remaining, MaxRisk: maxRiskPolicy(n.Risk), RequiredPermissions: n.RequiredPermissions, AllowFallback: true})
			if err != nil {
				return nil, nil, err
			}
			activity = provider.Activity
		}
		if activity == "" {
			return nil, nil, fmt.Errorf("adgo: no activity resolved for node %s", id)
		}
		if until := e.ThrottleUntil[activity]; !until.IsZero() && until.After(now) {
			waiting = append(waiting, id)
			continue
		}
		out = append(out, Candidate{Node: n, Activity: activity})
	}
	sort.Strings(waiting)
	return out, waiting, nil
}

func (r *Runtime) executeCandidates(ctx context.Context, e *Execution, selected []Candidate) ([]string, error) {
	now := time.Now().UTC()
	list := make([]scheduledActivity, 0, len(selected))
	for _, c := range selected {
		n := c.Node
		rt := e.Nodes[n.ID]
		attempt := rt.Attempts + 1
		key := renderIdempotency(n.IdempotencyKey, e, n, attempt)
		taskID := taskID(e.ID, n.ID, attempt)
		deadline := time.Time{}
		if n.Timeout > 0 {
			deadline = now.Add(n.Timeout)
		}
		list = append(list, scheduledActivity{c: c, task: TaskRuntime{ID: taskID, NodeID: n.ID, Activity: c.Activity, IdempotencyKey: key, Attempt: attempt, Status: TaskRunning, WorkerID: r.workerID, LeaseUntil: now.Add(r.leaseTTL), StartedAt: now}, req: ActivityRequest{ExecutionID: e.ID, NodeID: n.ID, Attempt: attempt, IdempotencyKey: key, Data: cloneRawMap(e.Data), Artifacts: cloneArtifactMap(e.Artifacts), Deadline: deadline}})
	}
	current, err := r.commit(ctx, e, func(x *Execution) error {
		for _, s := range list {
			rt := x.Nodes[s.c.Node.ID]
			rt.Status = NodeRunning
			rt.StartedAt = now
			if rt.FirstAttemptAt.IsZero() {
				rt.FirstAttemptAt = now
			}
			rt.Attempts = s.task.Attempt
			x.ActiveTasks[s.task.ID] = s.task
			appendHistory(x, "activity_started", s.c.Node.ID, "activity scheduled", map[string]any{"activity": s.c.Activity, "attempt": s.task.Attempt, "idempotencyKey": s.task.IdempotencyKey})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	ch := make(chan activityExecResult, len(list))
	var wg sync.WaitGroup
	for _, s := range list {
		wg.Add(1)
		go func(s scheduledActivity) {
			defer wg.Done()
			started := time.Now()
			var res ActivityResult
			var err error
			callCtx := ctx
			cancel := func() {}
			if !s.req.Deadline.IsZero() {
				callCtx, cancel = context.WithDeadline(ctx, s.req.Deadline)
			}
			defer cancel()
			if s.c.Node.Kind == NodeSubflow {
				h, ok := r.registry.subflow(s.c.Activity)
				if !ok {
					err = fmt.Errorf("adgo: subflow handler %q is not registered", s.c.Activity)
				} else {
					res, err = h(callCtx, s.req)
				}
			} else {
				h, ok := r.registry.activity(s.c.Activity)
				if !ok {
					err = fmt.Errorf("adgo: activity handler %q is not registered", s.c.Activity)
				} else {
					res, err = h(callCtx, s.req)
				}
			}
			ch <- activityExecResult{s: s, res: res, err: err, duration: time.Since(started)}
		}(s)
	}
	wg.Wait()
	close(ch)
	results := make([]activityExecResult, 0, len(list))
	for rr := range ch {
		results = append(results, rr)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].s.c.Node.ID < results[j].s.c.Node.ID })
	executed := []string{}
	cur := current
	for _, rr := range results {
		executed = append(executed, rr.s.c.Node.ID)
		cur, err = r.applyActivityResult(ctx, cur, rr)
		if err != nil {
			return executed, err
		}
		if terminal(cur.Status) {
			break
		}
	}
	return executed, nil
}

func (r *Runtime) applyActivityResult(ctx context.Context, e *Execution, rr activityExecResult) (*Execution, error) {
	return r.applyActivityResultImpl(ctx, e, rr)
}

func (r *Runtime) applyActivityResultImpl(ctx context.Context, e *Execution, rr activityExecResult) (*Execution, error) {
	node := rr.s.c.Node
	task := rr.s.task
	if rr.err == nil {
		return r.commit(ctx, e, func(x *Execution) error {
			rt := x.Nodes[node.ID]
			rt.Status = NodeCompleted
			rt.Outcome = rr.res.Outcome
			if rt.Outcome == "" {
				rt.Outcome = OutcomeCompleted
			}
			rt.CompletedAt = time.Now().UTC()
			rt.LastError = ""
			rt.LastFailure = ""
			rt.Signature = rr.res.Signature
			delete(x.ActiveTasks, task.ID)
			for k, v := range rr.res.Facts {
				if err := SetData(x, k, v); err != nil {
					return err
				}
			}
			for k, v := range rr.res.Artifacts {
				x.Artifacts[k] = v
			}
			before := QualityUtility(x.Quality)
			mergeQuality(x, rr.res.Quality)
			after := QualityUtility(x.Quality)
			if after > before {
				x.Metrics.QualityGain += after - before
			}
			addBudget(&x.BudgetUsage, rr.res.Budget)
			x.Metrics.Cost = x.BudgetUsage.Cost
			x.Metrics.Tokens = x.BudgetUsage.Tokens
			x.Metrics.Activities++
			x.Metrics.ActiveComputeTime += rr.duration
			if rr.res.Signature != "" {
				x.Signatures = append(x.Signatures, rr.res.Signature)
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
			activateNext(r.plan, x, node.ID, rt.Outcome)
			appendHistory(x, "activity_completed", node.ID, "activity completed", map[string]any{"activity": task.Activity, "attempt": task.Attempt})
			return nil
		})
	}
	class := r.classify(rr.err)
	if errors.Is(rr.err, context.DeadlineExceeded) {
		class = FailureTransient
	}
	var fe *FailureError
	retryAfter := time.Duration(0)
	if errors.As(rr.err, &fe) {
		retryAfter = fe.RetryAfter
	}
	policy := normalizeRetry(node.Retry)
	rt := e.Nodes[node.ID]
	attempt := rt.Attempts
	canRetry := (class == FailureTransient || class == FailureRateLimit) && attempt < policy.MaxAttempts
	if canRetry {
		delay := retryAfter
		if delay <= 0 {
			delay = backoff(policy, attempt, e.ID+node.ID)
		}
		if !rt.FirstAttemptAt.IsZero() && policy.MaxRetryDuration > 0 && time.Since(rt.FirstAttemptAt)+delay > policy.MaxRetryDuration {
			canRetry = false
		}
		if canRetry {
			return r.commit(ctx, e, func(x *Execution) error {
				nrt := x.Nodes[node.ID]
				nrt.Status = NodePending
				nrt.NotBefore = time.Now().UTC().Add(delay)
				nrt.LastError = rr.err.Error()
				nrt.LastFailure = class
				delete(x.ActiveTasks, task.ID)
				x.Metrics.Retries++
				if errors.Is(rr.err, context.DeadlineExceeded) {
					x.Metrics.Timeouts++
				}
				if class == FailureRateLimit {
					x.ThrottleUntil[task.Activity] = time.Now().UTC().Add(delay)
				}
				appendHistory(x, "activity_retry", node.ID, "retry scheduled", map[string]any{"class": class, "after": delay.String(), "attempt": attempt})
				return nil
			})
		}
	}
	if class == FailureQuality || class == FailureInvalidInput {
		return r.commit(ctx, e, func(x *Execution) error {
			delete(x.ActiveTasks, task.ID)
			x.Nodes[node.ID].LastError = rr.err.Error()
			x.Nodes[node.ID].LastFailure = class
			v := Violation{Code: string(class), Message: rr.err.Error(), RepairFrom: []string{node.ID}}
			rp, err := r.repair.PlanRepair(r.plan, x, node.ID, []Violation{v})
			if err != nil {
				rp = RepairPlan{GateNode: node.ID, Roots: []string{node.ID}, AffectedNodes: []string{node.ID}, Reason: "activity-local repair", ExpectedCost: node.EstimatedCost, Risk: node.Risk}
			}
			if err := ApplyRepair(r.plan, x, rp); err != nil {
				x.Status = StatusHuman
				x.Nodes[node.ID].Status = NodeWaiting
				x.WaitingFor[node.ID] = "HumanRepairDecision"
				appendHistory(x, "repair_escalated", node.ID, err.Error(), nil)
			}
			return nil
		})
	}
	if class == FailureAmbiguousSideEffect {
		return r.commit(ctx, e, func(x *Execution) error {
			delete(x.ActiveTasks, task.ID)
			x.Nodes[node.ID].Status = NodeWaiting
			x.Nodes[node.ID].LastError = rr.err.Error()
			x.Nodes[node.ID].LastFailure = class
			x.Status = StatusHuman
			x.WaitingFor[node.ID] = "Reconcile:" + node.ID
			appendHistory(x, "ambiguous_side_effect", node.ID, "manual or provider reconciliation required", map[string]any{"idempotencyKey": task.IdempotencyKey})
			return nil
		})
	}
	failed, err := r.commit(ctx, e, func(x *Execution) error {
		delete(x.ActiveTasks, task.ID)
		x.Nodes[node.ID].Status = NodeFailed
		x.Nodes[node.ID].LastError = rr.err.Error()
		x.Nodes[node.ID].LastFailure = class
		x.Status = StatusFailed
		x.Failure = rr.err.Error()
		appendHistory(x, "activity_failed", node.ID, rr.err.Error(), map[string]any{"class": class, "attempt": attempt})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(failed.CompensationStack) > 0 {
		if err := r.compensate(ctx, failed, StatusFailed, failed.Failure); err != nil {
			return nil, err
		}
		return r.store.Load(ctx, failed.ID)
	}
	return failed, nil
}

func (r *Runtime) commit(ctx context.Context, e *Execution, mutate func(*Execution) error) (*Execution, error) {
	next, err := r.store.Commit(ctx, e.ID, e.Version, mutate)
	if errors.Is(err, ErrConflict) {
		fresh, loadErr := r.store.Load(ctx, e.ID)
		if loadErr != nil {
			return nil, loadErr
		}
		return r.store.Commit(ctx, fresh.ID, fresh.Version, mutate)
	}
	return next, err
}

func (r *Runtime) compensate(ctx context.Context, e *Execution, finalStatus ExecutionStatus, reason string) error {
	cur, err := r.commit(ctx, e, func(x *Execution) error {
		x.Status = StatusCompensating
		appendHistory(x, "compensation_started", "", reason, nil)
		return nil
	})
	if err != nil {
		return err
	}
	for len(cur.CompensationStack) > 0 {
		entry := cur.CompensationStack[len(cur.CompensationStack)-1]
		h, ok := r.registry.compensation(entry.Activity)
		if !ok {
			return fmt.Errorf("adgo: compensation handler %q is not registered", entry.Activity)
		}
		req := ActivityRequest{ExecutionID: cur.ID, NodeID: entry.NodeID, Attempt: 1, IdempotencyKey: entry.IdempotencyKey, Data: cloneRawMap(cur.Data), Artifacts: cloneArtifactMap(cur.Artifacts)}
		if err := h(ctx, req); err != nil {
			_, _ = r.commit(ctx, cur, func(x *Execution) error {
				x.Failure = "compensation failed: " + err.Error()
				appendHistory(x, "compensation_failed", entry.NodeID, err.Error(), nil)
				return nil
			})
			return err
		}
		cur, err = r.commit(ctx, cur, func(x *Execution) error {
			x.CompensationStack = x.CompensationStack[:len(x.CompensationStack)-1]
			appendHistory(x, "compensated", entry.NodeID, "compensation completed", map[string]any{"activity": entry.Activity})
			return nil
		})
		if err != nil {
			return err
		}
	}
	_, err = r.commit(ctx, cur, func(x *Execution) error {
		x.Status = finalStatus
		x.Failure = reason
		x.Metrics.WallTime = time.Since(x.CreatedAt)
		appendHistory(x, string(finalStatus), "", reason, nil)
		return nil
	})
	return err
}

func isReady(p *Plan, e *Execution, n Node, rt *NodeRuntime, now time.Time) bool {
	if rt == nil || !rt.Activated || rt.Status != NodePending {
		return false
	}
	if !rt.NotBefore.IsZero() && rt.NotBefore.After(now) {
		return false
	}
	if e.StrategyBans[n.ID] {
		return false
	}
	if n.Kind == NodeJoin && n.Join != nil {
		return joinSatisfied(e, n)
	}
	for _, dep := range n.DependsOn {
		drt := e.Nodes[dep]
		if drt == nil || (drt.Status != NodeCompleted && drt.Status != NodeSkipped) {
			return false
		}
	}
	for _, key := range n.Requires {
		if _, ok := e.Data[key]; !ok {
			if _, ok := e.Artifacts[key]; !ok {
				return false
			}
		}
	}
	return true
}

func joinSatisfied(e *Execution, n Node) bool {
	done := 0
	for _, dep := range n.DependsOn {
		rt := e.Nodes[dep]
		if rt != nil && (rt.Status == NodeCompleted || rt.Status == NodeSkipped) {
			done++
		}
	}
	switch n.Join.Mode {
	case JoinAny:
		return done >= 1
	case JoinNOfM, JoinQuorum:
		return done >= n.Join.Threshold
	default:
		return done == len(n.DependsOn)
	}
}

func completeNode(p *Plan, e *Execution, id string, outcome Outcome, res ActivityResult) error {
	rt := e.Nodes[id]
	if rt == nil {
		return fmt.Errorf("adgo: node runtime missing: %s", id)
	}
	if outcome == "" {
		outcome = OutcomeCompleted
	}
	rt.Status = NodeCompleted
	rt.Outcome = outcome
	rt.CompletedAt = time.Now().UTC()
	activateNext(p, e, id, outcome)
	appendHistory(e, "node_completed", id, "node completed", map[string]any{"outcome": outcome})
	return nil
}
func activateNext(p *Plan, e *Execution, id string, outcome Outcome) {
	for _, tr := range p.outbound[id] {
		if tr.Outcome != "" && tr.Outcome != outcome {
			continue
		}
		rt := e.Nodes[tr.To]
		if rt == nil {
			continue
		}
		rt.Activated = true
		if rt.Status == NodeDormant || rt.Status == NodeSkipped {
			rt.Status = NodePending
		}
	}
}

func genericGate(n Node, e *Execution) GateResult {
	gr := GateResult{Outcome: OutcomePass, Quality: cloneQuality(e.Quality)}
	if n.Gate == nil {
		return gr
	}
	for dim, min := range n.Gate.HardFloors {
		observed := e.Quality[dim]
		if observed < min {
			gr.Outcome = OutcomeRepair
			gr.Violations = append(gr.Violations, Violation{Code: "quality_floor", Message: fmt.Sprintf("%s %.4f < %.4f", dim, observed, min), Dimension: dim, Required: min, Observed: observed, Critical: true, RepairFrom: append([]string(nil), n.Gate.RepairFrom...)})
		}
	}
	if n.Gate.MaxCriticalErrors >= 0 {
		if raw, ok := e.Data["criticalErrors"]; ok {
			var count int
			if json.Unmarshal(raw, &count) == nil && count > n.Gate.MaxCriticalErrors {
				gr.Outcome = OutcomeRepair
				gr.Violations = append(gr.Violations, Violation{Code: "critical_errors", Message: fmt.Sprintf("critical errors %d > %d", count, n.Gate.MaxCriticalErrors), Critical: true, RepairFrom: append([]string(nil), n.Gate.RepairFrom...)})
			}
		}
	}
	return gr
}

func mergeQuality(e *Execution, q QualityVector) {
	if e.Quality == nil {
		e.Quality = QualityVector{}
	}
	for k, v := range q {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		e.Quality[k] = v
	}
}
func recordQuality(e *Execution, nodeID string) {
	u := QualityUtility(e.Quality)
	e.QualityHistory = append(e.QualityHistory, QualitySnapshot{At: time.Now().UTC(), NodeID: nodeID, Values: cloneQuality(e.Quality), Utility: u})
	if len(e.QualityHistory) > 64 {
		e.QualityHistory = e.QualityHistory[len(e.QualityHistory)-64:]
	}
}
func cloneQuality(q QualityVector) QualityVector {
	out := QualityVector{}
	for k, v := range q {
		out[k] = v
	}
	return out
}
func cloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}
func cloneArtifactMap(in map[string]ArtifactRef) map[string]ArtifactRef {
	out := map[string]ArtifactRef{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func snapshot(e *Execution) Snapshot {
	nodes := map[string]NodeRuntime{}
	for k, v := range e.Nodes {
		if v != nil {
			nodes[k] = *v
		}
	}
	return Snapshot{ExecutionID: e.ID, Status: e.Status, Data: cloneRawMap(e.Data), Artifacts: cloneArtifactMap(e.Artifacts), Quality: cloneQuality(e.Quality), BudgetLimit: e.BudgetLimit, BudgetUsage: e.BudgetUsage, Nodes: nodes}
}

func appendHistory(e *Execution, typ, node, msg string, data map[string]any) {
	seq := uint64(len(e.History) + 1)
	e.History = append(e.History, HistoryEntry{Seq: seq, At: time.Now().UTC(), Type: typ, NodeID: node, Message: msg, Data: data})
}
func terminal(s ExecutionStatus) bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCanceled || s == StatusDeadlocked
}
func goalsSatisfied(p *Plan, e *Execution) bool {
	active := 0
	unfinished := 0
	for id, rt := range e.Nodes {
		_ = id
		if !rt.Activated {
			continue
		}
		active++
		if rt.Status != NodeCompleted && rt.Status != NodeSkipped {
			unfinished++
		}
	}
	return active > 0 && unfinished == 0 && len(e.ActiveTasks) == 0
}
func hasPendingTime(e *Execution) bool {
	for _, rt := range e.Nodes {
		if rt.Status == NodeWaiting || (!rt.NotBefore.IsZero() && rt.NotBefore.After(time.Now().UTC())) {
			return true
		}
	}
	return false
}
func deadlockReason(p *Plan, e *Execution) string {
	parts := []string{}
	for id, rt := range e.Nodes {
		if !rt.Activated || rt.Status == NodeCompleted || rt.Status == NodeSkipped {
			continue
		}
		n := p.Nodes[id]
		missing := []string{}
		for _, dep := range n.DependsOn {
			drt := e.Nodes[dep]
			if drt == nil || (drt.Status != NodeCompleted && drt.Status != NodeSkipped) {
				missing = append(missing, "node:"+dep)
			}
		}
		for _, key := range n.Requires {
			if _, ok := e.Data[key]; !ok {
				if _, ok := e.Artifacts[key]; !ok {
					missing = append(missing, "data:"+key)
				}
			}
		}
		parts = append(parts, fmt.Sprintf("%s waits for [%s]", id, strings.Join(missing, ",")))
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "no ready nodes and no pending external events"
	}
	return strings.Join(parts, "; ")
}

func needsApproval(r *Runtime, n Node, e *Execution) bool {
	if !n.ExternalEffect {
		return false
	}
	if n.Risk < RiskCritical && n.Risk < r.approvalThreshold {
		return false
	}
	key := "approval:" + n.ID
	raw, ok := e.Data[key]
	if !ok {
		return true
	}
	var approved bool
	return json.Unmarshal(raw, &approved) != nil || !approved
}
func maxRiskPolicy(nodeRisk RiskLevel) RiskLevel {
	if nodeRisk == 0 {
		return RiskCritical
	}
	return nodeRisk
}

func checkBudget(e *Execution, now time.Time) error {
	l, u := e.BudgetLimit, e.BudgetUsage
	if l.MaxCost > 0 && u.Cost >= l.MaxCost {
		return ErrBudgetExceeded
	}
	if l.MaxTokens > 0 && u.Tokens >= l.MaxTokens {
		return ErrBudgetExceeded
	}
	if l.MaxDuration > 0 && now.Sub(e.CreatedAt) >= l.MaxDuration {
		return ErrBudgetExceeded
	}
	if l.MaxLLMCalls > 0 && u.LLMCalls >= l.MaxLLMCalls {
		return ErrBudgetExceeded
	}
	if l.MaxSearchQueries > 0 && u.SearchQueries >= l.MaxSearchQueries {
		return ErrBudgetExceeded
	}
	if l.MaxBrowserFetches > 0 && u.BrowserFetches >= l.MaxBrowserFetches {
		return ErrBudgetExceeded
	}
	return nil
}
func addBudget(dst *BudgetUsage, inc BudgetUsage) {
	dst.Cost += inc.Cost
	dst.Tokens += inc.Tokens
	dst.ActiveDuration += inc.ActiveDuration
	dst.LLMCalls += inc.LLMCalls
	dst.SearchQueries += inc.SearchQueries
	dst.BrowserFetches += inc.BrowserFetches
}

func DefaultClassify(err error) FailureClass {
	if err == nil {
		return ""
	}
	var fe *FailureError
	if errors.As(err, &fe) && fe.Class != "" {
		return fe.Class
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return FailureTransient
	}
	return FailurePermanent
}
func backoff(p RetryPolicy, attempt int, seed string) time.Duration {
	exp := float64(p.BaseDelay) * math.Pow(2, float64(maxInt(attempt-1, 0)))
	if exp > float64(p.MaxDelay) {
		exp = float64(p.MaxDelay)
	}
	h := sha256.Sum256([]byte(seed + fmt.Sprint(attempt)))
	src := rand.New(rand.NewSource(int64(h[0])<<56 | int64(h[1])<<48 | int64(h[2])<<40 | int64(h[3])<<32 | int64(h[4])<<24 | int64(h[5])<<16 | int64(h[6])<<8 | int64(h[7])))
	j := (src.Float64()*2 - 1) * p.JitterFraction
	d := time.Duration(exp * (1 + j))
	if d < 0 {
		return 0
	}
	return d
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func renderIdempotency(template string, e *Execution, n Node, attempt int) string {
	if template == "" {
		template = "{execution}:{node}:{revision}"
	}
	revision := e.RevisionCounters[n.ID]
	r := strings.NewReplacer("{execution}", e.ID, "{node}", n.ID, "{attempt}", fmt.Sprint(attempt), "{revision}", fmt.Sprint(revision), "{plan}", e.PlanDigest)
	return r.Replace(template)
}
func taskID(exec, node string, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", exec, node, attempt)))
	return "task-" + hex.EncodeToString(sum[:8])
}
func eventID(e Event) string {
	sum := sha256.Sum256(append([]byte(e.Type+"|"+e.TargetNode+"|"), e.Payload...))
	return "evt-" + hex.EncodeToString(sum[:8])
}

func (r *Runtime) resumeDueTimers(ctx context.Context, e *Execution) (bool, error) {
	now := time.Now().UTC()
	due := []string{}
	for id, n := range r.plan.Nodes {
		rt := e.Nodes[id]
		if n.Kind == NodeWait && n.Wait != nil && n.Wait.Duration > 0 && rt != nil && rt.Status == NodeWaiting && !rt.NotBefore.IsZero() && !rt.NotBefore.After(now) {
			due = append(due, id)
		}
	}
	if len(due) == 0 {
		return false, nil
	}
	sort.Strings(due)
	_, err := r.commit(ctx, e, func(x *Execution) error {
		for _, id := range due {
			delete(x.WaitingFor, id)
			if err := completeNode(r.plan, x, id, OutcomeCompleted, ActivityResult{}); err != nil {
				return err
			}
			x.Nodes[id].NotBefore = time.Time{}
			appendHistory(x, "timer_fired", id, "durable timer fired", nil)
		}
		x.Status = StatusRunning
		return nil
	})
	return err == nil, err
}
