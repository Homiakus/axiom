package adgo

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

type Provider struct {
	Name        string
	Activity    string
	Available   func(context.Context) bool
	Quality     float64
	Cost        float64
	Latency     time.Duration
	Privacy     float64
	Risk        RiskLevel
	Permissions []string
}

type ProviderPolicy struct {
	MinQuality          float64
	MaxCost             float64
	MaxLatency          time.Duration
	MinPrivacy          float64
	MaxRisk             RiskLevel
	RequiredPermissions []string
	AllowFallback       bool
}

type Registry struct {
	mu            sync.RWMutex
	activities    map[string]ActivityHandler
	decisions     map[string]DecisionHandler
	gates         map[string]GateHandler
	subflows      map[string]SubflowHandler
	compensations map[string]CompensationHandler
	capabilities  map[string][]Provider
}

func NewRegistry() *Registry {
	return &Registry{activities: map[string]ActivityHandler{}, decisions: map[string]DecisionHandler{}, gates: map[string]GateHandler{}, subflows: map[string]SubflowHandler{}, compensations: map[string]CompensationHandler{}, capabilities: map[string][]Provider{}}
}
func (r *Registry) Activity(name string, h ActivityHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activities[name] = h
}
func (r *Registry) Decision(name string, h DecisionHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisions[name] = h
}
func (r *Registry) Gate(name string, h GateHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gates[name] = h
}
func (r *Registry) Subflow(name string, h SubflowHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subflows[name] = h
}
func (r *Registry) Compensation(name string, h CompensationHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.compensations[name] = h
}
func (r *Registry) Provider(capability string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[capability] = append(r.capabilities[capability], p)
}

func (r *Registry) activity(name string) (ActivityHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.activities[name]
	return h, ok
}
func (r *Registry) decision(name string) (DecisionHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.decisions[name]
	return h, ok
}
func (r *Registry) gate(name string) (GateHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.gates[name]
	return h, ok
}
func (r *Registry) subflow(name string) (SubflowHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.subflows[name]
	return h, ok
}
func (r *Registry) compensation(name string) (CompensationHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.compensations[name]
	return h, ok
}

func (r *Registry) Resolve(ctx context.Context, capability string, policy ProviderPolicy) (Provider, error) {
	r.mu.RLock()
	providers := append([]Provider(nil), r.capabilities[capability]...)
	r.mu.RUnlock()
	if len(providers) == 0 {
		return Provider{}, fmt.Errorf("adgo: no providers for capability %q", capability)
	}
	required := setOf(policy.RequiredPermissions)
	type scored struct {
		p Provider
		s float64
	}
	var candidates []scored
	for _, p := range providers {
		if p.Available != nil && !p.Available(ctx) {
			continue
		}
		if policy.MinQuality > 0 && p.Quality < policy.MinQuality {
			continue
		}
		if policy.MaxCost > 0 && p.Cost > policy.MaxCost {
			continue
		}
		if policy.MaxLatency > 0 && p.Latency > policy.MaxLatency {
			continue
		}
		if policy.MinPrivacy > 0 && p.Privacy < policy.MinPrivacy {
			continue
		}
		if p.Risk > policy.MaxRisk && policy.MaxRisk > 0 {
			continue
		}
		pp := setOf(p.Permissions)
		ok := true
		for k := range required {
			if _, yes := pp[k]; !yes {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		latencySeconds := p.Latency.Seconds()
		if latencySeconds < 0 {
			latencySeconds = 0
		}
		score := 3*p.Quality + 1.5*p.Privacy - 1.2*p.Cost - 0.05*latencySeconds - 0.75*float64(p.Risk)
		candidates = append(candidates, scored{p: p, s: score})
	}
	if len(candidates) == 0 {
		return Provider{}, fmt.Errorf("adgo: no provider satisfies policy for capability %q", capability)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if math.Abs(candidates[i].s-candidates[j].s) < 1e-9 {
			return candidates[i].p.Name < candidates[j].p.Name
		}
		return candidates[i].s > candidates[j].s
	})
	return candidates[0].p, nil
}
