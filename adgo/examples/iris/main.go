package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Homiakus/axiom/adgo"
)

func main() {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "axiom-adgo-iris-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root)

	plan, err := adgo.Compile(irisDefinition())
	if err != nil {
		log.Fatal(err)
	}
	store, err := adgo.NewFileStore(filepath.Join(root, "runtime"))
	if err != nil {
		log.Fatal(err)
	}
	artifacts, err := adgo.NewContentAddressedStore(filepath.Join(root, "artifacts"))
	if err != nil {
		log.Fatal(err)
	}
	registry := irisRegistry(artifacts)
	runtime, err := adgo.NewRuntime(plan, store, registry)
	if err != nil {
		log.Fatal(err)
	}

	_, err = runtime.Start(ctx, "article-42", map[string]any{
		"brief": "Durable orchestration for AI content systems",
	}, adgo.BudgetLimit{MaxCost: 5, MaxLLMCalls: 20, MaxSearchQueries: 20})
	if err != nil {
		log.Fatal(err)
	}

	// First run intentionally stops before a high-risk publication side effect.
	exec, err := runtime.Run(ctx, "article-42")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("before approval: status=%s repairs=%d\n", exec.Status, exec.Metrics.Repairs)
	if exec.Status != adgo.StatusHuman {
		log.Fatalf("expected human approval, got %s", exec.Status)
	}

	// The approval is a durable event. It can arrive minutes or days later, or
	// after the process has restarted on another worker sharing the same store.
	if err := runtime.Signal(ctx, exec.ID, adgo.Event{
		ID:         "approve-publication-1",
		Type:       "Approved",
		TargetNode: "publish",
	}); err != nil {
		log.Fatal(err)
	}
	exec, err = runtime.Run(ctx, exec.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("after approval:  status=%s quality=%.3f cost=%.2f activities=%d\n",
		exec.Status, adgo.QualityUtility(exec.Quality), exec.BudgetUsage.Cost, exec.Metrics.Activities)

	explanation := adgo.Explain(plan, exec, "quality_gate")
	pretty, _ := json.MarshalIndent(explanation, "", "  ")
	fmt.Printf("quality gate explanation:\n%s\n", pretty)
}

func irisDefinition() adgo.Definition {
	repairBound := &adgo.LoopBound{
		MaxIterations: 3,
		MaxCost:       2,
		MaxDuration:   10 * time.Minute,
		Epsilon:       0.001,
	}
	return adgo.Definition{
		ID:                 "iris.article",
		Version:            "1",
		InitialData:        []string{"brief"},
		AllowedPermissions: []string{"network", "publish"},
		GlobalConcurrency:  4,
		CapabilityLimits:   map[string]int{"FindEvidence": 3},
		Nodes: []adgo.Node{
			{
				ID: "briefing", Kind: adgo.NodeActivity, Activity: "Briefing",
				Requires: []string{"brief"}, Produces: []string{"researchQuestion"},
				Next: []adgo.Transition{{To: "search"}},
			},
			{
				ID: "search", Kind: adgo.NodeActivity, Capability: "FindEvidence",
				Requires: []string{"researchQuestion"}, Produces: []string{"sourceManifest"},
				RequiredPermissions: []string{"network"},
				Next:                []adgo.Transition{{To: "extract"}},
			},
			{
				ID: "extract", Kind: adgo.NodeActivity, Activity: "Extract",
				DependsOn: []string{"search"}, Requires: []string{"sourceManifest"}, Produces: []string{"evidence"},
				Next: []adgo.Transition{{To: "outline"}},
			},
			{
				ID: "outline", Kind: adgo.NodeActivity, Activity: "Outline",
				DependsOn: []string{"extract"}, Requires: []string{"evidence"}, Produces: []string{"outline"},
				Next: []adgo.Transition{{To: "draft"}},
			},
			{
				ID: "draft", Kind: adgo.NodeActivity, Activity: "Draft",
				DependsOn: []string{"outline"}, Requires: []string{"outline", "evidence"}, Produces: []string{"draft", "draftRevision"},
				Loop: repairBound, EstimatedCost: 0.25, ExpectedQualityGain: 0.20,
				Next: []adgo.Transition{{To: "factcheck"}, {To: "editorial"}},
			},
			{
				ID: "factcheck", Kind: adgo.NodeActivity, Activity: "FactCheck",
				DependsOn: []string{"draft"}, Requires: []string{"draft", "evidence"}, Produces: []string{"criticalErrors"},
				EstimatedCost: 0.10, ExpectedQualityGain: 0.12,
				Next: []adgo.Transition{{To: "verify"}},
			},
			{
				ID: "editorial", Kind: adgo.NodeActivity, Activity: "Editorial",
				DependsOn: []string{"draft"}, Requires: []string{"draft"}, Produces: []string{"editedDraft"},
				EstimatedCost: 0.10, ExpectedQualityGain: 0.08,
				Next: []adgo.Transition{{To: "export_join"}},
			},
			{
				ID: "verify", Kind: adgo.NodeActivity, Activity: "Verify",
				DependsOn: []string{"factcheck"}, Requires: []string{"criticalErrors"},
				Next: []adgo.Transition{{To: "quality_gate"}},
			},
			{
				ID: "quality_gate", Kind: adgo.NodeGate,
				DependsOn: []string{"verify"},
				Gate: &adgo.QualityGateSpec{
					HardFloors:        map[string]float64{"factuality": 0.95, "evidenceCoverage": 0.90},
					MaxCriticalErrors: 0,
					RepairFrom:        []string{"draft"},
				},
				Next: []adgo.Transition{{To: "export_join", Outcome: adgo.OutcomePass}},
			},
			{
				ID: "export_join", Kind: adgo.NodeJoin,
				DependsOn: []string{"quality_gate", "editorial"}, Join: &adgo.JoinSpec{Mode: adgo.JoinAll},
				Next: []adgo.Transition{{To: "export"}},
			},
			{
				ID: "export", Kind: adgo.NodeActivity, Activity: "Export",
				DependsOn: []string{"export_join"}, Requires: []string{"editedDraft"}, Produces: []string{"export"},
				Next: []adgo.Transition{{To: "publish"}},
			},
			{
				ID: "publish", Kind: adgo.NodeActivity, Activity: "Publish",
				DependsOn: []string{"export"}, Requires: []string{"export"},
				RequiredPermissions: []string{"publish"}, Risk: adgo.RiskHigh,
				ExternalEffect: true, Timeout: 5 * time.Second,
				Retry:          adgo.RetryPolicy{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second, MaxRetryDuration: 30 * time.Second, JitterFraction: 0.1},
				IdempotencyKey: "{execution}:{node}", Compensation: "Unpublish",
			},
		},
	}
}

func irisRegistry(artifacts *adgo.ContentAddressedStore) *adgo.Registry {
	registry := adgo.NewRegistry()

	registry.Activity("Briefing", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"researchQuestion": "How should a durable adaptive orchestrator behave?"}}, nil
	})
	registry.Provider("FindEvidence", adgo.Provider{
		Name: "web", Activity: "SearchWeb", Quality: 0.95, Cost: 0.08, Latency: 100 * time.Millisecond,
		Privacy: 0.8, Risk: adgo.RiskLow, Permissions: []string{"network"},
	})
	registry.Activity("SearchWeb", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{
			Facts:  map[string]any{"sourceManifest": []string{"temporal", "durable-task", "step-functions"}},
			Budget: adgo.BudgetUsage{Cost: 0.08, SearchQueries: 1},
		}, nil
	})
	registry.Activity("Extract", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"evidence": []string{"durable history", "idempotent activities", "bounded repair"}}}, nil
	})
	registry.Activity("Outline", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"outline": []string{"control plane", "execution plane", "repair"}}}, nil
	})
	registry.Activity("Draft", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		// Attempts survive targeted invalidation and therefore form a compact
		// orchestration revision counter without keeping large draft state inline.
		revision := req.Attempt
		body := fmt.Sprintf("ADGO article draft revision %d", revision)
		ref, err := artifacts.Put("draft.txt", "text/plain", strings.NewReader(body))
		if err != nil {
			return adgo.ActivityResult{}, err
		}
		return adgo.ActivityResult{
			Facts:     map[string]any{"draft": ref.URI, "draftRevision": revision},
			Artifacts: map[string]adgo.ArtifactRef{"draft": ref},
			Quality:   adgo.QualityVector{"structure": 0.90, "clarity": 0.88},
			Budget:    adgo.BudgetUsage{Cost: 0.25, LLMCalls: 1, Tokens: 1200},
			Signature: fmt.Sprintf("draft-r%d", revision),
		}, nil
	})
	registry.Activity("FactCheck", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		var revision int
		_ = json.Unmarshal(req.Data["draftRevision"], &revision)
		critical := 1
		factuality := 0.86
		coverage := 0.88
		if revision >= 2 {
			critical = 0
			factuality = 0.98
			coverage = 0.96
		}
		return adgo.ActivityResult{
			Facts:   map[string]any{"criticalErrors": critical},
			Quality: adgo.QualityVector{"factuality": factuality, "evidenceCoverage": coverage},
			Budget:  adgo.BudgetUsage{Cost: 0.10, LLMCalls: 1, Tokens: 400},
		}, nil
	})
	registry.Activity("Editorial", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{
			Facts:   map[string]any{"editedDraft": "artifact://editorial/final"},
			Quality: adgo.QualityVector{"style": 0.91, "clarity": 0.93},
			Budget:  adgo.BudgetUsage{Cost: 0.10, LLMCalls: 1, Tokens: 500},
		}, nil
	})
	registry.Activity("Verify", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{}, nil
	})
	registry.Activity("Export", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"export": "artifact://exports/article-42.md"}}, nil
	})
	registry.Activity("Publish", func(_ context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		fmt.Printf("publish once with idempotency key %q\n", req.IdempotencyKey)
		return adgo.ActivityResult{}, nil
	})
	registry.Compensation("Unpublish", func(_ context.Context, req adgo.ActivityRequest) error {
		fmt.Printf("compensate publication with key %q\n", req.IdempotencyKey)
		return nil
	})
	return registry
}
