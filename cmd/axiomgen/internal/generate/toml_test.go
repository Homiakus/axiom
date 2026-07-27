package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewTOMLFrontend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.toml")
	source := `[workflow]
name = "GeneratedTable"

[state.Counter]
value = 0

[[event]]
name = "SetValue"
[event.fields]
value = "Int"

[[transition]]
name = "set"
on = "SetValue"
[transition.set]
"Counter.value" = "signal.value"
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := Preview(Request{File: path, OutDir: dir, PackageName: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Domain != "GeneratedTable" {
		t.Fatalf("domain = %q", plan.Domain)
	}
	if len(plan.files) == 0 || !strings.Contains(string(plan.files[0].Content), "table.Parse") {
		t.Fatalf("generated TOML loader is missing table.Parse")
	}
}
