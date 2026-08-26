package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
	PolicyDelay PolicyAction = "delay"
	PolicyHuman PolicyAction = "human"
)

type PolicyRequest struct {
	ExecutionID string                     `json:"executionId"`
	PlanID      string                     `json:"planId"`
	PlanDigest  string                     `json:"planDigest"`
	Node        Node                       `json:"node"`
	Activity    string                     `json:"activity"`
	Provider    string                     `json:"provider,omitempty"`
	WorkerID    string                     `json:"workerId"`
	Attempt     int                        `json:"attempt"`
	Data        map[string]json.RawMessage `json:"data,omitempty"`
	Artifacts   map[string]ArtifactRef     `json:"artifacts,omitempty"`
	BudgetUsage BudgetUsage                `json:"budgetUsage"`
	BudgetLimit BudgetLimit                `json:"budgetLimit"`
	Quality     QualityVector              `json:"quality,omitempty"`
	At          time.Time                  `json:"at"`
}

type PolicyDecision struct {
	Action     PolicyAction   `json:"action"`
	Reason     string         `json:"reason,omitempty"`
	RetryAfter time.Duration  `json:"retryAfter,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type RuntimePolicy interface {
	Authorize(context.Context, PolicyRequest) (PolicyDecision, error)
}

type RuntimePolicyFunc func(context.Context, PolicyRequest) (PolicyDecision, error)

func (f RuntimePolicyFunc) Authorize(ctx context.Context, request PolicyRequest) (PolicyDecision, error) {
	return f(ctx, request)
}

type PolicyEngineOptions struct {
	// FailClosed denies work when the policy service itself errors. When false,
	// policy-service errors durably delay the task and retry later.
	FailClosed            bool
	PolicyErrorRetry      time.Duration
	MaxPolicySkipsPerPoll int
}

// PolicyEngine inserts a dynamic policy-as-code boundary after durable task
// claim but before handler execution. The underlying Engine still performs all
// static plan/risk/permission/provider checks. Dynamic policy can allow, deny,
// delay or escalate the claimed task to a durable human decision.
type PolicyEngine struct {
	Engine  *Engine
	Policy  RuntimePolicy
	Options PolicyEngineOptions
}

func NewPolicyEngine(engine *Engine, policy RuntimePolicy, options PolicyEngineOptions) (*PolicyEngine, error) {
	if engine == nil || policy == nil {
		return nil, fmt.Errorf("adgo: policy engine and runtime policy are required")
	}
	if options.PolicyErrorRetry <= 0 {
		options.PolicyErrorRetry = 5 * time.Second
	}
	if options.MaxPolicySkipsPerPoll <= 0 {
		options.MaxPolicySkipsPerPoll = 32
	}
	return &PolicyEngine{Engine: engine, Policy: policy, Options: options}, nil
}

func (p *PolicyEngine) Poll(ctx context.Context, spec WorkerSpec) (*WorkItem, error) {
	for skipped := 0; skipped < p.Options.MaxPolicySkipsPerPoll; skipped++ {
		item, err := p.Engine.Poll(ctx, spec)
		if err != nil {
			return nil, err
		}
		execution, err := p.Engine.store.Load(ctx, item.Token.ExecutionID)
		if err != nil {
			return nil, err
		}
		now := p.Engine.now()
		decision, policyErr := p.Policy.Authorize(ctx, PolicyRequest{
			ExecutionID: execution.ID,
			PlanID:      execution.PlanID,
			PlanDigest:  execution.PlanDigest,
			Node:        item.Node,
			Activity:    item.Activity,
			Provider:    item.Provider,
			WorkerID:    item.Token.WorkerID,
			Attempt:     item.Token.Attempt,
			Data:        cloneRawMap(item.Request.Data),
			Artifacts:   cloneArtifactMap(item.Request.Artifacts),
			BudgetUsage: execution.BudgetUsage,
			BudgetLimit: execution.BudgetLimit,
			Quality:     cloneQuality(execution.Quality),
			At:          now,
		})
		if policyErr != nil {
			if p.Options.FailClosed {
				decision = PolicyDecision{Action: PolicyDeny, Reason: "runtime policy unavailable: " + policyErr.Error()}
			} else {
				decision = PolicyDecision{Action: PolicyDelay, Reason: "runtime policy unavailable: " + policyErr.Error(), RetryAfter: p.Options.PolicyErrorRetry}
			}
		}
		if decision.Action == "" {
			decision.Action = PolicyDeny
			if decision.Reason == "" {
				decision.Reason = "runtime policy returned no action"
			}
		}
		switch decision.Action {
		case PolicyAllow:
			if err := p.auditDecision(ctx, item, decision, false); err != nil {
				return nil, err
			}
			return item, nil
		case PolicyDelay:
			if decision.RetryAfter <= 0 {
				decision.RetryAfter = p.Options.PolicyErrorRetry
			}
			if err := p.releaseClaim(ctx, item, NodePending, decision, p.Engine.now().Add(decision.RetryAfter), "policy_delayed"); err != nil {
				if errors.Is(err, ErrStaleTask) {
					continue
				}
				return nil, err
			}
		case PolicyHuman:
			if err := p.releaseClaim(ctx, item, NodeWaiting, decision, time.Time{}, "policy_human"); err != nil {
				if errors.Is(err, ErrStaleTask) {
					continue
				}
				return nil, err
			}
		case PolicyDeny:
			if err := p.denyClaim(ctx, item, decision); err != nil {
				if errors.Is(err, ErrStaleTask) {
					continue
				}
				return nil, err
			}
		default:
			return nil, fmt.Errorf("adgo: unsupported runtime policy action %q", decision.Action)
		}
	}
	return nil, ErrNoWork
}

func (p *PolicyEngine) auditDecision(ctx context.Context, item *WorkItem, decision PolicyDecision, release bool) error {
	_, err := p.Engine.mutate(ctx, item.Token.ExecutionID, func(execution *Execution) error {
		if release {
			if _, err := validateClaim(execution, item.Token, p.Engine.now()); err != nil {
				return err
			}
		}
		appendHistory(execution, "runtime_policy", item.Node.ID, decision.Reason, map[string]any{
			"action":   decision.Action,
			"worker":   item.Token.WorkerID,
			"metadata": decision.Metadata,
		})
		return nil
	})
	return err
}

func (p *PolicyEngine) releaseClaim(ctx context.Context, item *WorkItem, status NodeStatus, decision PolicyDecision, notBefore time.Time, historyKind string) error {
	_, err := p.Engine.mutate(ctx, item.Token.ExecutionID, func(execution *Execution) error {
		if _, err := validateClaim(execution, item.Token, p.Engine.now()); err != nil {
			return err
		}
		delete(execution.ActiveTasks, item.Token.TaskID)
		runtime := execution.Nodes[item.Node.ID]
		runtime.Status = status
		runtime.NotBefore = notBefore
		runtime.LastError = decision.Reason
		if status == NodeWaiting {
			execution.WaitingFor[item.Node.ID] = "PolicyDecision:" + item.Node.ID
			execution.Status = StatusHuman
		} else if !terminal(execution.Status) {
			execution.Status = StatusRunning
		}
		appendHistory(execution, historyKind, item.Node.ID, decision.Reason, map[string]any{
			"action":     decision.Action,
			"worker":     item.Token.WorkerID,
			"metadata":   decision.Metadata,
			"retryAfter": decision.RetryAfter.String(),
		})
		return nil
	})
	return err
}

func (p *PolicyEngine) denyClaim(ctx context.Context, item *WorkItem, decision PolicyDecision) error {
	_, err := p.Engine.mutate(ctx, item.Token.ExecutionID, func(execution *Execution) error {
		if _, err := validateClaim(execution, item.Token, p.Engine.now()); err != nil {
			return err
		}
		delete(execution.ActiveTasks, item.Token.TaskID)
		runtime := execution.Nodes[item.Node.ID]
		runtime.LastError = decision.Reason
		if hasOutcomeTransition(p.Engine.plan, item.Node.ID, OutcomeRejected) {
			runtime.Status = NodeCompleted
			runtime.Outcome = OutcomeRejected
			runtime.CompletedAt = p.Engine.now()
			activateNext(p.Engine.plan, execution, item.Node.ID, OutcomeRejected)
			execution.Status = StatusRunning
		} else {
			runtime.Status = NodeFailed
			runtime.Outcome = OutcomeRejected
			execution.Status = StatusFailed
			execution.Failure = decision.Reason
			if execution.Failure == "" {
				execution.Failure = "runtime policy denied activity"
			}
		}
		appendHistory(execution, "policy_denied", item.Node.ID, execution.Failure, map[string]any{
			"worker":   item.Token.WorkerID,
			"metadata": decision.Metadata,
		})
		return nil
	})
	return err
}


func (p *PolicyEngine) Resolve(ctx context.Context, executionID, nodeID string, allow bool, actor, reason string, patch map[string]any) (*Execution, error) {
	return p.Engine.mutate(ctx, executionID, func(execution *Execution) error {
		runtime := execution.Nodes[nodeID]
		if runtime == nil || runtime.Status != NodeWaiting || execution.WaitingFor[nodeID] != "PolicyDecision:"+nodeID {
			return fmt.Errorf("%w: node %s is not waiting for runtime policy decision", ErrStaleTask, nodeID)
		}
		for key, value := range patch {
			if err := SetData(execution, key, value); err != nil {
				return err
			}
		}
		delete(execution.WaitingFor, nodeID)
		if allow {
			runtime.Status = NodePending
			runtime.NotBefore = time.Time{}
			execution.Status = StatusRunning
			appendHistory(execution, "policy_human_allowed", nodeID, reason, map[string]any{"actor": actor})
			return nil
		}
		runtime.Status = NodeFailed
		runtime.Outcome = OutcomeRejected
		execution.Status = StatusFailed
		execution.Failure = reason
		if execution.Failure == "" {
			execution.Failure = "runtime policy rejected by operator"
		}
		appendHistory(execution, "policy_human_denied", nodeID, execution.Failure, map[string]any{"actor": actor})
		return nil
	})
}

func (p *PolicyEngine) RunWorker(ctx context.Context, spec WorkerSpec) error {
	if spec.Concurrency <= 0 {
		spec.Concurrency = 1
	}
	if spec.PollInterval <= 0 {
		spec.PollInterval = 100 * time.Millisecond
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, spec.Concurrency)
	for slot := 0; slot < spec.Concurrency; slot++ {
		local := spec
		if local.ID == "" {
			local.ID = fmt.Sprintf("policy-worker-%d", time.Now().UnixNano())
		}
		if spec.Concurrency > 1 {
			local.ID = fmt.Sprintf("%s/%d", local.ID, slot+1)
		}
		go func() {
			for {
				item, err := p.Poll(workerCtx, local)
				if errors.Is(err, ErrNoWork) {
					if err := sleepContext(workerCtx, local.PollInterval); err != nil {
						errCh <- err
						return
					}
					continue
				}
				if err != nil {
					errCh <- err
					return
				}
				if err := p.Engine.executeWorkItem(workerCtx, p.Engine.normalizeWorker(local), item); err != nil && !errors.Is(err, ErrStaleTask) {
					errCh <- err
					return
				}
				if _, err := p.Engine.Advance(workerCtx, item.Token.ExecutionID); err != nil && !errors.Is(err, ErrDeadlock) {
					errCh <- err
					return
				}
			}
		}()
	}
	for range spec.Concurrency {
		err := <-errCh
		if err != nil && !errors.Is(err, context.Canceled) {
			cancel()
			return err
		}
	}
	return ctx.Err()
}
