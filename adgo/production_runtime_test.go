package adgo

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenProductionPebbleReopensExecution(t *testing.T) {
	plan, err := Compile(Definition{ID: "production", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	production, err := OpenProduction(plan, NewRegistry(), DefaultProductionConfig(root))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := production.Engine.Start(ctx, "prod-1", map[string]any{"key": "value"}, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if err := production.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenProduction(plan, NewRegistry(), DefaultProductionConfig(root))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	execution, err := reopened.Engine.Get(ctx, "prod-1")
	if err != nil {
		t.Fatal(err)
	}
	if execution.PlanDigest != plan.Digest || execution.ID != "prod-1" {
		t.Fatalf("execution=%+v", execution)
	}
}

func TestWorkerServiceDrainFinishesClaimedWithoutTakingNext(t *testing.T) {
	plan, err := Compile(Definition{ID: "drain", Version: "1", GlobalConcurrency: 2, Nodes: []Node{
		{ID: "a", Kind: NodeActivity, Activity: "a"},
		{ID: "b", Kind: NodeActivity, Activity: "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	registry := NewRegistry()
	registry.Activity("a", func(context.Context, ActivityRequest) (ActivityResult, error) {
		close(started)
		<-release
		return ActivityResult{}, nil
	})
	var bCalls atomic.Int32
	registry.Activity("b", func(context.Context, ActivityRequest) (ActivityResult, error) {
		bCalls.Add(1)
		return ActivityResult{}, nil
	})
	store := NewMemoryStore()
	engine, _ := NewEngine(plan, store, registry, WithEnginePollInterval(time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _ = engine.Start(ctx, "drain-1", nil, BudgetLimit{})
	if _, err := engine.Advance(ctx, "drain-1"); err != nil {
		t.Fatal(err)
	}
	service, err := NewWorkerService(engine, WorkerSpec{ID: "worker", Concurrency: 1, PollInterval: time.Millisecond, LeaseTTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	runErr := make(chan error, 1)
	go func() { runErr <- service.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not claim first task")
	}
	// BeginDrain is synchronous: after it returns no new task may be claimed.
	service.BeginDrain()
	close(release)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := service.Drain(drainCtx); err != nil {
		t.Fatal(err)
	}
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
	if bCalls.Load() != 0 {
		t.Fatalf("draining worker executed another task: bCalls=%d", bCalls.Load())
	}
	if !service.Status().Stopped || !service.Status().Draining {
		t.Fatalf("status=%+v", service.Status())
	}
}

func TestRetentionDeletesTerminalAndPrunesFileVersions(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	execution := &Execution{ID: "retain-1", PlanID: "p", PlanVersion: "1", PlanDigest: "d", Version: 1, Status: StatusRunning, CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour)}
	ensureExecution(execution)
	if err := store.Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		current, _ := store.Load(ctx, execution.ID)
		if _, err := store.Commit(ctx, execution.ID, current.Version, func(x *Execution) error {
			x.Status = StatusRunning
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.PruneVersions(ctx, execution.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 3 {
		t.Fatalf("removed=%d want 3", removed)
	}
	versions, err := store.ListVersions(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions=%d", len(versions))
	}
	current, _ := store.Load(ctx, execution.ID)
	if _, err := store.Commit(ctx, execution.ID, current.Version, func(x *Execution) error {
		x.Status = StatusCompleted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := CollectExecutions(ctx, store, RetentionPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != execution.ID {
		t.Fatalf("retention=%+v", result)
	}
	if _, err := store.Load(ctx, execution.ID); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("load after retention err=%v", err)
	}
}

func TestDiagnosticsDetectExpiredLeaseAndStateCorruption(t *testing.T) {
	plan, err := Compile(Definition{ID: "diag", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	execution := &Execution{ID: "diag-1", PlanID: plan.ID, PlanVersion: plan.Version, PlanDigest: plan.Digest, Version: 1, Status: StatusRunning}
	ensureExecution(execution)
	execution.Nodes["work"] = &NodeRuntime{Status: NodeCompleted, Activated: true}
	execution.ActiveTasks["task"] = TaskRuntime{ID: "task", NodeID: "work", Activity: "work", Status: TaskRunning, WorkerID: "dead", LeaseUntil: time.Now().Add(-time.Second)}
	diagnostics := AuditExecution(plan, execution, time.Now())
	codes := map[string]bool{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["ADG-DIAG-TASK-STATE"] || !codes["ADG-DIAG-LEASE-EXPIRED"] || !codes["ADG-DIAG-COMPLETED-TASK"] {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
}

func TestProductionChildWorkflowIDIsDeterministic(t *testing.T) {
	store := NewMemoryStore()
	host, err := NewHost(store)
	if err != nil {
		t.Fatal(err)
	}
	parent, _ := Compile(Definition{ID: "parent", Version: "1", Nodes: []Node{{ID: "root", Kind: NodeActivity, Activity: "root"}}})
	child, _ := Compile(Definition{ID: "child", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if _, err := host.Register(parent, NewRegistry()); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Register(child, NewRegistry()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, firstExecution, err := host.StartChild(ctx, "parent-1", "fanout", "item-7", PlanRef{Digest: child.Digest}, ChildOptions{Initial: map[string]any{"value": 7}})
	if err != nil {
		t.Fatal(err)
	}
	second, secondExecution, err := host.StartChild(ctx, "parent-1", "fanout", "item-7", PlanRef{Digest: child.Digest}, ChildOptions{Initial: map[string]any{"value": 999}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ExecutionID != second.ExecutionID || firstExecution.ID != secondExecution.ID {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if firstExecution.Version != secondExecution.Version {
		t.Fatalf("redelivery should resume same child: versions %d %d", firstExecution.Version, secondExecution.Version)
	}
}
