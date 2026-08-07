package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEngineFencesExpiredWorker(t *testing.T) {
	plan, err := Compile(Definition{ID: "worker-fence", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, NewRegistry(), WithEngineLeaseTTL(15*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := engine.Start(ctx, "wf-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Advance(ctx, "wf-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.QueuedTasks) != 1 {
		t.Fatalf("queued=%v", result.QueuedTasks)
	}
	oldWork, err := engine.Poll(ctx, WorkerSpec{ID: "worker-a", LeaseTTL: 15 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	if _, err := engine.Advance(ctx, "wf-1"); err != nil {
		t.Fatal(err)
	}
	newWork, err := engine.Poll(ctx, WorkerSpec{ID: "worker-b", LeaseTTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if newWork.Token.Attempt != 2 {
		t.Fatalf("attempt=%d want 2", newWork.Token.Attempt)
	}
	if _, err := engine.Complete(ctx, oldWork.Token, ActivityResult{}, time.Millisecond); !errors.Is(err, ErrStaleTask) {
		t.Fatalf("stale completion err=%v", err)
	}
	if _, err := engine.Complete(ctx, newWork.Token, ActivityResult{}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "wf-1"); err != nil {
		t.Fatal(err)
	}
	execution, _ := store.Load(ctx, "wf-1")
	if execution.Status != StatusCompleted {
		t.Fatalf("status=%s", execution.Status)
	}
	if execution.Metrics.RecoveryEvents != 1 {
		t.Fatalf("recoveries=%d", execution.Metrics.RecoveryEvents)
	}
}

func TestEngineHeartbeatKeepsLeaseAlive(t *testing.T) {
	plan, err := Compile(Definition{ID: "heartbeat", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, _ := NewEngine(plan, store, NewRegistry(), WithEngineLeaseTTL(30*time.Millisecond))
	ctx := context.Background()
	_, _ = engine.Start(ctx, "hb-1", nil, BudgetLimit{})
	_, _ = engine.Advance(ctx, "hb-1")
	work, err := engine.Poll(ctx, WorkerSpec{ID: "worker", LeaseTTL: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := engine.Heartbeat(ctx, work.Token, map[string]any{"progress": 0.5}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := engine.Advance(ctx, "hb-1"); err != nil {
		t.Fatal(err)
	}
	execution, _ := store.Load(ctx, "hb-1")
	if execution.Metrics.RecoveryEvents != 0 {
		t.Fatalf("heartbeat lease was recovered unexpectedly")
	}
	if _, err := engine.Complete(ctx, work.Token, ActivityResult{}, time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestEngineHumanResolutionCarriesPatchAndPayload(t *testing.T) {
	plan, err := Compile(Definition{ID: "human-engine", Version: "1", Nodes: []Node{
		{ID: "prepare", Kind: NodeActivity, Activity: "prepare", Next: []Transition{{To: "review"}}},
		{ID: "review", Kind: NodeHuman, DependsOn: []string{"prepare"}, Human: &HumanSpec{EventType: "Review", Risk: RiskHigh}, Next: []Transition{{To: "done"}}},
		{ID: "done", Kind: NodeActivity, Activity: "done", DependsOn: []string{"review"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, _ := NewEngine(plan, store, NewRegistry())
	ctx := context.Background()
	_, _ = engine.Start(ctx, "human-1", nil, BudgetLimit{})
	_, _ = engine.Advance(ctx, "human-1")
	prepare, _ := engine.Poll(ctx, WorkerSpec{ID: "w"})
	_, _ = engine.Complete(ctx, prepare.Token, ActivityResult{}, time.Millisecond)
	_, err = engine.Advance(ctx, "human-1")
	if err != nil {
		t.Fatal(err)
	}
	execution, _ := store.Load(ctx, "human-1")
	if execution.Status != StatusHuman {
		t.Fatalf("status=%s", execution.Status)
	}
	_, err = engine.ResolveHuman(ctx, "human-1", "review", HumanResolution{
		Decision: HumanEdit,
		Actor:    "reviewer@example",
		Patch:    map[string]any{"approvedTitle": "better"},
		Payload:  map[string]any{"comment": "ship it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = engine.Advance(ctx, "human-1")
	done, err := engine.Poll(ctx, WorkerSpec{ID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = engine.Complete(ctx, done.Token, ActivityResult{}, time.Millisecond)
	_, _ = engine.Advance(ctx, "human-1")
	execution, _ = store.Load(ctx, "human-1")
	if execution.Status != StatusCompleted {
		t.Fatalf("status=%s", execution.Status)
	}
	var title string
	if err := json.Unmarshal(execution.Data["approvedTitle"], &title); err != nil || title != "better" {
		t.Fatalf("title=%q err=%v", title, err)
	}
}

func TestAdaptiveRouterFallsBackAfterProviderFailure(t *testing.T) {
	registry := NewRegistry()
	registry.Provider("llm", Provider{Name: "primary", Activity: "primary", Quality: .95, Privacy: .9, Cost: .1})
	registry.Provider("llm", Provider{Name: "fallback", Activity: "fallback", Quality: .80, Privacy: .9, Cost: .1})
	router := NewAdaptiveRouter(registry, RouterConfig{FailureThreshold: 1, BaseCooldown: time.Minute, MaxCooldown: time.Minute, EWMAAlpha: .5})
	ctx := context.Background()
	first, err := router.Resolve(ctx, "llm", ProviderPolicy{AllowFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "primary" {
		t.Fatalf("first=%s", first.Name)
	}
	router.Report("llm", "primary", time.Second, nil, Fail(FailureTransient, errors.New("down")))
	second, err := router.Resolve(ctx, "llm", ProviderPolicy{AllowFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "fallback" {
		t.Fatalf("fallback=%s", second.Name)
	}
}

func TestPlanMigrationActivatesAddedSuccessor(t *testing.T) {
	from, err := Compile(Definition{ID: "migration", Version: "1", Nodes: []Node{{ID: "a", Kind: NodeActivity, Activity: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	to, err := Compile(Definition{ID: "migration", Version: "2", Nodes: []Node{
		{ID: "a", Kind: NodeActivity, Activity: "a", Next: []Transition{{To: "b"}}},
		{ID: "b", Kind: NodeActivity, Activity: "b", DependsOn: []string{"a"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Activity("a", noopActivity)
	registry.Activity("b", noopActivity)
	store := NewMemoryStore()
	runtime, _ := NewRuntime(from, store, registry)
	_, _ = runtime.Start(context.Background(), "m-1", nil, BudgetLimit{})
	if _, err := runtime.Run(context.Background(), "m-1"); err != nil {
		t.Fatal(err)
	}
	migrated, report, err := MigrateExecution(context.Background(), store, from, to, "m-1", MigrationPolicy{Reason: "add b"})
	if err != nil {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if migrated.PlanDigest != to.Digest || !migrated.Nodes["b"].Activated || migrated.Nodes["b"].Status != NodePending {
		t.Fatalf("migrated b=%+v", migrated.Nodes["b"])
	}
	engine, _ := NewEngine(to, store, registry)
	_, _ = engine.Advance(context.Background(), "m-1")
	work, err := engine.Poll(context.Background(), WorkerSpec{ID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	if work.Node.ID != "b" {
		t.Fatalf("node=%s", work.Node.ID)
	}
}

func TestForkExecutionFromHistoricalSnapshot(t *testing.T) {
	plan, err := Compile(Definition{ID: "fork", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := NewRuntime(plan, store, NewRegistry())
	if _, err := runtime.Start(context.Background(), "source", map[string]any{"topic": "A"}, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	fork, info, err := ForkExecution(context.Background(), store, plan, "source", 1, "branch", ForkOptions{DataPatch: map[string]any{"topic": "B"}})
	if err != nil {
		t.Fatal(err)
	}
	if info.SourceVersion != 1 || fork.ID != "branch" || fork.Nodes["work"].Status != NodePending {
		t.Fatalf("fork=%+v info=%+v", fork, info)
	}
	var topic string
	_ = json.Unmarshal(fork.Data["topic"], &topic)
	if topic != "B" {
		t.Fatalf("topic=%q", topic)
	}
}

func TestAwaitableStoresPayloadBeforeResume(t *testing.T) {
	plan, err := Compile(Definition{ID: "awaitable", Version: "1", Nodes: []Node{{ID: "callback", Kind: NodeWait, Wait: &WaitSpec{EventType: "Callback"}}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, _ := NewEngine(plan, store, NewRegistry())
	ctx := context.Background()
	_, _ = engine.Start(ctx, "aw-1", nil, BudgetLimit{})
	_, _ = engine.Advance(ctx, "aw-1")
	awaitable, err := engine.Awaitable(ctx, "aw-1", "callback")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ResolveAwaitable(ctx, awaitable, map[string]any{"value": 42}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "aw-1"); err != nil {
		t.Fatal(err)
	}
	execution, _ := store.Load(ctx, "aw-1")
	if execution.Status != StatusCompleted {
		t.Fatalf("status=%s", execution.Status)
	}
	if _, ok := execution.Data["awaitable:callback"]; !ok {
		t.Fatal("callback payload not persisted")
	}
}

func TestDurableScheduleUsesDeterministicExecutionID(t *testing.T) {
	plan, err := Compile(Definition{ID: "scheduled", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	engine, _ := NewEngine(plan, NewMemoryStore(), NewRegistry())
	schedules := NewMemoryScheduleStore()
	runner, _ := NewScheduleRunner(engine, schedules)
	fireAt := time.Date(2026, 8, 7, 19, 0, 0, 0, time.UTC)
	if _, err := runner.Register(context.Background(), Schedule{ID: "daily", Every: time.Hour, StartAt: fireAt}); err != nil {
		t.Fatal(err)
	}
	first, err := runner.Tick(context.Background(), fireAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Tick(context.Background(), fireAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("first=%v second=%v", first, second)
	}
	if first[0] != scheduledExecutionID("daily", fireAt) {
		t.Fatalf("id=%s", first[0])
	}
}

func TestContinueAsNewCarriesDurableFacts(t *testing.T) {
	plan, err := Compile(Definition{ID: "continue", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, _ := NewEngine(plan, store, NewRegistry())
	ctx := context.Background()
	_, _ = engine.Start(ctx, "old", map[string]any{"memory": "keep"}, BudgetLimit{})
	fresh, err := engine.ContinueAsNew(ctx, "old", "new", ContinueOptions{Reason: "compact history"})
	if err != nil {
		t.Fatal(err)
	}
	var memory string
	_ = json.Unmarshal(fresh.Data["memory"], &memory)
	if memory != "keep" {
		t.Fatalf("memory=%q", memory)
	}
	old, _ := store.Load(ctx, "old")
	if old.Status != StatusCompleted {
		t.Fatalf("old status=%s", old.Status)
	}
}
