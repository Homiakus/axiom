package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePlanAndNextActionable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	content := `# Plan
## A-001 — done
Status: **DONE**
## A-002 — external
Status: **EXTERNAL**
## A-003 — partial
Status: **PARTIAL**
## A-004 — todo
Status: **TODO**
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks, err := parsePlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 4 {
		t.Fatalf("len(tasks)=%d, want 4", len(tasks))
	}
	next, ok := nextActionable(tasks)
	if !ok {
		t.Fatal("expected actionable task")
	}
	if next.Heading != "A-003 — partial" || next.Status != "PARTIAL" {
		t.Fatalf("next=%+v", next)
	}
}

func TestDonePartialRemainsActionable(t *testing.T) {
	task, ok := nextActionable([]Task{{Heading: "X", Status: "DONE/PARTIAL"}})
	if !ok || task.Heading != "X" {
		t.Fatalf("nextActionable()=(%+v,%v)", task, ok)
	}
}
