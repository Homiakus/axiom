package table

import (
	"strings"
	"testing"

	"github.com/Homiakus/axiom"
)

func TestRenderPolicyUsesCanonicalAXMSyntax(t *testing.T) {
	source, err := render(document{
		Workflow: workflow{Name: "PolicyExample"},
		Policies: []policy{{
			Name:        "external",
			Retry:       2,
			Timeout:     "5s",
			Concurrency: "once",
			Idempotency: "required",
		}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"retry: 2",
		"timeout: 5s",
		"concurrency: once",
		"idempotency: required",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("rendered policy missing %q:\n%s", want, source)
		}
	}
	if strings.Contains(source, "retry =") {
		t.Fatalf("rendered policy uses non-canonical assignment syntax:\n%s", source)
	}

	if _, err := axiom.CompilePlan([]byte(source), axiom.WithSourceName("rendered-table.axm")); err != nil {
		t.Fatalf("canonical policy source should compile: %v\n%s", err, source)
	}
}
