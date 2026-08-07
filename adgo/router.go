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
	FailureThreshold  int
	BaseCooldown      time.Duration
	MaxCooldown       time.Duration
	EWMAAlpha         float64
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
// score. Exploration is only applied among already-valid providers. A router can
// use an optional ProviderHealthStore so circuit/reliability state survives
// process restarts and is shared by multiple coordinators.
type AdaptiveRouter struct {
	registry    *Registry
	config      RouterConfig
	healthStore ProviderHealthStore
	mu          sync.Mutex
	health      map[string]*ProviderHealth
}

func normalizeRouterConfig(config RouterConfig) RouterConfig {
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
	return config
}

func NewAdaptiveRouter(registry *Registry, config RouterConfig) *AdaptiveRouter {
	if registry == nil {
		registry = NewRegistry()
	}
	return &AdaptiveRouter{registry: registry, config: normalizeRouterConfig(config), health: map[string]*ProviderHealth{}}
}

func NewDurableAdaptiveRouter(registry *Registry, config RouterConfig, store ProviderHealthStore) *AdaptiveRouter {
	router := NewAdaptiveRouter(registry, config)
	router.healthStore = store
	return router
}

func providerHealthKey(capability, provider string) string { return capability + "\x00" + provider }

func (r *AdaptiveRouter) state(ctx context.Context, capability, provider string) (ProviderHealth, error) {
	if r.healthStore != nil {
		health, err := r.healthStore.LoadProviderHealth(ctx, capability, provider)
		if errors.Is(err, ErrProviderHealthNotFound) {
			return ProviderHealth{Capability: capability, Provider: provider}, nil
		}
		return health, err
	}
	key := providerHealthKey(capability, provider)
	r.mu.Lock()
	defer r.mu.Unlock()
	if health := r.health[key]; health != nil {
		return *health, nil
	}
	return ProviderHealth{Capability: capability, Provider: provider}, nil
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
	for _, provider := range providers {
		health, err := r.state(ctx, capability, provider.Name)
		if err != nil {
			return nil, err
		}
		states[provider.Name] = health
		totalRequests += health.Requests
	}

	for _, provider := range providers {
		if provider.Available != nil && !provider.Available(ctx) {
			continue
		}
		if policy.MinQuality > 0 && provider.Quality < policy.MinQuality {
			continue
		}
		if policy.MaxCost > 0 && provider.Cost > policy.MaxCost {
			continue
		}
		if policy.MaxLatency > 0 && provider.Latency > policy.MaxLatency {
			continue
		}
		if policy.MinPrivacy > 0 && provider.Privacy < policy.MinPrivacy {
			continue
		}
		if policy.MaxRisk > 0 && provider.Risk > policy.MaxRisk {
			continue
		}
		permissions := stringSet(provider.Permissions)
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

		health := states[provider.Name]
		if health.CircuitOpenUntil.After(now) {
			continue
		}
		latency := provider.Latency
		if health.EWMALatency > 0 {
			latency = health.EWMALatency
		}
		quality := provider.Quality
		if health.EWMAQuality > 0 {
			quality = 0.45*provider.Quality + 0.55*health.EWMAQuality
		}
		cost := provider.Cost
		if health.EWMACost > 0 {
			cost = 0.45*provider.Cost + 0.55*health.EWMACost
		}
		reliability := float64(health.Successes+2) / float64(health.Requests+4) // beta prior
		exploration := 0.0
		if r.config.ExplorationWeight > 0 {
			exploration = r.config.ExplorationWeight * math.Sqrt(math.Log(float64(totalRequests)+2)/float64(health.Requests+1))
		}
		score := 3.2*quality + 1.5*provider.Privacy + 1.8*reliability + exploration - 1.2*cost - 0.05*latency.Seconds() - 0.75*float64(provider.Risk) - 0.35*float64(health.ConsecutiveFailures)
		valid = append(valid, scored{provider: provider, score: score})
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

// Report feeds an observed provider result back into the router. The legacy
// convenience method intentionally cannot return persistence errors; production
// code that needs to surface them can call ReportContext.
func (r *AdaptiveRouter) Report(capability, provider string, duration time.Duration, result *ActivityResult, err error) {
	_ = r.ReportContext(context.Background(), capability, provider, duration, result, err)
}

func (r *AdaptiveRouter) ReportContext(ctx context.Context, capability, provider string, duration time.Duration, result *ActivityResult, observedErr error) error {
	if capability == "" || provider == "" {
		return nil
	}
	now := time.Now().UTC()
	mutate := func(health *ProviderHealth) {
		if health.Capability == "" {
			health.Capability = capability
			health.Provider = provider
		}
		health.Requests++
		health.LastUpdated = now
		if duration > 0 {
			health.EWMALatency = ewmaDuration(health.EWMALatency, duration, r.config.EWMAAlpha)
		}
		if result != nil {
			if quality := QualityUtility(result.Quality); quality > 0 {
				health.EWMAQuality = ewmaFloat(health.EWMAQuality, quality, r.config.EWMAAlpha)
			}
			if result.Budget.Cost > 0 {
				health.EWMACost = ewmaFloat(health.EWMACost, result.Budget.Cost, r.config.EWMAAlpha)
			}
		}
		if observedErr == nil {
			health.Successes++
			health.ConsecutiveFailures = 0
			health.LastFailure = ""
			health.LastError = ""
			health.CircuitOpenUntil = time.Time{}
			return
		}

		class := DefaultClassify(observedErr)
		health.Failures++
		health.LastFailure = class
		health.LastError = observedErr.Error()
		if class == FailureInvalidInput {
			return
		}
		health.ConsecutiveFailures++
		if health.ConsecutiveFailures < r.config.FailureThreshold && class != FailureRateLimit {
			return
		}
		cooldown := r.config.BaseCooldown
		shift := health.ConsecutiveFailures - r.config.FailureThreshold
		if shift > 0 {
			for i := 0; i < shift && cooldown < r.config.MaxCooldown; i++ {
				cooldown *= 2
			}
		}
		if cooldown > r.config.MaxCooldown {
			cooldown = r.config.MaxCooldown
		}
		var failure *FailureError
		if errors.As(observedErr, &failure) && failure.RetryAfter > cooldown {
			cooldown = failure.RetryAfter
		}
		health.CircuitOpenUntil = now.Add(cooldown)
	}

	if r.healthStore != nil {
		_, err := r.healthStore.UpdateProviderHealth(ctx, capability, provider, mutate)
		return err
	}
	key := providerHealthKey(capability, provider)
	r.mu.Lock()
	defer r.mu.Unlock()
	health := r.health[key]
	if health == nil {
		health = &ProviderHealth{Capability: capability, Provider: provider}
		r.health[key] = health
	}
	mutate(health)
	return nil
}

func (r *AdaptiveRouter) Snapshot() []ProviderHealth {
	values, _ := r.SnapshotContext(context.Background())
	return values
}

func (r *AdaptiveRouter) SnapshotContext(ctx context.Context) ([]ProviderHealth, error) {
	if r.healthStore != nil {
		return r.healthStore.ListProviderHealth(ctx)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProviderHealth, 0, len(r.health))
	for _, health := range r.health {
		out = append(out, *health)
	}
	sortProviderHealth(out)
	return out, nil
}

func (r *AdaptiveRouter) Reset(capability, provider string) {
	_ = r.ResetContext(context.Background(), capability, provider)
}

func (r *AdaptiveRouter) ResetContext(ctx context.Context, capability, provider string) error {
	if r.healthStore != nil {
		return r.healthStore.DeleteProviderHealth(ctx, capability, provider)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.health, providerHealthKey(capability, provider))
	return nil
}

func (r *AdaptiveRouter) ProviderForActivity(capability, activity string) (Provider, bool) {
	r.registry.mu.RLock()
	defer r.registry.mu.RUnlock()
	for _, provider := range r.registry.capabilities[capability] {
		if provider.Activity == activity {
			return provider, true
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
