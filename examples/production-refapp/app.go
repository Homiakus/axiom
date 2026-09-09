package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Homiakus/axiom/adgo"
)

// PipelineConfig defines runtime options and tunable limits for the reference pipeline.
type PipelineConfig struct {
	ExecutionID       string
	MaxBudgetCost     float64
	SimulateTransient bool
	SimulateDegraded  bool
	StorageDir        string
}

// DefaultPipelineConfig provides safe defaults for testing and execution.
func DefaultPipelineConfig(execID string) PipelineConfig {
	return PipelineConfig{
		ExecutionID:       execID,
		MaxBudgetCost:     10.0,
		SimulateTransient: true,
		SimulateDegraded:  true,
	}
}

// BuildPlan compiles the full production reference plan exercising:
// 1. Transient retry
// 2. Admission control & rate limits
// 3. Pure result cache & speculative hedging
// 4. Adaptive provider routing & fallback
// 5. Quality gate + targeted repair loop
// 6. Child workflow / subflow
// 7. External effect with idempotency & compensation
// 8. Human approval pause
// 9. Durable timer / signal
// 10. Final promotion
func BuildPlan() (*adgo.Plan, error) {
	return adgo.Compile(adgo.Definition{
		ID:                "production-reference-pipeline",
		Version:           "1",
		GlobalConcurrency: 8,
		InitialData:       []string{"targetService"},
		Nodes: []adgo.Node{
			{
				ID:            "fetch_telemetry",
				Kind:          adgo.NodeActivity,
				Activity:      "FetchTelemetry",
				Produces:      []string{"telemetry"},
				Requires:      []string{"targetService"},
				Retry:         adgo.RetryPolicy{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond, MaxRetryDuration: time.Minute, JitterFraction: 0.1},
				Next:          []adgo.Transition{{To: "acquire_admission"}},
				EstimatedCost: 0.1,
			},
			{
				ID:            "acquire_admission",
				Kind:          adgo.NodeActivity,
				Activity:      "AcquireAdmission",
				DependsOn:     []string{"fetch_telemetry"},
				Requires:      []string{"telemetry"},
				Produces:      []string{"admissionTicket"},
				Next:          []adgo.Transition{{To: "analyze_diagnostics"}},
				EstimatedCost: 0.05,
			},
			{
				ID:            "analyze_diagnostics",
				Kind:          adgo.NodeActivity,
				Activity:      "AnalyzeDiagnostics",
				DependsOn:     []string{"acquire_admission"},
				Requires:      []string{"admissionTicket", "telemetry"},
				Produces:      []string{"diagnosisReport"},
				Next:          []adgo.Transition{{To: "generate_patch"}},
				EstimatedCost: 0.25,
			},
			{
				ID:            "generate_patch",
				Kind:          adgo.NodeActivity,
				Activity:      "GeneratePatch",
				DependsOn:     []string{"analyze_diagnostics"},
				Requires:      []string{"diagnosisReport"},
				Produces:      []string{"patchContent", "patchErrors"},
				Loop: &adgo.LoopBound{
					MaxIterations: 3,
					MaxCost:       5.0,
					MaxDuration:   time.Minute,
					Epsilon:       0.01,
				},
				Next:          []adgo.Transition{{To: "quality_gate"}},
				EstimatedCost: 0.50,
			},
			{
				ID:        "quality_gate",
				Kind:      adgo.NodeGate,
				DependsOn: []string{"generate_patch"},
				Gate: &adgo.QualityGateSpec{
					HardFloors:        map[string]float64{"confidence": 0.90},
					MaxCriticalErrors: 0,
					RepairFrom:        []string{"generate_patch"},
				},
				Next: []adgo.Transition{
					{To: "audit_subflow", Outcome: adgo.OutcomePass},
				},
			},
			{
				ID:            "audit_subflow",
				Kind:          adgo.NodeSubflow,
				Activity:      "AuditSubflow",
				DependsOn:     []string{"quality_gate"},
				Requires:      []string{"patchContent"},
				Produces:      []string{"auditReceipt"},
				Next:          []adgo.Transition{{To: "provision_canary"}},
				EstimatedCost: 0.1,
			},
			{
				ID:             "provision_canary",
				Kind:           adgo.NodeActivity,
				Activity:       "ProvisionCanary",
				DependsOn:      []string{"audit_subflow"},
				Requires:       []string{"auditReceipt", "patchContent"},
				Produces:       []string{"canaryID"},
				ExternalEffect: true,
				IdempotencyKey: "canary-release-v1",
				Timeout:        30 * time.Second,
				Retry:          adgo.RetryPolicy{MaxAttempts: 2, BaseDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond, MaxRetryDuration: time.Minute, JitterFraction: 0.1},
				Compensation:   "RollbackCanary",
				Next:           []adgo.Transition{{To: "human_approval"}},
				EstimatedCost:  0.2,
			},
			{
				ID:        "human_approval",
				Kind:      adgo.NodeHuman,
				DependsOn: []string{"provision_canary"},
				Human: &adgo.HumanSpec{
					EventType: "ApproveProductionCutover",
					Risk:      adgo.RiskHigh,
				},
				Next: []adgo.Transition{{To: "wait_verification", Outcome: adgo.OutcomePass}},
			},
			{
				ID:        "wait_verification",
				Kind:      adgo.NodeWait,
				DependsOn: []string{"human_approval"},
				Wait: &adgo.WaitSpec{
					EventType: "VerificationSignal",
				},
				Next: []adgo.Transition{{To: "promote_production"}},
			},
			{
				ID:            "promote_production",
				Kind:          adgo.NodeActivity,
				Activity:      "PromoteProduction",
				DependsOn:     []string{"wait_verification"},
				Requires:      []string{"canaryID"},
				Produces:      []string{"deploymentResult"},
				EstimatedCost: 0.1,
			},
		},
	})
}

// SharedState provides observable counters to assert execution behavior in tests.
type SharedState struct {
	mu                sync.Mutex
	TelemetryAttempts atomic.Int32
	AdmissionCalls    atomic.Int32
	DiagnosticsCalls  atomic.Int32
	HedgePrimaryCalls atomic.Int32
	HedgeSpecCalls    atomic.Int32
	PatchAttempts     atomic.Int32
	AuditSubflowCalls atomic.Int32
	CanaryProvisions  atomic.Int32
	CanaryRollbacks   atomic.Int32
	PromoteCalls      atomic.Int32
	CompensatedIDs    []string
}

// BuildRegistry wires all activity, subflow, gate, and compensation handlers.
func BuildRegistry(state *SharedState, router *adgo.AdaptiveRouter, cache adgo.ActivityCache, admission adgo.AdmissionController) (*adgo.Registry, error) {
	registry := adgo.NewRegistry()

	// Register providers for adaptive routing demonstration
	registry.Provider("ai_reasoning", adgo.Provider{
		Name:     "primary-llm",
		Activity: "PrimaryLLM",
		Quality:  0.95,
		Cost:     0.50,
	})
	registry.Provider("ai_reasoning", adgo.Provider{
		Name:     "fallback-llm",
		Activity: "FallbackLLM",
		Quality:  0.85,
		Cost:     0.20,
	})

	// 1. FetchTelemetry: simulates transient failure on attempt 1, succeeds on retry
	registry.Activity("FetchTelemetry", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		attempt := state.TelemetryAttempts.Add(1)
		if attempt == 1 {
			return adgo.ActivityResult{}, adgo.Fail(adgo.FailureTransient, errors.New("simulated transient network timeout contacting cluster metrics agent"))
		}
		return adgo.ActivityResult{
			Facts: map[string]any{
				"telemetry": map[string]any{
					"service":   "order-svc",
					"errorRate": 0.14,
					"p99":       420.0,
				},
			},
			Budget: adgo.BudgetUsage{Cost: 0.10},
		}, nil
	})

	// 2. AcquireAdmission: integrates with ADGO AdmissionController
	registry.Activity("AcquireAdmission", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		state.AdmissionCalls.Add(1)
		if admission != nil {
			lease, err := admission.Acquire(ctx, "canary_quota", adgo.AdmissionPolicy{
				MaxConcurrent: 5,
				Rate:          100,
				Period:        time.Second,
				Burst:         10,
			}, 30*time.Second)
			if err != nil {
				return adgo.ActivityResult{}, adgo.Fail(adgo.FailureRateLimit, err)
			}
			defer func() { _ = admission.Release(ctx, lease) }()
		}
		return adgo.ActivityResult{
			Facts: map[string]any{
				"admissionTicket": "admit-tok-42",
			},
			Budget: adgo.BudgetUsage{Cost: 0.05},
		}, nil
	})

	// 3. AnalyzeDiagnostics: pure result caching + speculative hedging
	hedgedAnalysis, err := adgo.NewHedgedActivity([]adgo.ActivityVariant{
		{
			Name: "fast_heuristic",
			Handler: func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
				state.HedgePrimaryCalls.Add(1)
				return adgo.ActivityResult{
					Facts:   map[string]any{"method": "fast_heuristic"},
					Quality: adgo.QualityVector{"confidence": 0.92},
					Budget:  adgo.BudgetUsage{Cost: 0.10},
				}, nil
			},
		},
		{
			Name: "deep_ensemble",
			Handler: func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
				state.HedgeSpecCalls.Add(1)
				return adgo.ActivityResult{
					Facts:   map[string]any{"method": "deep_ensemble"},
					Quality: adgo.QualityVector{"confidence": 0.96},
					Budget:  adgo.BudgetUsage{Cost: 0.20},
				}, nil
			},
		},
	}, adgo.SpeculationPolicy{
		Pure:       true,
		HedgeDelay: 50 * time.Millisecond,
		MinQuality: 0.90,
	})
	if err != nil {
		return nil, fmt.Errorf("build hedged activity: %w", err)
	}

	registry.Activity("AnalyzeDiagnostics", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		state.DiagnosticsCalls.Add(1)
		cacheKey := "diag:order-svc:v1"
		if cache != nil {
			if cached, err := cache.Get(ctx, cacheKey); err == nil && cached != nil {
				return cached.Result, nil
			}
		}

		res, err := hedgedAnalysis(ctx, req)
		if err != nil {
			return res, err
		}
		if res.Facts == nil {
			res.Facts = map[string]any{}
		}
		res.Facts["diagnosisReport"] = "degraded_connection_pool"

		if cache != nil {
			lease, claimErr := cache.Claim(ctx, cacheKey, time.Minute)
			if claimErr == nil {
				_ = cache.Put(ctx, lease, res, time.Minute)
			}
		}
		return res, nil
	})

	// 4. GeneratePatch: exercises adaptive provider routing & targeted repair loop
	registry.Activity("GeneratePatch", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		iteration := state.PatchAttempts.Add(1)

		if router != nil {
			_, _ = router.Resolve(ctx, "ai_reasoning", adgo.ProviderPolicy{
				AllowFallback: true,
			})
		}

		// On first attempt, generate low confidence to trigger QualityGate repair loop
		if iteration == 1 {
			return adgo.ActivityResult{
				Facts: map[string]any{
					"patchContent": "max_connections = 50",
					"patchErrors":  1,
				},
				Quality: adgo.QualityVector{"confidence": 0.75}, // below 0.90 floor!
				Budget:  adgo.BudgetUsage{Cost: 0.25},
			}, nil
		}

		// On repair iteration, generate high confidence passing QualityGate
		return adgo.ActivityResult{
			Facts: map[string]any{
				"patchContent": "max_connections = 100; timeout = 5s",
				"patchErrors":  0,
			},
			Quality: adgo.QualityVector{"confidence": 0.98}, // meets floor!
			Budget:  adgo.BudgetUsage{Cost: 0.25},
		}, nil
	})

	// 5. AuditSubflow: child workflow handler
	registry.Subflow("AuditSubflow", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		state.AuditSubflowCalls.Add(1)
		return adgo.ActivityResult{
			Facts: map[string]any{
				"auditReceipt": fmt.Sprintf("audit-rec-%d", time.Now().UnixNano()),
			},
			Budget: adgo.BudgetUsage{Cost: 0.05},
		}, nil
	})

	// 6. ProvisionCanary: external effect with idempotency key and compensation registration
	registry.Activity("ProvisionCanary", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		state.CanaryProvisions.Add(1)
		canaryID := fmt.Sprintf("canary-%s", req.IdempotencyKey)
		return adgo.ActivityResult{
			Facts: map[string]any{
				"canaryID": canaryID,
			},
			Budget: adgo.BudgetUsage{Cost: 0.15},
		}, nil
	})

	// 7. RollbackCanary: compensation handler for ProvisionCanary
	registry.Compensation("RollbackCanary", func(ctx context.Context, req adgo.ActivityRequest) error {
		state.CanaryRollbacks.Add(1)
		state.mu.Lock()
		state.CompensatedIDs = append(state.CompensatedIDs, req.ExecutionID)
		state.mu.Unlock()
		return nil
	})

	// 8. PromoteProduction: final deployment
	registry.Activity("PromoteProduction", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		state.PromoteCalls.Add(1)
		return adgo.ActivityResult{
			Facts: map[string]any{
				"deploymentResult": "SUCCESS_PROMOTED",
			},
			Budget: adgo.BudgetUsage{Cost: 0.10},
		}, nil
	})

	return registry, nil
}
