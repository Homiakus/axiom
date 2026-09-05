package adgo

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCommitFencedRequiresLiveClaimAndCommitsAtomically(t *testing.T) {
	ctx := context.Background()
	plan, err := Compile(Definition{
		ID:      "fenced-commit-test",
		Version: "1",
		Nodes: []Node{{
			ID:       "publish",
			Kind:     NodeActivity,
			Activity: "Publish",
			Produces: []string{"skillstate"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	engine, err := NewEngine(plan, store, NewRegistry(), WithEngineLeaseTTL(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(ctx, "run-1", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "run-1"); err != nil {
		t.Fatal(err)
	}
	item, err := engine.Poll(ctx, WorkerSpec{
		ID:         "worker-1",
		Activities: []string{"Publish"},
		LeaseTTL:   time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	before, err := engine.Get(ctx, item.Token.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := engine.CommitFenced(ctx, item.Token, func(execution *Execution) error {
		return SetData(execution, "skillstate", map[string]any{"revision": 2, "digest": "sha256:next"})
	})
	if err != nil {
		t.Fatalf("CommitFenced() error = %v", err)
	}
	if next.Version != before.Version+1 {
		t.Fatalf("version = %d, want %d", next.Version, before.Version+1)
	}
	state, ok, err := Data[map[string]any](next, "skillstate")
	if err != nil || !ok {
		t.Fatalf("committed data missing: ok=%v err=%v", ok, err)
	}
	if got := state["digest"]; got != "sha256:next" {
		t.Fatalf("digest = %v", got)
	}

	stale := item.Token
	stale.WorkerID = "worker-2"
	if _, err := engine.CommitFenced(ctx, stale, func(*Execution) error { return nil }); !errors.Is(err, ErrStaleTask) {
		t.Fatalf("stale worker error = %v, want ErrStaleTask", err)
	}
}

func TestCommitFencedDoesNotPublishFailedMutation(t *testing.T) {
	ctx := context.Background()
	plan, err := Compile(Definition{
		ID:      "fenced-rollback-test",
		Version: "1",
		Nodes: []Node{{ID: "publish", Kind: NodeActivity, Activity: "Publish"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(plan, NewMemoryStore(), NewRegistry(), WithEngineLeaseTTL(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(ctx, "run-2", nil, BudgetLimit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Advance(ctx, "run-2"); err != nil {
		t.Fatal(err)
	}
	item, err := engine.Poll(ctx, WorkerSpec{ID: "worker-1", Activities: []string{"Publish"}, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	before, err := engine.Get(ctx, "run-2")
	if err != nil {
		t.Fatal(err)
	}

	precondition := errors.New("skillstate precondition failed")
	if _, err := engine.CommitFenced(ctx, item.Token, func(execution *Execution) error {
		if err := SetData(execution, "must-not-publish", true); err != nil {
			return err
		}
		return precondition
	}); !errors.Is(err, precondition) {
		t.Fatalf("error = %v, want precondition error", err)
	}
	after, err := engine.Get(ctx, "run-2")
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version {
		t.Fatalf("version changed on failed mutation: before=%d after=%d", before.Version, after.Version)
	}
	if _, ok := after.Data["must-not-publish"]; ok {
		t.Fatal("failed fenced mutation leaked data")
	}
}
