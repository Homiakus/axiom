package profile_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/profile"
)

type counterState struct {
	Count int `json:"count"`
}

type incrementEvent struct {
	Delta int `json:"delta"`
}

type notifyCmd struct {
	Value int `json:"value"`
}

func buildTestFlow() *axiom.Flow[counterState] {
	fl := axiom.NewFlow("counter", counterState{Count: 0})
	axiom.Handle(fl, func(ctx context.Context, s counterState, e incrementEvent) (axiom.FlowResult[counterState], error) {
		s.Count += e.Delta
		return axiom.Next(s, axiom.Call(notifyCmd{Value: s.Count})), nil
	})
	axiom.EffectHandler(fl, func(ctx context.Context, c notifyCmd) error {
		return nil
	})
	return fl
}

const testAxmSource = `
domain TestEmbedded

signal Step:
  val: String

context Status:
  val: String = "init"

rule doStep:
  on Step
  write:
    Status.val = signal.val
`

// ── Profile 1: Embedded Conformance ──────────────────────────────────────────

func TestEmbeddedProfileConformance(t *testing.T) {
	emb := profile.NewEmbedded()
	defer func() {
		if err := emb.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}()

	g := emb.Guarantees()
	if g.Name != profile.ProfileEmbedded {
		t.Fatalf("expected profile name %s, got %s", profile.ProfileEmbedded, g.Name)
	}
	if len(g.Guarantees) == 0 || len(g.NonGuarantees) == 0 {
		t.Fatalf("guarantees or non-guarantees are empty: %+v", g)
	}

	// 1. Flow runner in-process
	fl := buildTestFlow()
	engine, err := profile.OpenEmbeddedFlow(emb, fl)
	if err != nil {
		t.Fatalf("new flow engine: %v", err)
	}
	ctx := context.Background()
	exec := engine.Execution("exec-1")
	if err := exec.Dispatch(ctx, incrementEvent{Delta: 5}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	st, err := exec.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if st.Count != 5 {
		t.Fatalf("expected count 5, got %d", st.Count)
	}

	// 2. Declarative engine in-process
	axmEngine, err := emb.CompileAndNew([]byte(testAxmSource))
	if err != nil {
		t.Fatalf("compile and new: %v", err)
	}
	execID := "axm-1"
	axmExec := axmEngine.Execution(execID)
	if err := axmExec.Signal(ctx, "Step", map[string]any{"val": "done"}); err != nil {
		t.Fatalf("axm signal: %v", err)
	}
	res, err := axmEngine.Query(ctx, execID, "state")
	if err != nil {
		t.Fatalf("query state: %v", err)
	}
	contexts, ok := res["context"].(map[string]map[string]any)
	if !ok || contexts["Status"]["val"] != "done" {
		t.Fatalf("expected Status.val 'done', got %v", res)
	}
}

// ── Profile 2: Durable Single Node Conformance ───────────────────────────────

func TestDurableSingleNodeProfileConformance(t *testing.T) {
	dir := t.TempDir()

	cfg := profile.DurableSingleNodeConfig{
		Dir:        dir,
		SyncWrites: true,
	}
	p1, err := profile.OpenDurableSingleNode(cfg)
	if err != nil {
		t.Fatalf("open durable single node: %v", err)
	}

	g := p1.Guarantees()
	if g.Name != profile.ProfileDurableSingleNode {
		t.Fatalf("expected profile name %s, got %s", profile.ProfileDurableSingleNode, g.Name)
	}
	if len(g.Guarantees) == 0 || len(g.NonGuarantees) == 0 {
		t.Fatalf("guarantees or non-guarantees are empty: %+v", g)
	}

	// 1. Flow with durable outbox
	fl := buildTestFlow()
	flowEngine, err := profile.OpenDurableFlow(p1, fl)
	if err != nil {
		t.Fatalf("new flow engine: %v", err)
	}
	ctx := context.Background()
	exec := flowEngine.Execution("flow-durable-1")
	if err := exec.Dispatch(ctx, incrementEvent{Delta: 42}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	st, err := exec.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if st.Count != 42 {
		t.Fatalf("expected count 42, got %d", st.Count)
	}

	// 2. Declarative engine with Pebble store
	axmEngine, err := p1.CompileAndNew([]byte(testAxmSource))
	if err != nil {
		t.Fatalf("compile and new: %v", err)
	}
	axmExec := axmEngine.Execution("axm-durable-1")
	if err := axmExec.Signal(ctx, "Step", map[string]any{"val": "done"}); err != nil {
		t.Fatalf("axm signal: %v", err)
	}

	// Simulate crash: close p1
	if err := p1.Close(); err != nil {
		t.Fatalf("p1 close: %v", err)
	}

	// Reopen p2 from the same directory
	p2, err := profile.OpenDurableSingleNode(cfg)
	if err != nil {
		t.Fatalf("reopen durable single node: %v", err)
	}
	defer p2.Close()

	// Verify Flow state survived crash/restart
	reopenedFlowEngine, err := profile.OpenDurableFlow(p2, fl)
	if err != nil {
		t.Fatalf("reopened flow engine: %v", err)
	}
	reopenedExec := reopenedFlowEngine.Execution("flow-durable-1")
	recoveredState, err := reopenedExec.State(ctx)
	if err != nil {
		t.Fatalf("load recovered flow state: %v", err)
	}
	if recoveredState.Count != 42 {
		t.Fatalf("recovered flow state count expected 42, got %d", recoveredState.Count)
	}

	// Verify Declarative state survived crash/restart
	reopenedAxmEngine, err := p2.CompileAndNew([]byte(testAxmSource))
	if err != nil {
		t.Fatalf("reopen axm engine: %v", err)
	}
	res, err := reopenedAxmEngine.Query(ctx, "axm-durable-1", "state")
	if err != nil {
		t.Fatalf("query recovered axm state: %v", err)
	}
	contexts, ok := res["context"].(map[string]map[string]any)
	if !ok || contexts["Status"]["val"] != "done" {
		t.Fatalf("expected recovered Status.val 'done', got %v", res)
	}
}

// ── Profile 3: Distributed Production Conformance ────────────────────────────

func TestDistributedProductionProfileConformance(t *testing.T) {
	plan, err := adgo.Compile(adgo.Definition{
		ID:      "dist-test",
		Version: "1",
		Nodes: []adgo.Node{
			{ID: "compute", Kind: adgo.NodeActivity, Activity: "compute"},
		},
	})
	if err != nil {
		t.Fatalf("compile plan: %v", err)
	}

	var executed atomic.Int32
	reg := adgo.NewRegistry()
	reg.Activity("compute", func(ctx context.Context, req adgo.ActivityRequest) (adgo.ActivityResult, error) {
		executed.Add(1)
		return adgo.ActivityResult{Facts: map[string]any{"res": "ok"}}, nil
	})

	root := t.TempDir()
	cfg := profile.DefaultDistributedProductionConfig("node-1", root)
	cfg.PollInterval = 10 * time.Millisecond
	cfg.CoordinatorInterval = 10 * time.Millisecond

	prod, err := profile.OpenDistributedProduction(plan, reg, cfg)
	if err != nil {
		t.Fatalf("open distributed production: %v", err)
	}
	defer prod.Close()

	g := prod.Guarantees()
	if g.Name != profile.ProfileDistributedProduction {
		t.Fatalf("expected profile name %s, got %s", profile.ProfileDistributedProduction, g.Name)
	}
	if len(g.Guarantees) == 0 || len(g.NonGuarantees) == 0 {
		t.Fatalf("guarantees or non-guarantees are empty: %+v", g)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start execution before running workers
	if _, err := prod.Production().Engine.Start(ctx, "dist-exec-1", nil, adgo.BudgetLimit{}); err != nil {
		t.Fatalf("start execution: %v", err)
	}

	workerSpec := adgo.WorkerSpec{
		ID:           "worker-compute",
		Activities:   []string{"compute"},
		Concurrency:  2,
		LeaseTTL:     5 * time.Second,
		PollInterval: 10 * time.Millisecond,
	}

	// Run coordinator and worker via Serve
	errCh := make(chan error, 1)
	go func() {
		errCh <- prod.Serve(ctx, workerSpec)
	}()

	// Poll until completed
	for {
		exec, err := prod.Production().Engine.Get(ctx, "dist-exec-1")
		if err == nil && exec.Status == adgo.StatusCompleted {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for execution completion; executed=%d, exec=%+v", executed.Load(), exec)
		case <-time.After(20 * time.Millisecond):
		}
	}

	if executed.Load() != 1 {
		t.Fatalf("expected activity executed exactly once, got %d", executed.Load())
	}

	// Cleanly stop Serve before closing the DB
	cancel()
	<-errCh
}
