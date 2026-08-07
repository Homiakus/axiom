package adgo

import (
	"context"
	"testing"
)

func TestFileStoreAuditReplay(t *testing.T) {
	plan, err := Compile(Definition{ID: "replay", Version: "1", Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work"}}})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := NewFileStore(t.TempDir())
	reg := NewRegistry()
	reg.Activity("work", noopActivity)
	rt, _ := NewRuntime(plan, store, reg)
	_, _ = rt.Start(context.Background(), "x", nil, BudgetLimit{})
	e, err := rt.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("status=%s", e.Status)
	}
	versions, err := store.ListVersions(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	report := VerifyReplay(plan, versions)
	if !report.Valid {
		t.Fatalf("replay problems: %v", report.Problems)
	}
	if report.Versions < 3 {
		t.Fatalf("expected multiple durable commits, got %d", report.Versions)
	}
}
