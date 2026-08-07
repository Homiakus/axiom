package adgo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// RouterConfig controls online provider adaptation. The router applies hard
// policy constraints first, then ranks valid providers using static metadata and
// observed reliability/latency/quality. Circuit breaking is deliberately local
// to provider+capability, so one degraded backend cannot stall unrelated work.
type RouterConfig struct {
	FailureThreshold int
	BaseCooldown     time.Duration
	MaxCooldown      time.Duration
	EWMAAlpha        float64
	ExplorationWeight float64
}

func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		FailureThreshold:  1,
		BaseCooldown:      5 * time.Second,
		MaxCooldown:       2 * time.Minute,
		EWMAAlpha:         0.25,
		ExplorationWeight: 0.15,
	}
}

// ProviderHealth is safe to expose through admin/observability APIs.
type ProviderHealth struct {
	Capability          string        `json:"capability"`
	Provider            string        `json:"provider"`
	Requests            uint64        `json:"requests"`
	Successes           uint64        `json:"successes"`
	Failures            uint64        `json:"failures"`
	ConsecutiveFailures int           `json:"consecutiveFailures"`
	EWMALatency         time.Duration `json:"ewmaLatency"`
	EWMAQuality         float64       `json:"ewmaQuality"`
	EWMACost            float64       `json:"ewmaCost"`
	CircuitOpenUntil    time.Time     `json:"circuitOpenUntil,omitempty"`
	LastFailure         FailureClass  `json:"lastFailure,omitempty"`
	LastError           string        `json:"lastError,omitempty"`
	LastUpdated         time.Time     `json:"lastUpdated"`
}

// AdaptiveRouter turns Registry capability providers into an online routing
// layer. It never relaxes hard privacy/risk/permission/cost constraints to gain
// score. Exploration is only applied among already-valid providers.
type AdaptiveRouter struct {
	registry *Registry
	config   RouterConfig
	mu       sync.Mutex
	health   map[string]*ProviderHealth
}

func NewAdaptiveRouter(registry *Registry, config RouterConfig) *AdaptiveRouter {
	if registry == nil {
		registry = NewRegistry()
	}
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 1
	}
	if config.BaseCooldown <= 0 {
		config.BaseCooldown = 5 * time.Second
	}
	if config.MaxCooldown <= 0 {
		config.MaxCooldown = 2 * time.Minute
	}
	if config.MaxCooldown < config.BaseCooldown {
		config.MaxCooldown = config.BaseCooldown
	}
	if config.EWMAAlpha <= 0 || config.EWMAAlpha > 1 {
		config.EWMAAlpha = 0.25
	}
	if config.ExplorationWeight < 0 {
		config.ExplorationWeight = 0
	}
	return &AdaptiveRouter{registry: registry, config: config, health: map[string]*ProviderHealth{}}
}

func providerHealthKey(capability, provider string) string { return capability + "\x00" + provider }

func (r *AdaptiveRouter) state(capability, provider string) ProviderHealth {
	key := providerHealthKey(capability, provider)
	r.mu.Lock()
	defer r.mu.Unlock()
	if h := r.health[key]; h != nil {
		return *h
	}
	return ProviderHealth{Capability: capability, Provider: provider}
}

// Ranked returns all currently usable providers from best to worst. This is the
// concrete fallback primitive missing from Registry.Resolve's legacy API.
func (r *AdaptiveRouter) Ranked(ctx context.Context, capability string, policy ProviderPolicy) ([]Provider, error) {
	r.registry.mu.RLock()
	providers := append([]Provider(nil), r.registry.capabilities[capability]...)
	r.registry.mu.RUnlock()
	if len(providers) == 0 {
		return nil, fmt.Errorf("adgo: no providers for capability %q", capability)
	}

	type scored struct {
		provider Provider
		score    float64
	}
	now := time.Now().UTC()
	required := stringSet(policy.RequiredPermissions)
	valid := make([]scored, 0, len(providers))
	totalRequests := uint64(0)
	states := make(map[string]ProviderHealth, len(providers))
	for _, p := range providers {
		h := r.state(capability, p.Name)
		states[p.Name] = h
		totalRequests += h.Requests
	}

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
		if policy.MaxRisk > 0 && p.Risk > policy.MaxRisk {
			continue
		}
		permissions := stringSet(p.Permissions)
		allowed := true
		for permission := range required {
			if _, ok := permissions[permission]; !ok {
				allowed = false
				break
			}
		}
		if !allowed {
			continue
		}

		h := states[p.Name]
		if h.CircuitOpenUntil.After(now) {
			continue
		}
		latency := p.Latency
		if h.EWMALatency > 0 {
			latency = h.EWMALatency
		}
		quality := p.Quality
		if h.EWMAQuality > 0 {
			quality = 0.45*p.Quality + 0.55*h.EWMAQuality
		}
		cost := p.Cost
		if h.EWMACost > 0 {
			cost = 0.45*p.Cost + 0.55*h.EWMACost
		}
		reliability := float64(h.Successes+2) / float64(h.Requests+4) // beta prior
		exploration := 0.0
		if r.config.ExplorationWeight > 0 {
			exploration = r.config.ExplorationWeight * math.Sqrt(math.Log(float64(totalRequests)+2)/float64(h.Requests+1))
		}
		score := 3.2*quality + 1.5*p.Privacy + 1.8*reliability + exploration - 1.2*cost - 0.05*latency.Seconds() - 0.75*float64(p.Risk) - 0.35*float64(h.ConsecutiveFailures)
		valid = append(valid, scored{provider: p, score: score})
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("adgo: no healthy provider satisfies policy for capability %q", capability)
	}
	sort.SliceStable(valid, func(i, j int) bool {
		if math.Abs(valid[i].score-valid[j].score) < 1e-9 {
			return valid[i].provider.Name < valid[j].provider.Name
		}
		return valid[i].score > valid[j].score
	})
	out := make([]Provider, 0, len(valid))
	for _, item := range valid {
		out = append(out, item.provider)
		if !policy.AllowFallback {
			break
		}
	}
	return out, nil
}

func (r *AdaptiveRouter) Resolve(ctx context.Context, capability string, policy ProviderPolicy) (Provider, error) {
	ranked, err := r.Ranked(ctx, capability, policy)
	if err != nil {
		return Provider{}, err
	}
	return ranked[0], nil
}

// Report feeds an observed provider result back into the router. A rate limit or
// repeated transient/permanent backend failure opens a bounded circuit. Invalid
// user input does not poison provider health.
func (r *AdaptiveRouter) Report(capability, provider string, duration time.Duration, result *ActivityResult, err error) {
	if capability == "" || provider == "" {
		return
	}
	key := providerHealthKey(capability, provider)
	now := time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.health[key]
	if h == nil {
		h = &ProviderHealth{Capability: capability, Provider: provider}
		r.health[key] = h
	}
	h.Requests++
	h.LastUpdated = now
	if duration > 0 {
		h.EWMALatency = ewmaDuration(h.EWMALatency, duration, r.config.EWMAAlpha)
	}
	if result != nil {
		if q := QualityUtility(result.Quality); q > 0 {
			h.EWMAQuality = ewmaFloat(h.EWMAQuality, q, r.config.EWMAAlpha)
		}
		if result.Budget.Cost > 0 {
			h.EWMACost = ewmaFloat(h.EWMACost, result.Budget.Cost, r.config.EWMAAlpha)
		}
	}
	if err == nil {
		h.Successes++
		h.ConsecutiveFailures = 0
		h.LastFailure = ""
		h.LastError = ""
		h.CircuitOpenUntil = time.Time{}
		return
	}

	class := DefaultClassify(err)
	h.Failures++
	h.LastFailure = class
	h.LastError = err.Error()
	if class == FailureInvalidInput {
		return
	}
	h.ConsecutiveFailures++
	if h.ConsecutiveFailures < r.config.FailureThreshold && class != FailureRateLimit {
		return
	}

	cooldown := r.config.BaseCooldown
	shift := h.ConsecutiveFailures - r.config.FailureThreshold
	if shift > 0 {
		for i := 0; i < shift && cooldown < r.config.MaxCooldown; i++ {
			cooldown *= 2
		}
	}
	if cooldown > r.config.MaxCooldown {
		cooldown = r.config.MaxCooldown
	}
	var failure *FailureError
	if errors.As(err, &failure) && failure.RetryAfter > cooldown {
		cooldown = failure.RetryAfter
	}
	h.CircuitOpenUntil = now.Add(cooldown)
}

func (r *AdaptiveRouter) Snapshot() []ProviderHealth {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProviderHealth, 0, len(r.health))
	for _, h := range r.health {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Capability == out[j].Capability {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Capability < out[j].Capability
	})
	return out
}

func (r *AdaptiveRouter) Reset(capability, provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.health, providerHealthKey(capability, provider))
}

func (r *AdaptiveRouter) ProviderForActivity(capability, activity string) (Provider, bool) {
	r.registry.mu.RLock()
	defer r.registry.mu.RUnlock()
	for _, p := range r.registry.capabilities[capability] {
		if p.Activity == activity {
			return p, true
		}
	}
	return Provider{}, false
}

func ewmaFloat(previous, current, alpha float64) float64 {
	if previous == 0 {
		return current
	}
	return alpha*current + (1-alpha)*previous
}

func ewmaDuration(previous, current time.Duration, alpha float64) time.Duration {
	if previous == 0 {
		return current
	}
	return time.Duration(alpha*float64(current) + (1-alpha)*float64(previous))
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}
