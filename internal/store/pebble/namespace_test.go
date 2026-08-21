package pebble

import (
	"context"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
)

// CE-009: Executions "a" and "a/b" must have strictly isolated history/task lists.
func TestPebbleExecutionNamespaceIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("failed to open pebble store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	e1 := &runtime.Execution{
		ID:        "a",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	e2 := &runtime.Execution{
		ID:        "a/b",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := store.CreateExecution(ctx, e1); err != nil {
		t.Fatalf("failed to create execution a: %v", err)
	}
	if err := store.CreateExecution(ctx, e2); err != nil {
		t.Fatalf("failed to create execution a/b: %v", err)
	}

	if err := store.AppendHistory(ctx, "a", "event_a", map[string]any{"target": "a"}); err != nil {
		t.Fatalf("failed to append history for a: %v", err)
	}
	if err := store.AppendHistory(ctx, "a/b", "event_ab", map[string]any{"target": "a/b"}); err != nil {
		t.Fatalf("failed to append history for a/b: %v", err)
	}

	histA, err := store.ListHistory(ctx, "a")
	if err != nil {
		t.Fatalf("ListHistory(a) failed: %v", err)
	}
	if len(histA) != 1 {
		t.Fatalf("expected 1 history entry for 'a', got %d", len(histA))
	}
	if histA[0].Type != "event_a" {
		t.Fatalf("expected history entry 'event_a' for 'a', got %q", histA[0].Type)
	}

	histAB, err := store.ListHistory(ctx, "a/b")
	if err != nil {
		t.Fatalf("ListHistory(a/b) failed: %v", err)
	}
	if len(histAB) != 1 {
		t.Fatalf("expected 1 history entry for 'a/b', got %d", len(histAB))
	}
	if histAB[0].Type != "event_ab" {
		t.Fatalf("expected history entry 'event_ab' for 'a/b', got %q", histAB[0].Type)
	}
}

// CE-010: Task IDs "/" and "%2f" must produce distinct task keys and not collide.
func TestPebbleKeyEncodingInjective(t *testing.T) {
	k1 := escape("/")
	k2 := escape("%2f")

	if k1 == k2 {
		t.Fatalf("escape collision: escape('/') == %q, escape('%%2f') == %q", k1, k2)
	}
	if k1 != "%2f" {
		t.Fatalf("expected escape('/') to be '%%2f', got %q", k1)
	}
	if k2 != "%252f" {
		t.Fatalf("expected escape('%%2f') to be '%%252f', got %q", k2)
	}
}
