package adgo

import (
	"context"
	"path/filepath"
	"testing"
)

// CE-001: execution ID ".." must stay strictly contained under store's root and not escape to root.
func TestFileStoreIdentityContainment(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create FileStore: %v", err)
	}

	execDir := store.executionDir("..")
	executionsRoot := filepath.Join(tempDir, "executions")
	if !IsContainedPath(executionsRoot, execDir) {
		t.Fatalf("expected execDir %q to be contained under %q", execDir, executionsRoot)
	}

	// Creating an execution with ID ".." must not create files in tempDir root
	e := &Execution{
		ID:          "..",
		PlanID:      "test-plan",
		PlanVersion: "1.0",
		PlanDigest:  "sha256:abc",
		Status:      StatusRunning,
	}
	ctx := context.Background()
	if err := store.Create(ctx, e); err != nil {
		t.Fatalf("store.Create with id '..' failed: %v", err)
	}

	loaded, err := store.Load(ctx, "..")
	if err != nil {
		t.Fatalf("store.Load with id '..' failed: %v", err)
	}
	if loaded.ID != ".." {
		t.Fatalf("expected loaded.ID to be '..', got %q", loaded.ID)
	}
}

// CE-002: IDs "a/b" and "a?b" and "a_b" must have distinct durable representations and separate files.
func TestIdentityInjectiveExamples(t *testing.T) {
	cases := []string{
		"a/b",
		"a?b",
		"a_b",
		"a:b",
		"a%2Fb",
		"..",
		".",
		"",
		"user/profile/1",
		"user?profile?1",
		"hello world",
		"unicode_тест_🚀",
	}

	seen := make(map[string]string)
	for _, c := range cases {
		encoded := EncodeDurableName(c)
		if orig, ok := seen[encoded]; ok {
			t.Fatalf("collision detected: EncodeDurableName(%q) == EncodeDurableName(%q) == %q", c, orig, encoded)
		}
		seen[encoded] = c

		decoded, err := DecodeDurableName(encoded)
		if err != nil {
			t.Fatalf("DecodeDurableName failed for %q (encoded %q): %v", c, encoded, err)
		}
		if decoded != c {
			t.Fatalf("round-trip mismatch: got %q, want %q", decoded, c)
		}
	}
}

// CE-003: events ("a|b","c",p) and ("a","b|c",p) must produce different event IDs.
func TestEventIDComponentFraming(t *testing.T) {
	e1 := Event{
		Type:       "a|b",
		TargetNode: "c",
		Payload:    []byte("payload"),
	}
	e2 := Event{
		Type:       "a",
		TargetNode: "b|c",
		Payload:    []byte("payload"),
	}

	id1 := CanonicalEventID(e1)
	id2 := CanonicalEventID(e2)

	if id1 == id2 {
		t.Fatalf("expected distinct event IDs for framed components, got both = %q", id1)
	}
}

// CE-004: two child items "a/b", "a?b", "a_b" must produce distinct durable encoded names.
func TestChildExecutionIDDistinctItems(t *testing.T) {
	parent := "exec-parent"
	node := "node-1"

	child1 := parent + "/" + EncodeDurableName(node) + "/" + EncodeDurableName("a/b")
	child2 := parent + "/" + EncodeDurableName(node) + "/" + EncodeDurableName("a?b")
	child3 := parent + "/" + EncodeDurableName(node) + "/" + EncodeDurableName("a_b")

	if child1 == child2 {
		t.Fatalf("expected child1 (%q) != child2 (%q)", child1, child2)
	}
	if child1 == child3 {
		t.Fatalf("expected child1 (%q) != child3 (%q)", child1, child3)
	}
	if child2 == child3 {
		t.Fatalf("expected child2 (%q) != child3 (%q)", child2, child3)
	}
}
