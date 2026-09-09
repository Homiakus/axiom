package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/profile"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("=== Axiom Production Reference Application ===")

	dataDir, err := os.MkdirTemp("", "axiom-production-refapp-*")
	if err != nil {
		log.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	plan, err := BuildPlan()
	if err != nil {
		log.Fatalf("compile plan: %v", err)
	}

	state := &SharedState{}

	// Setup cache and admission controllers
	cache, err := adgo.NewFileActivityCache(filepath.Join(dataDir, "cache"))
	if err != nil {
		log.Fatalf("create cache: %v", err)
	}
	admission, err := adgo.NewFileAdmissionController(filepath.Join(dataDir, "admission"))
	if err != nil {
		log.Fatalf("create admission controller: %v", err)
	}

	registry, err := BuildRegistry(state, nil, cache, admission)
	if err != nil {
		log.Fatalf("build registry: %v", err)
	}

	// Setup adaptive router with fallback
	routerCfg := adgo.DefaultRouterConfig()
	router := adgo.NewAdaptiveRouter(registry, routerCfg)

	// Update registry with wired router
	registry, err = BuildRegistry(state, router, cache, admission)
	if err != nil {
		log.Fatalf("build registry with router: %v", err)
	}

	// Initialize DurableSingleNode profile for synchronous Pebble persistence
	durableProfile, err := profile.OpenDurableSingleNode(profile.DurableSingleNodeConfig{
		Dir:        filepath.Join(dataDir, "durable-node"),
		SyncWrites: true,
	})
	if err != nil {
		log.Fatalf("open durable profile: %v", err)
	}
	defer durableProfile.Close()

	production, err := durableProfile.OpenAdgoProduction(plan, registry)
	if err != nil {
		log.Fatalf("open adgo production: %v", err)
	}
	defer production.Close()

	execID := fmt.Sprintf("deploy-incident-%d", time.Now().Unix())
	initialFacts := map[string]any{
		"targetService": "order-svc",
	}

	fmt.Printf("[1/6] Starting durable execution %s...\n", execID)
	execution, err := production.Engine.StartOrLoad(ctx, execID, initialFacts, adgo.BudgetLimit{MaxCost: 10.0})
	if err != nil {
		log.Fatalf("start execution: %v", err)
	}
	fmt.Printf("      Execution initialized: Status=%s Version=%d\n", execution.Status, execution.Version)

	// Start background resilient coordinator and worker pool
	serviceCtx, stopServices := context.WithCancel(ctx)
	defer stopServices()

	errChan := make(chan error, 2)
	go func() { errChan <- production.Engine.RunResilientCoordinator(serviceCtx) }()
	go func() {
		errChan <- production.Engine.RunWorker(serviceCtx, adgo.WorkerSpec{
			ID:          "prod-worker-primary",
			Concurrency: 4,
			LeaseTTL:    5 * time.Second,
		})
	}()

	// Monitor until human approval is needed
	fmt.Println("[2/6] Progressing through automated phases (retry, admission, speculation, quality gate)...")
	for {
		time.Sleep(100 * time.Millisecond)
		cur, err := production.Engine.Diagnostics(ctx, execID)
		if err != nil {
			log.Fatalf("diagnostics: %v", err)
		}
		if cur.Summary.Status == adgo.StatusHuman {
			fmt.Printf("      Awaiting human approval at node: %v\n", cur.Waiting)
			break
		}
		if cur.Summary.Status == adgo.StatusCompleted || cur.Summary.Status == adgo.StatusFailed {
			break
		}
	}

	// Operator approval
	fmt.Println("[3/6] Operator inspecting diagnostics and approving production deployment...")
	if _, err := production.Engine.ResolveHuman(ctx, execID, "human_approval", adgo.HumanResolution{
		Decision: adgo.HumanApprove,
		Actor:    "operator@axiom.internal",
		Reason:   "All canary metrics within acceptable baseline thresholds",
	}); err != nil {
		log.Fatalf("resolve human approval: %v", err)
	}

	// Wait for wait_verification node
	for {
		time.Sleep(50 * time.Millisecond)
		cur, err := production.Engine.Diagnostics(ctx, execID)
		if err != nil {
			log.Fatalf("diagnostics: %v", err)
		}
		if _, ok := cur.Waiting["wait_verification"]; ok {
			fmt.Println("[4/6] Workflow arrived at verification wait point. Emitting HealthVerifiedSignal...")
			if err := production.Engine.Signal(ctx, execID, adgo.Event{
				Type: "VerificationSignal",
			}); err != nil {
				log.Fatalf("signal: %v", err)
			}
			break
		}
		if cur.Summary.Status == adgo.StatusCompleted {
			break
		}
	}

	// Await completion
	fmt.Println("[5/6] Awaiting terminal workflow completion...")
	finalExecution, err := production.Engine.Await(ctx, execID, adgo.AwaitOptions{})
	if err != nil {
		log.Fatalf("await: %v", err)
	}

	stopServices()

	var deployRes string
	_ = json.Unmarshal(finalExecution.Data["deploymentResult"], &deployRes)

	fmt.Println("[6/6] Pipeline completed successfully!")
	fmt.Printf("      Final Status: %s\n", finalExecution.Status)
	fmt.Printf("      Total Cost:   $%.2f (bounded by budget $10.00)\n", finalExecution.BudgetUsage.Cost)
	fmt.Printf("      Deploy Result: %s\n", deployRes)
	fmt.Printf("      Quality Score: %.2f\n", adgo.QualityUtility(finalExecution.Quality))
	fmt.Printf("      History Steps: %d events recorded\n", len(finalExecution.History))
}
