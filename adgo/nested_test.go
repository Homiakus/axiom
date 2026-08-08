package adgo

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestRunNestedLocalResumesSameParentApplication(t *testing.T) {
	store := NewMemoryStore()
	registry := NewRegistry()
	var calls atomic.Int32
	registry.Activity("work", func(context.Context, ActivityRequest) (ActivityResult, error) {
		calls.Add(1)
		return ActivityResult{Facts: map[string]any{"done": true}}, nil
	})
	definition := Definition{
		ID: "nested.test", Version: "1",
		Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work", Produces: []string{"done"}}},
	}
	invocation := NestedInvocation{
		ParentExecutionID: "parent", ParentNodeID: "drafting",
		ParentApplicationID: "parent:drafting:r1:input-a", FlowName: "writer",
	}
	first, err := RunNestedLocal(context.Background(), store, definition, registry, invocation, nil, NestedLocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunNestedLocal(context.Background(), store, definition, registry, invocation, nil, NestedLocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("child identity changed: %q != %q", first.ID, second.ID)
	}
	if calls.Load() != 1 {
		t.Fatalf("activity calls=%d, want 1", calls.Load())
	}
}

func TestRunNestedLocalChangesIdentityWithParentApplication(t *testing.T) {
	store := NewMemoryStore()
	registry := NewRegistry()
	var calls atomic.Int32
	registry.Activity("work", func(context.Context, ActivityRequest) (ActivityResult, error) {
		calls.Add(1)
		return ActivityResult{Facts: map[string]any{"done": true}}, nil
	})
	definition := Definition{
		ID: "nested.test.revision", Version: "1",
		Nodes: []Node{{ID: "work", Kind: NodeActivity, Activity: "work", Produces: []string{"done"}}},
	}
	firstInvocation := NestedInvocation{
		ParentExecutionID: "parent", ParentNodeID: "drafting",
		ParentApplicationID: "parent:drafting:r1:input-a", FlowName: "writer",
	}
	secondInvocation := firstInvocation
	secondInvocation.ParentApplicationID = "parent:drafting:r2:input-a"
	first, err := RunNestedLocal(context.Background(), store, definition, registry, firstInvocation, nil, NestedLocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunNestedLocal(context.Background(), store, definition, registry, secondInvocation, nil, NestedLocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("new parent application reused child %q", first.ID)
	}
	if calls.Load() != 2 {
		t.Fatalf("activity calls=%d, want 2", calls.Load())
	}
}

func TestNestedExecutionIDDoesNotExposeApplicationMaterial(t *testing.T) {
	id, err := NestedExecutionID(NestedInvocation{
		ParentExecutionID: "parent", ParentNodeID: "node",
		ParentApplicationID: "secret-looking-logical-input-that-must-not-be-in-id", FlowName: "writer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty child id")
	}
	if containsString(id, "secret-looking") {
		t.Fatalf("nested id leaks application material: %q", id)
	}
}

func TestChildExecutionIDIsCanonicalAcrossHostAndNestedHelpers(t *testing.T) {
	got := ChildExecutionID("parent", "drafting stage", "item/7")
	want := "parent/drafting_stage/item_7"
	if got != want {
		t.Fatalf("ChildExecutionID=%q, want %q", got, want)
	}
}

func TestNestedExecutionIDUsesCanonicalChildPrefix(t *testing.T) {
	id, err := NestedExecutionID(NestedInvocation{
		ParentExecutionID: "parent", ParentNodeID: "drafting stage",
		ParentApplicationID: "revision-3", FlowName: "writer flow",
	})
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "parent/drafting_stage/writer_flow-"
	if len(id) <= len(prefix) || id[:len(prefix)] != prefix {
		t.Fatalf("nested id=%q, want prefix %q", id, prefix)
	}
}

func containsString(value, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
