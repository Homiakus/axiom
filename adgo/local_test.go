package adgo

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunLocalExecutesParallelBatchThroughEngine(t *testing.T) {
	plan, err := Compile(Definition{
		ID: "local-parallel", Version: "1", GlobalConcurrency: 2,
		Nodes: []Node{
			{ID: "a", Kind: NodeActivity, Activity: "a", Next: []Transition{{To: "join"}}},
			{ID: "b", Kind: NodeActivity, Activity: "b", Next: []Transition{{To: "join"}}},
			{ID: "join", Kind: NodeJoin, DependsOn: []string{"a", "b"}, Join: &JoinSpec{Mode: JoinAll}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Activity("a", func(context.Context, ActivityRequest) (ActivityResult, error) {
		return ActivityResult{Facts: map[string]any{"a": true}}, nil
	})
	registry.Activity("b", func(context.Context, ActivityRequest) (ActivityResult, error) {
		return ActivityResult{Facts: map[string]any{"b": true}}, nil
	})
	engine, err := NewEngine(plan, NewMemoryStore(), registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(context.Background(), "local-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	execution, err := engine.RunLocal(context.Background(), "local-1", LocalRunOptions{Worker: WorkerSpec{ID: "local", Concurrency: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != StatusCompleted {
		t.Fatalf("status=%s", execution.Status)
	}
	if _, ok := execution.Data["a"]; !ok {
		t.Fatal("fact a missing")
	}
	if _, ok := execution.Data["b"]; !ok {
		t.Fatal("fact b missing")
	}
}

func TestRunLocalOwnsRetryAndBackoff(t *testing.T) {
	plan, err := Compile(Definition{ID: "local-retry", Version: "1", Nodes: []Node{{
		ID: "work", Kind: NodeActivity, Activity: "work",
		Retry: RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxRetryDuration: time.Second},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	registry := NewRegistry()
	registry.Activity("work", func(context.Context, ActivityRequest) (ActivityResult, error) {
		if calls.Add(1) == 1 {
			return ActivityResult{}, Fail(FailureTransient, errors.New("temporary"))
		}
		return ActivityResult{Facts: map[string]any{"done": true}}, nil
	})
	engine, _ := NewEngine(plan, NewMemoryStore(), registry)
	_, _ = engine.Start(context.Background(), "retry-1", nil, BudgetLimit{})
	execution, err := engine.RunLocal(context.Background(), "retry-1", LocalRunOptions{Worker: WorkerSpec{ID: "local"}})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != StatusCompleted || calls.Load() != 2 || execution.Metrics.Retries != 1 {
		t.Fatalf("status=%s calls=%d retries=%d", execution.Status, calls.Load(), execution.Metrics.Retries)
	}
}

func TestRunLocalReturnsDurableExternalWait(t *testing.T) {
	plan, err := Compile(Definition{ID: "local-wait", Version: "1", Nodes: []Node{{ID: "wait", Kind: NodeWait, Wait: &WaitSpec{EventType: "Continue"}}}})
	if err != nil {
		t.Fatal(err)
	}
	engine, _ := NewEngine(plan, NewMemoryStore(), NewRegistry())
	_, _ = engine.Start(context.Background(), "wait-1", nil, BudgetLimit{})
	execution, err := engine.RunLocal(context.Background(), "wait-1", LocalRunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != StatusWaiting {
		t.Fatalf("status=%s", execution.Status)
	}
}
