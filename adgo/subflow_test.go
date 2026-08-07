package adgo

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestRunFanoutUsesDeterministicDurableChildren(t *testing.T) {
	plan, err := Compile(Definition{ID: "child", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	var calls atomic.Int32
	reg.Activity("work", func(context.Context, ActivityRequest) (ActivityResult, error) {
		calls.Add(1)
		return ActivityResult{}, nil
	})
	store := NewMemoryStore()
	child, _ := NewRuntime(plan, store, reg)
	items := []FanoutItem{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got, err := RunFanout(context.Background(), "parent", "sources", child, items, 10, 3, JoinSpec{Mode: JoinAll})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Satisfied || got.Completed != 3 {
		t.Fatalf("result=%+v", got)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
	got, err = RunFanout(context.Background(), "parent", "sources", child, items, 10, 3, JoinSpec{Mode: JoinAll})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Satisfied {
		t.Fatal("second resume not satisfied")
	}
	if calls.Load() != 3 {
		t.Fatalf("durable children should not repeat completed work; calls=%d", calls.Load())
	}
}
