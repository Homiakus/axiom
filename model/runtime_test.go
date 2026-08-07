package model

import (
	"strings"
	"testing"
)

func TestRuntimeNamespaceRendersStableProjectionNames(t *testing.T) {
	definition := New("RuntimeMetadata")
	definition.Query("Metadata", map[string]Expr{
		"id":              Runtime.ID(),
		"domain":          Runtime.Domain(),
		"status":          Runtime.Status(),
		"version":         Runtime.Version(),
		"createdAt":       Runtime.CreatedAt(),
		"updatedAt":       Runtime.UpdatedAt(),
		"moduleHash":      Runtime.ModuleHash(),
		"compilerVersion": Runtime.CompilerVersion(),
		"planVersion":     Runtime.PlanVersion(),
	})

	source := definition.Source()
	for _, projection := range []string{
		"runtime.id",
		"runtime.domain",
		"runtime.status",
		"runtime.version",
		"runtime.createdAt",
		"runtime.updatedAt",
		"runtime.moduleHash",
		"runtime.compilerVersion",
		"runtime.planVersion",
	} {
		if !strings.Contains(source, projection) {
			t.Fatalf("Source() = %q, missing %s", source, projection)
		}
	}
	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}
