package adgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type PlanRef struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Digest  string `json:"digest,omitempty"`
}

// Host is a multi-plan deployment container. One durable store can contain
// executions pinned to many immutable plan versions while workers poll them
// through a single process-level API.
type Host struct {
	store    Store
	mu       sync.RWMutex
	byDigest map[string]*Engine
	order    []string
	next     atomic.Uint64
}

func NewHost(store Store) (*Host, error) {
	if store == nil {
		return nil, fmt.Errorf("adgo: host store is required")
	}
	if _, ok := store.(ExecutionCatalog); !ok {
		return nil, fmt.Errorf("adgo: host requires ExecutionCatalog")
	}
	return &Host{store: store, byDigest: map[string]*Engine{}}, nil
}

func (h *Host) Register(plan *Plan, registry *Registry, opts ...EngineOption) (*Engine, error) {
	if plan == nil {
		return nil, fmt.Errorf("adgo: plan is required")
	}
	engine, err := NewEngine(plan, h.store, registry, opts...)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if existing := h.byDigest[plan.Digest]; existing != nil {
		return existing, nil
	}
	h.byDigest[plan.Digest] = engine
	h.order = append(h.order, plan.Digest)
	sort.Strings(h.order)
	return engine, nil
}

func (h *Host) Plans() []PlanRef {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]PlanRef, 0, len(h.order))
	for _, digest := range h.order {
		engine := h.byDigest[digest]
		out = append(out, PlanRef{ID: engine.plan.ID, Version: engine.plan.Version, Digest: digest})
	}
	return out
}

func (h *Host) Engine(ref PlanRef) (*Engine, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if ref.Digest != "" {
		if engine := h.byDigest[ref.Digest]; engine != nil {
			return engine, nil
		}
		return nil, fmt.Errorf("adgo: plan digest %s is not registered", ref.Digest)
	}
	var found *Engine
	for _, engine := range h.byDigest {
		if engine.plan.ID != ref.ID {
			continue
		}
		if ref.Version != "" && engine.plan.Version != ref.Version {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("adgo: plan ref %s/%s is ambiguous; use digest", ref.ID, ref.Version)
		}
		found = engine
	}
	if found == nil {
		return nil, fmt.Errorf("adgo: plan %s/%s is not registered", ref.ID, ref.Version)
	}
	return found, nil
}

func (h *Host) Start(ctx context.Context, ref PlanRef, executionID string, initial map[string]any, budget BudgetLimit) (*Execution, error) {
	engine, err := h.Engine(ref)
	if err != nil {
		return nil, err
	}
	return engine.StartOrLoad(ctx, executionID, initial, budget)
}

func (h *Host) engineForExecution(ctx context.Context, executionID string) (*Engine, *Execution, error) {
	execution, err := h.store.Load(ctx, executionID)
	if err != nil {
		return nil, nil, err
	}
	h.mu.RLock()
	engine := h.byDigest[execution.PlanDigest]
	h.mu.RUnlock()
	if engine == nil {
		return nil, execution, fmt.Errorf("adgo: no registered engine for execution %s plan digest %s", executionID, execution.PlanDigest)
	}
	return engine, execution, nil
}

func (h *Host) Advance(ctx context.Context, executionID string) (AdvanceResult, error) {
	engine, _, err := h.engineForExecution(ctx, executionID)
	if err != nil {
		return AdvanceResult{}, err
	}
	return engine.Advance(ctx, executionID)
}

func (h *Host) Signal(ctx context.Context, executionID string, event Event) error {
	engine, _, err := h.engineForExecution(ctx, executionID)
	if err != nil {
		return err
	}
	return engine.Signal(ctx, executionID, event)
}

type HostedWorkItem struct {
	PlanDigest string    `json:"planDigest"`
	Work       *WorkItem `json:"work"`
}

func hostRoundRobinSlot(sequence uint64, count uint64) uint64 {
	if count == 0 {
		return 0
	}
	return sequence % count
}

// Poll performs round-robin fairness across registered plan versions. Fairness
// within one plan is handled by Engine.Poll's execution aging/utility score.
func (h *Host) Poll(ctx context.Context, spec WorkerSpec) (*HostedWorkItem, error) {
	h.mu.RLock()
	order := append([]string(nil), h.order...)
	engines := make(map[string]*Engine, len(h.byDigest))
	for digest, engine := range h.byDigest {
		engines[digest] = engine
	}
	h.mu.RUnlock()
	if len(order) == 0 {
		return nil, ErrNoWork
	}
	count := uint64(len(order))
	start := hostRoundRobinSlot(h.next.Add(1)-1, count)
	for offset := uint64(0); offset < count; offset++ {
		digest := order[(start+offset)%count]
		work, err := engines[digest].Poll(ctx, spec)
		if err == nil {
			return &HostedWorkItem{PlanDigest: digest, Work: work}, nil
		}
		if errors.Is(err, ErrNoWork) {
			continue
		}
		return nil, err
	}
	return nil, ErrNoWork
}

func (h *Host) Heartbeat(ctx context.Context, token WorkToken, details map[string]any) error {
	engine, _, err := h.engineForExecution(ctx, token.ExecutionID)
	if err != nil {
		return err
	}
	return engine.Heartbeat(ctx, token, details)
}

func (h *Host) Complete(ctx context.Context, token WorkToken, result ActivityResult, duration time.Duration) (*Execution, error) {
	engine, _, err := h.engineForExecution(ctx, token.ExecutionID)
	if err != nil {
		return nil, err
	}
	return engine.Complete(ctx, token, result, duration)
}

func (h *Host) Fail(ctx context.Context, token WorkToken, activityErr error, duration time.Duration) (*Execution, error) {
	engine, _, err := h.engineForExecution(ctx, token.ExecutionID)
	if err != nil {
		return nil, err
	}
	return engine.Fail(ctx, token, activityErr, duration)
}

func (h *Host) RunCoordinator(ctx context.Context) error {
	catalog := h.store.(ExecutionCatalog)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := catalog.ListExecutionIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			engine, execution, err := h.engineForExecution(ctx, id)
			if err != nil {
				continue
			}
			if terminal(execution.Status) {
				continue
			}
			if _, err := engine.Advance(ctx, id); err != nil && !errors.Is(err, ErrDeadlock) {
				return err
			}
		}
		if err := sleepContext(ctx, 50*time.Millisecond); err != nil {
			return err
		}
	}
}

func (h *Host) RunWorker(ctx context.Context, spec WorkerSpec) error {
	if spec.Concurrency <= 0 {
		spec.Concurrency = 1
	}
	if spec.PollInterval <= 0 {
		spec.PollInterval = 100 * time.Millisecond
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	errCh := make(chan error, spec.Concurrency)
	for i := 0; i < spec.Concurrency; i++ {
		wg.Add(1)
		local := spec
		if local.ID == "" {
			local.ID = fmt.Sprintf("host-worker-%d", time.Now().UnixNano())
		}
		if spec.Concurrency > 1 {
			local.ID = fmt.Sprintf("%s/%d", local.ID, i+1)
		}
		go func() {
			defer wg.Done()
			for {
				item, err := h.Poll(workerCtx, local)
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
				h.mu.RLock()
				engine := h.byDigest[item.PlanDigest]
				h.mu.RUnlock()
				if engine == nil {
					errCh <- fmt.Errorf("adgo: plan disappeared while work was claimed")
					return
				}
				if err := engine.executeWorkItem(workerCtx, local, item.Work); err != nil && !errors.Is(err, ErrStaleTask) {
					errCh <- err
					return
				}
				if _, err := engine.Advance(workerCtx, item.Work.Token.ExecutionID); err != nil && !errors.Is(err, ErrDeadlock) {
					errCh <- err
					return
				}
			}
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
