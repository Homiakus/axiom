package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCreatesStubsOnce(t *testing.T) {
	out := t.TempDir()
	source := filepath.Join(repoRoot(t), "examples", "axiom-files", "welcome.axm")
	req := Request{File: source, OutDir: out, PackageName: "generated"}

	first, err := Run(req)
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if len(first.Written) != 2 {
		t.Fatalf("first written = %d, want 2: %#v", len(first.Written), first.Written)
	}

	stub := filepath.Join(out, "welcome_activities.go")
	sentinel := []byte("package generated\n\n// hand edited\n")
	if err := os.WriteFile(stub, sentinel, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	second, err := Run(req)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(second.Skipped) != 1 || second.Skipped[0] != stub {
		t.Fatalf("skipped = %#v, want only %s", second.Skipped, stub)
	}
	got, err := os.ReadFile(stub)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(sentinel) {
		t.Fatalf("stub was overwritten:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(out, "welcome_axiom.gen.go")); err != nil {
		t.Fatalf("generated file missing: %v", err)
	}
}

func TestPreviewDefaultsPackageFromOutDir(t *testing.T) {
	out := filepath.Join(t.TempDir(), "flows")
	source := filepath.Join(repoRoot(t), "examples", "axiom-files", "welcome.axm")

	plan, err := Preview(Request{File: source, OutDir: out})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if plan.Package != "flows" {
		t.Fatalf("package = %q, want flows", plan.Package)
	}
	if plan.Domain != "Welcome" {
		t.Fatalf("domain = %q, want Welcome", plan.Domain)
	}
}

func TestPreviewDefaultsOutDirToSourceDir(t *testing.T) {
	source := filepath.Join(repoRoot(t), "examples", "axiom-files", "welcome.axm")

	plan, err := Preview(Request{File: source})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	wantOut := filepath.Dir(source)
	if plan.OutDir != wantOut {
		t.Fatalf("out = %q, want %q", plan.OutDir, wantOut)
	}
	if plan.Package != "axiom_files" {
		t.Fatalf("package = %q, want axiom_files", plan.Package)
	}
	for _, file := range plan.Files {
		if filepath.Dir(file.Path) != wantOut {
			t.Fatalf("file path = %q, want dir %q", file.Path, wantOut)
		}
	}
}

func TestPreviewAcceptsTRIZSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "switcher.axm")
	if err := os.WriteFile(source, []byte(`system Switcher

state System:
  running: Bool = false

event Start(zone: Int)

profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent

function TurnOn(zone: Int) -> { ok: Bool }

rule ApplyTurnOn when:
  Start
do critical:
  result = TurnOn(event.zone)
then:
  set System.running = result.ok
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	plan, err := Preview(Request{File: source, OutDir: dir, PackageName: "generated"})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if plan.Domain != "Switcher" {
		t.Fatalf("domain = %q, want Switcher", plan.Domain)
	}
	found := false
	for _, file := range plan.files {
		if strings.Contains(string(file.Content), "type TurnOnInput struct") {
			found = true
		}
	}
	if !found {
		t.Fatalf("generated files did not include TurnOn input type")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	return root
}
