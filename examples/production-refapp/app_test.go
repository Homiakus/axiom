package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/profile"
)

// TestProductionRefApp_DeterministicEndToEnd executes the full reference application
// locally, testing composition of:
// - Transient retry (FetchTelemetry attempt 1 fails, attempt 2 succeeds)
// - Admission control & rate limits (AcquireAdmission)
// - Pure result cache & speculative hedging (AnalyzeDiagnostics)
// - Adaptive routing & fallback (GeneratePatch)
// - Quality gate + targeted repair loop (QualityGate repairs iteration 1 to iteration 2)
// - Child workflow / subflow execution (AuditSubflow)
// - External effect with idempotency & compensation (ProvisionCanary)
// - Human approval pause & resume (HumanApprove)
// - Durable timer / signal event (VerificationSignal)
// - Final promotion & bounded budget assertions
// - Expected history assertions
func TestProductionRefApp_DeterministicEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	plan, err := BuildPlan()
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	state := &SharedState{}
	cache := adgo.NewMemoryActivityCache()
	admission := adgo.NewMemoryAdmissionController()

	routerCfg := adgo.DefaultRouterConfig()
	router := adgo.NewAdaptiveRouter(nil, routerCfg)

	registry, err := BuildRegistry(state, router, cache, admission)
	if err != nil {
		t.Fatalf("BuildRegistry failed: %v", err)
	}

	cfg := adgo.DefaultProductionConfig("")
	cfg.Backend = adgo.BackendMemory

	production, err := adgo.OpenProduction(plan, registry, cfg)
	if err != nil {
		t.Fatalf("OpenProduction failed: %v", err)
	}
	defer production.Close()

	execID := "test-e2e-exec-001"
	initialFacts := map[string]any{"targetService": "payment-gateway"}
	budgetLimit := adgo.BudgetLimit{MaxCost: 5.0}

	execution, err := production.Engine.Start(ctx, execID, initialFacts, budgetLimit)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if execution.Status != adgo.StatusRunning {
		t.Fatalf("expected running, got %s", execution.Status)
	}

	serviceCtx, stopServices := context.WithCancel(ctx)
	defer stopServices()

	go func() { _ = production.Engine.RunResilientCoordinator(serviceCtx) }()
	go func() {
		_ = production.Engine.RunWorker(serviceCtx, adgo.WorkerSpec{
			ID:          "worker-e2e",
			Concurrency: 2,
			LeaseTTL:    time.Minute,
		})
	}()

	// 1. Wait for human approval pause
	assertCondition(t, 5*time.Second, 100*time.Millisecond, func() bool {
		cur, err := production.Engine.Diagnostics(ctx, execID)
		if err == nil && cur.Summary.Status == adgo.StatusHuman {
			return true
		}
		if err == nil {
			t.Logf("diag: status=%s failure=%s waiting=%v ready=%v active=%+v state=(telemetry=%d admission=%d diag=%d patch=%d canary=%d)",
				cur.Summary.Status, cur.Summary.Failure, cur.Waiting, cur.Ready, cur.ActiveTasks,
				state.TelemetryAttempts.Load(), state.AdmissionCalls.Load(), state.DiagnosticsCalls.Load(), state.PatchAttempts.Load(), state.CanaryProvisions.Load())
		}
		return false
	}, "workflow did not reach StatusHuman")

	// Verify transient retry occurred
	if attempts := state.TelemetryAttempts.Load(); attempts < 2 {
		t.Fatalf("expected at least 2 telemetry attempts (transient retry), got %d", attempts)
	}

	// Verify admission control was called
	if calls := state.AdmissionCalls.Load(); calls < 1 {
		t.Fatalf("expected admission controller invocation, got %d", calls)
	}

	// Verify quality gate caused repair: patch attempts must be >= 2
	if patchAttempts := state.PatchAttempts.Load(); patchAttempts < 2 {
		t.Fatalf("expected at least 2 patch attempts (quality gate repair), got %d", patchAttempts)
	}

	// Verify child subflow was executed
	if subflows := state.AuditSubflowCalls.Load(); subflows < 1 {
		t.Fatalf("expected child subflow invocation, got %d", subflows)
	}

	// Verify canary was provisioned
	if provisions := state.CanaryProvisions.Load(); provisions < 1 {
		t.Fatalf("expected canary provision, got %d", provisions)
	}

	// 2. Resolve human approval
	if _, err := production.Engine.ResolveHuman(ctx, execID, "human_approval", adgo.HumanResolution{
		Decision: adgo.HumanApprove,
		Actor:    "lead-sre",
		Reason:   "Confidence verified > 0.95",
	}); err != nil {
		t.Fatalf("ResolveHuman failed: %v", err)
	}

	// 3. Wait for wait_verification node
	assertCondition(t, 15*time.Second, 50*time.Millisecond, func() bool {
		cur, err := production.Engine.Diagnostics(ctx, execID)
		if err != nil {
			return false
		}
		_, ok := cur.Waiting["wait_verification"]
		return ok
	}, "workflow did not reach wait_verification")

	// 4. Emit durable signal
	if err := production.Engine.Signal(ctx, execID, adgo.Event{
		Type: "VerificationSignal",
	}); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}

	// 5. Await terminal completion
	completed, err := production.Engine.Await(ctx, execID, adgo.AwaitOptions{})
	if err != nil {
		t.Fatalf("Await failed: %v", err)
	}
	if completed.Status != adgo.StatusCompleted {
		t.Fatalf("expected completed status, got %s", completed.Status)
	}

	// 6. Assertions on budget, history, and results
	if completed.BudgetUsage.Cost > budgetLimit.MaxCost {
		t.Fatalf("budget exceeded: cost=%.2f limit=%.2f", completed.BudgetUsage.Cost, budgetLimit.MaxCost)
	}
	var deployResult string
	_ = json.Unmarshal(completed.Data["deploymentResult"], &deployResult)
	if deployResult != "SUCCESS_PROMOTED" {
		t.Fatalf("unexpected deployment result: %q", deployResult)
	}

	// Verify history contains all expected checkpoints
	expectedEvents := []string{
		"execution_started",
		"activity_completed",
		"repair_planned",
		"human_resolved",
		"execution_completed",
	}
	historyTypes := map[string]int{}
	for _, h := range completed.History {
		historyTypes[h.Type]++
	}
	for _, ev := range expectedEvents {
		if historyTypes[ev] == 0 {
			t.Errorf("expected history event %q was not found in execution history", ev)
		}
	}
}

// TestProductionRefApp_CrashRecoveryCheckpoint tests restart resilience:
// 1. Starts execution in Pebble-backed store
// 2. Runs until human approval checkpoint
// 3. Closes runtime (simulating crash)
// 4. Reopens runtime from the same directory using StartOrLoad
// 5. Verifies all prior state, data, and version are intact
// 6. Successfully finishes execution to completion
func TestProductionRefApp_CrashRecoveryCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	pebbleDir := filepath.Join(tempDir, "pebble-durable")

	plan, err := BuildPlan()
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	state := &SharedState{}
	registry, err := BuildRegistry(state, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildRegistry failed: %v", err)
	}

	execID := "crash-recovery-test-1"

	// PHASE 1: Open DurableSingleNode, start workflow and advance to human approval
	func() {
		node, err := profile.OpenDurableSingleNode(profile.DurableSingleNodeConfig{
			Dir:        pebbleDir,
			SyncWrites: true,
		})
		if err != nil {
			t.Fatalf("OpenDurableSingleNode failed: %v", err)
		}
		defer node.Close()

		prod, err := node.OpenAdgoProduction(plan, registry)
		if err != nil {
			t.Fatalf("OpenAdgoProduction failed: %v", err)
		}
		defer prod.Close()

		if _, err := prod.Engine.Start(ctx, execID, map[string]any{"targetService": "auth-svc"}, adgo.BudgetLimit{MaxCost: 10.0}); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		serviceCtx, stop := context.WithCancel(ctx)
		defer stop()

		coordErrCh := make(chan error, 1)
		workerErrCh := make(chan error, 1)
		go func() { coordErrCh <- prod.Engine.RunResilientCoordinator(serviceCtx) }()
		go func() {
			workerErrCh <- prod.Engine.RunWorker(serviceCtx, adgo.WorkerSpec{
				ID:          "worker-pre-crash",
				Concurrency: 2,
				LeaseTTL:    time.Second,
			})
		}()

		// Wait until execution reaches human approval
		assertCondition(t, 15*time.Second, 100*time.Millisecond, func() bool {
			select {
			case err := <-coordErrCh:
				t.Fatalf("coordinator exited early: %v", err)
			case err := <-workerErrCh:
				t.Fatalf("worker exited early: %v", err)
			default:
			}
			diag, err := prod.Engine.Diagnostics(ctx, execID)
			if err == nil && diag.Summary.Status == adgo.StatusHuman {
				return true
			}
			if err == nil {
				t.Logf("pre-crash diag: status=%s failure=%s waiting=%v ready=%v active=%+v state=(telemetry=%d admission=%d diag=%d patch=%d canary=%d)",
					diag.Summary.Status, diag.Summary.Failure, diag.Waiting, diag.Ready, diag.ActiveTasks,
					state.TelemetryAttempts.Load(), state.AdmissionCalls.Load(), state.DiagnosticsCalls.Load(), state.PatchAttempts.Load(), state.CanaryProvisions.Load())
			} else {
				t.Logf("diagnostics err: %v", err)
			}
			return false
		}, "did not reach StatusHuman before crash")
	}()

	// PHASE 2: Reopen runtime from the exact same storage directory (simulating restart)
	nodeReopened, err := profile.OpenDurableSingleNode(profile.DurableSingleNodeConfig{
		Dir:        pebbleDir,
		SyncWrites: true,
	})
	if err != nil {
		t.Fatalf("reopen DurableSingleNode failed: %v", err)
	}
	defer nodeReopened.Close()

	prodReopened, err := nodeReopened.OpenAdgoProduction(plan, registry)
	if err != nil {
		t.Fatalf("reopen AdgoProduction failed: %v", err)
	}
	defer prodReopened.Close()

	// Load recovered execution
	loaded, err := prodReopened.Engine.StartOrLoad(ctx, execID, nil, adgo.BudgetLimit{})
	if err != nil {
		t.Fatalf("StartOrLoad failed: %v", err)
	}
	if loaded.Status != adgo.StatusHuman {
		t.Fatalf("expected recovered status %s, got %s", adgo.StatusHuman, loaded.Status)
	}
	var targetSvc string
	_ = json.Unmarshal(loaded.Data["targetService"], &targetSvc)
	if targetSvc != "auth-svc" {
		t.Fatalf("lost targetService fact after restart: %v", targetSvc)
	}

	// Resume workers & coordinator on restarted node
	serviceCtx2, stop2 := context.WithCancel(ctx)
	defer stop2()

	go func() { _ = prodReopened.Engine.RunResilientCoordinator(serviceCtx2) }()
	go func() {
		_ = prodReopened.Engine.RunWorker(serviceCtx2, adgo.WorkerSpec{
			ID:          "worker-post-crash",
			Concurrency: 2,
			LeaseTTL:    time.Second,
		})
	}()

	// Resolve human approval on reopened engine
	if _, err := prodReopened.Engine.ResolveHuman(ctx, execID, "human_approval", adgo.HumanResolution{
		Decision: adgo.HumanApprove,
		Actor:    "restarted-sre",
	}); err != nil {
		t.Fatalf("reopened ResolveHuman failed: %v", err)
	}

	// Wait for wait_verification
	assertCondition(t, 15*time.Second, 50*time.Millisecond, func() bool {
		cur, err := prodReopened.Engine.Diagnostics(ctx, execID)
		if err != nil {
			return false
		}
		_, ok := cur.Waiting["wait_verification"]
		return ok
	}, "workflow did not reach wait_verification post-restart")

	// Emit signal
	if err := prodReopened.Engine.Signal(ctx, execID, adgo.Event{
		Type: "VerificationSignal",
	}); err != nil {
		t.Fatalf("Signal failed: %v", err)
	}

	// Verify clean completion post-restart
	final, err := prodReopened.Engine.Await(ctx, execID, adgo.AwaitOptions{})
	if err != nil {
		t.Fatalf("final await failed: %v", err)
	}
	if final.Status != adgo.StatusCompleted {
		t.Fatalf("expected completed post-restart, got %s", final.Status)
	}
}

// TestProductionRefApp_WorkerFencing proves that a stale/expired worker cannot
// commit results after being superseded or fenced by lease expiry.
func TestProductionRefApp_WorkerFencing(t *testing.T) {
	ctx := context.Background()
	plan, err := adgo.Compile(adgo.Definition{
		ID:      "fencing-test",
		Version: "1",
		Nodes: []adgo.Node{
			{ID: "task1", Kind: adgo.NodeActivity, Activity: "SimpleTask"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	store := adgo.NewMemoryStore()
	registry := adgo.NewRegistry()
	registry.Activity("SimpleTask", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{Facts: map[string]any{"done": true}}, nil
	})

	engine, err := adgo.NewEngine(plan, store, registry, adgo.WithEngineLeaseTTL(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Start(ctx, "wf-fence", nil, adgo.BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "wf-fence"); err != nil {
		t.Fatal(err)
	}

	// Worker A claims task
	staleWork, err := engine.Poll(ctx, adgo.WorkerSpec{ID: "worker-A", LeaseTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate lease expiration in store
	current, err := store.Load(ctx, "wf-fence")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(ctx, current.ID, current.Version, func(x *adgo.Execution) error {
		task := x.ActiveTasks[staleWork.Token.TaskID]
		task.LeaseUntil = time.Now().UTC().Add(-time.Hour)
		x.ActiveTasks[task.ID] = task
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Advance reclaims task and makes it available to attempt 2
	if _, err := engine.Advance(ctx, "wf-fence"); err != nil {
		t.Fatal(err)
	}

	newWork, err := engine.Poll(ctx, adgo.WorkerSpec{ID: "worker-B", LeaseTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if newWork.Token.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", newWork.Token.Attempt)
	}

	// Stale worker A tries to complete -> must fail with ErrStaleTask
	if _, err := engine.Complete(ctx, staleWork.Token, adgo.ActivityResult{}, time.Millisecond); !errors.Is(err, adgo.ErrStaleTask) {
		t.Fatalf("expected ErrStaleTask for fenced worker A, got %v", err)
	}

	// Worker B completes successfully
	if _, err := engine.Complete(ctx, newWork.Token, adgo.ActivityResult{}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

// TestProductionRefApp_CompensationOnAbort verifies that when human rejects or execution
// fails after an external effect, the compensation stack unwinds cleanly.
func TestProductionRefApp_CompensationOnAbort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	plan, err := BuildPlan()
	if err != nil {
		t.Fatal(err)
	}

	state := &SharedState{}
	registry, err := BuildRegistry(state, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	cfg := adgo.DefaultProductionConfig("")
	cfg.Backend = adgo.BackendMemory

	prod, err := adgo.OpenProduction(plan, registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer prod.Close()

	execID := "compensate-test-1"
	if _, err := prod.Engine.Start(ctx, execID, map[string]any{"targetService": "billing"}, adgo.BudgetLimit{}); err != nil {
		t.Fatal(err)
	}

	serviceCtx, stop := context.WithCancel(ctx)
	defer stop()

	coordErrCh := make(chan error, 1)
	workerErrCh := make(chan error, 1)
	go func() { coordErrCh <- prod.Engine.RunResilientCoordinator(serviceCtx) }()
	go func() {
		workerErrCh <- prod.Engine.RunWorker(serviceCtx, adgo.WorkerSpec{
			ID:          "worker-comp",
			Concurrency: 2,
			LeaseTTL:    time.Second,
		})
	}()

	// Wait for human approval pause (canary is already provisioned and in compensation stack)
	assertCondition(t, 15*time.Second, 100*time.Millisecond, func() bool {
		select {
		case err := <-coordErrCh:
			t.Fatalf("coordinator exited early: %v", err)
		case err := <-workerErrCh:
			t.Fatalf("worker exited early: %v", err)
		default:
		}
		diag, err := prod.Engine.Diagnostics(ctx, execID)
		if err == nil && diag.Summary.Status == adgo.StatusHuman {
			return true
		}
		if err == nil {
			t.Logf("comp diag: status=%s failure=%s waiting=%v ready=%v active=%+v state=(telemetry=%d admission=%d diag=%d patch=%d canary=%d)",
				diag.Summary.Status, diag.Summary.Failure, diag.Waiting, diag.Ready, diag.ActiveTasks,
				state.TelemetryAttempts.Load(), state.AdmissionCalls.Load(), state.DiagnosticsCalls.Load(), state.PatchAttempts.Load(), state.CanaryProvisions.Load())
		}
		return false
	}, "did not reach StatusHuman")

	// Human operator rejects and cancels deployment, triggering compensation
	if _, err := prod.Engine.Cancel(ctx, execID, "Safety policy violation detected"); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Await terminal state
	final, err := prod.Engine.Await(ctx, execID, adgo.AwaitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != adgo.StatusCanceled && final.Status != adgo.StatusFailed {
		t.Fatalf("expected canceled/failed status, got %s", final.Status)
	}

	// Verify compensation handler RollbackCanary was called
	if state.CanaryRollbacks.Load() < 1 {
		t.Fatalf("expected CanaryRollbacks >= 1, got %d", state.CanaryRollbacks.Load())
	}
}

// TestProductionRefApp_NoUnsafeSideEffectSpeculation enforces the design invariant that
// speculative hedging/ensembles fail closed when Pure is false.
func TestProductionRefApp_NoUnsafeSideEffectSpeculation(t *testing.T) {
	_, err := adgo.NewHedgedActivity([]adgo.ActivityVariant{
		{
			Name: "unsafe_mutation",
			Handler: func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
				return adgo.ActivityResult{}, nil
			},
		},
	}, adgo.SpeculationPolicy{
		Pure: false, // Unsafe speculation!
	})

	if err == nil {
		t.Fatalf("expected error when SpeculationPolicy.Pure=false, got nil")
	}
}

// TestProductionRefApp_RetentionPruning validates execution catalog retention.
func TestProductionRefApp_RetentionPruning(t *testing.T) {
	ctx := context.Background()
	store := adgo.NewMemoryStore()

	plan, err := adgo.Compile(adgo.Definition{
		ID:      "retention-test",
		Version: "1",
		Nodes:   []adgo.Node{{ID: "a", Kind: adgo.NodeActivity, Activity: "a"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	reg := adgo.NewRegistry()
	reg.Activity("a", func(context.Context, adgo.ActivityRequest) (adgo.ActivityResult, error) {
		return adgo.ActivityResult{}, nil
	})

	engine, err := adgo.NewEngine(plan, store, reg)
	if err != nil {
		t.Fatal(err)
	}

	exec, err := engine.Start(ctx, "retention-exec-1", nil, adgo.BudgetLimit{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, exec.ID); err != nil {
		t.Fatal(err)
	}
	work, err := engine.Poll(ctx, adgo.WorkerSpec{ID: "w", LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Complete(ctx, work.Token, adgo.ActivityResult{}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, exec.ID); err != nil {
		t.Fatal(err)
	}

	// Verify terminal completed
	final, err := store.Load(ctx, exec.ID)
	if err != nil || final.Status != adgo.StatusCompleted {
		t.Fatalf("execution not completed: %v, status=%v", err, final.Status)
	}

	// Prune terminal executions older than 0
	res, err := adgo.CollectExecutions(ctx, store, adgo.RetentionPolicy{
		TerminalFor: 0,
	})
	if err != nil {
		t.Fatalf("CollectExecutions failed: %v", err)
	}
	if len(res.Deleted) < 1 {
		t.Fatalf("expected deleted executions >= 1, got %d", len(res.Deleted))
	}
}

func assertCondition(t *testing.T, timeout, interval time.Duration, cond func() bool, errMsg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatal(errMsg)
}
