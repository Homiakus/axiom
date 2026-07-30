package axiomfiles_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Homiakus/axiom"
)

func TestAXMExamplesCompile(t *testing.T) {
	paths, err := filepath.Glob("*.axm")
	if err != nil {
		t.Fatalf("glob AXM examples: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no .axm examples found")
	}

	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if _, err := axiom.CompileAny(source, axiom.WithSourceName(path)); err != nil {
				t.Fatalf("compile %s: %v", path, err)
			}
		})
	}
}
