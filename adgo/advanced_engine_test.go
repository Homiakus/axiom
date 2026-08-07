package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoverCompensationAfterCoordinatorCrash(t *testing.T) {
	plan, err := Compile(Definition{ID: "comp-recovery", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work", Compensation: "undo"}}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	registry := NewRegistry()
	var calls atomic.Int32
	registry.Compensation("undo", func(context.Context, ActivityRequest) error {
		calls.Add(1)
		return nil
	})
	engine, _ := NewEngine(plan, store, registry)
	started, err := engine.Start(context.Background(), "comp-1", nil, BudgetLimit{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Commit(context.Background(), started.ID, started.Version, func(x *Execution) error {
		x.Status = StatusCompensating
		x.Failure = "boom"
		x.CompensationStack = []CompensationEntry{{NodeID: "work", Activity: "undo", IdempotencyKey: "comp-key"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := engine.RecoverCompensation(context.Background(), "comp-1")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusFailed || len(recovered.CompensationStack) != 0 || calls.Load() != 1 {
		t.Fatalf("status=%s stack=%d calls=%d", recovered.Status, len(recovered.CompensationStack), calls.Load())
	}
}

func TestResultCacheAvoidsDuplicatePureActivity(t *testing.T) {
	cache := NewMemoryActivityCache()
	var calls atomic.Int32
	handler := WithResultCache(cache, "llm", CachePolicy{Namespace: "prompt-v1", TTL: time.Minute}, func(context.Context, ActivityRequest) (ActivityResult, error) {
		calls.Add(1)
		return ActivityResult{Facts: map[string]any{"answer": "ok"}, Quality: QualityVector{"quality": .9}}, nil
	})
	raw, _ := json.Marshal("same")
	request := ActivityRequest{Data: map[string]json.RawMessage{"prompt": raw}}
	first, err := handler(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := handler(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	if first.Metrics["cache_miss"] != 1 || second.Metrics["cache_hit"] != 1 {
		t.Fatalf("first=%v second=%v", first.Metrics, second.Metrics)
	}
}

func TestSignalDeterministicRejectsAmbiguousTarget(t *testing.T) {
	plan, err := Compile(Definition{ID: "signals", Version: "1", Nodes: []Node{
		{ID: "left", Kind: NodeWait, Wait: &WaitSpec{EventType: "Wake"}},
		{ID: "right", Kind: NodeWait, Wait: &WaitSpec{EventType: "Wake"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, _ := NewEngine(plan, store, NewRegistry())
	_, _ = engine.Start(context.Background(), "sig-1", nil, BudgetLimit{})
	_, _ = engine.Advance(context.Background(), "sig-1")
	err = engine.SignalDeterministic(context.Background(), "sig-1", Event{ID: "wake", Type: "Wake"}, SignalPolicy{})
	if err == nil {
		t.Fatal("expected ambiguous signal rejection")
	}
	payload, _ := json.Marshal(map[string]any{"source": "webhook"})
	if err := engine.SignalDeterministic(context.Background(), "sig-1", Event{ID: "wake-left", Type: "Wake", TargetNode: "left", Payload: payload}, SignalPolicy{PersistPayload: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(context.Background(), "sig-1"); err != nil && !errors.Is(err, ErrDeadlock) {
		t.Fatal(err)
	}
	execution, _ := store.Load(context.Background(), "sig-1")
	if _, ok := execution.Data["event:left:Wake"]; !ok {
		t.Fatal("signal payload was not committed")
	}
	if execution.Nodes["left"].Status != NodeCompleted || execution.Nodes["right"].Status != NodeWaiting {
		t.Fatalf("left=%s right=%s", execution.Nodes["left"].Status, execution.Nodes["right"].Status)
	}
}

func TestRewindFromInvalidatesOnlyDescendants(t *testing.T) {
	plan, err := Compile(Definition{ID: "rewind", Version: "1", Nodes: []Node{
		{ID: "a", Kind: NodeActivity, Activity: "a", Produces: []string{"aout"}, Next: []Transition{{To: "b"}, {To: "side"}}},
		{ID: "b", Kind: NodeActivity, Activity: "b", DependsOn: []string{"a"}, Produces: []string{"bout"}},
		{ID: "side", Kind: NodeActivity, Activity: "side", DependsOn: []string{"a"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Activity("a", func(context.Context, ActivityRequest) (ActivityResult, error) { return ActivityResult{Facts: map[string]any{"aout": 1}}, nil })
	registry.Activity("b", func(context.Context, ActivityRequest) (ActivityResult, error) { return ActivityResult{Facts: map[string]any{"bout": 2}}, nil })
	registry.Activity("side", noopActivity)
	store := NewMemoryStore()
	runtime, _ := NewRuntime(plan, store, registry)
	_, _ = runtime.Start(context.Background(), "rw-1", nil, BudgetLimit{})
	if _, err := runtime.Run(context.Background(), "rw-1"); err != nil {
		t.Fatal(err)
	}
	engine, _ := NewEngine(plan, store, registry)
	rewound, err := engine.RewindFrom(context.Background(), "rw-1", "b", "operator correction", "ops")
	if err != nil {
		t.Fatal(err)
	}
	if rewound.Nodes["a"].Status != NodeCompleted || rewound.Nodes["side"].Status != NodeCompleted || rewound.Nodes["b"].Status != NodePending {
		t.Fatalf("a=%s side=%s b=%s", rewound.Nodes["a"].Status, rewound.Nodes["side"].Status, rewound.Nodes["b"].Status)
	}
	if _, ok := rewound.Data["bout"]; ok {
		t.Fatal("rewound output still present")
	}
}

func TestEnsembleSelectsBestAndAccountsAllBudget(t *testing.T) {
	activity, err := NewEnsembleActivity([]ActivityVariant{
		{Name: "cheap", Handler: func(context.Context, ActivityRequest) (ActivityResult, error) {
			return ActivityResult{Quality: QualityVector{"q": .80}, Budget: BudgetUsage{Cost: .2, LLMCalls: 1}}, nil
		}},
		{Name: "strong", Handler: func(context.Context, ActivityRequest) (ActivityResult, error) {
			return ActivityResult{Facts: map[string]any{"winner": "strong"}, Quality: QualityVector{"q": .97}, Budget: BudgetUsage{Cost: .7, LLMCalls: 1}}, nil
		}},
	}, SpeculationPolicy{Pure: true, MaxParallel: 2, MinQuality: .75})
	if err != nil {
		t.Fatal(err)
	}
	result, err := activity(context.Background(), ActivityRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Budget.Cost != .9 || result.Budget.LLMCalls != 2 {
		t.Fatalf("budget=%+v", result.Budget)
	}
	if QualityUtility(result.Quality) != .97 {
		t.Fatalf("quality=%v", result.Quality)
	}
	if _, err := NewHedgedActivity([]ActivityVariant{{Name: "unsafe", Handler: noopActivity}}, SpeculationPolicy{}); err == nil {
		t.Fatal("speculation must require explicit Pure=true")
	}
}
