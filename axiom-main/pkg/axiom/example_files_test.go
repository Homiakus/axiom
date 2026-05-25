package axiom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExamplesCompile(t *testing.T) {
	// Проверяем, что все публичные .axm примеры остаются валидными входными данными.
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "axiom-files", "*.axm"))
	if err != nil {
		t.Fatalf("Glob examples: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no examples found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if _, err := LoadModule(source); err != nil {
				t.Fatalf("LoadModule() error = %v", err)
			}
		})
	}
}

func TestTRIZExamplesCompile(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "triz", "*.axm"))
	if err != nil {
		t.Fatalf("Glob TRIZ examples: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no TRIZ examples found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if _, err := CompileAny(source, WithSourceName(path)); err != nil {
				t.Fatalf("CompileAny() error = %v", err)
			}
		})
	}
}
