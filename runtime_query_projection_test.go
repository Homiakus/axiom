package axiom

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeQueryProjections(t *testing.T) {
	module, err := Compile([]byte(`domain RuntimeMetadata

context State:
  value: Int = 0

query Metadata:
  return:
    id = runtime.id
    domain = runtime.domain
    status = runtime.status
    version = runtime.version
    createdAt = runtime.createdAt
    updatedAt = runtime.updatedAt
    moduleHash = runtime.moduleHash
    compilerVersion = runtime.compilerVersion
    planVersion = runtime.planVersion
`))
	if err != nil {
		t.Fatal(err)
	}

	engine, err := New(module)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "execution-42", nil); err != nil {
		t.Fatal(err)
	}

	got, err := engine.Query(ctx, "execution-42", "Metadata")
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "execution-42" {
		t.Fatalf("id = %#v", got["id"])
	}
	if got["domain"] != "RuntimeMetadata" {
		t.Fatalf("domain = %#v", got["domain"])
	}
	if got["status"] != StatusStarted {
		t.Fatalf("status = %#v, want %q", got["status"], StatusStarted)
	}
	if _, ok := got["version"].(int); !ok {
		t.Fatalf("version = %#v, want int", got["version"])
	}
	createdAt, ok := got["createdAt"].(time.Time)
	if !ok || createdAt.IsZero() {
		t.Fatalf("createdAt = %#v, want non-zero time.Time", got["createdAt"])
	}
	updatedAt, ok := got["updatedAt"].(time.Time)
	if !ok || updatedAt.IsZero() {
		t.Fatalf("updatedAt = %#v, want non-zero time.Time", got["updatedAt"])
	}
	if got["moduleHash"] != module.CompiledHash {
		t.Fatalf("moduleHash = %#v, want %q", got["moduleHash"], module.CompiledHash)
	}
	if got["compilerVersion"] != module.CompilerVersion {
		t.Fatalf("compilerVersion = %#v, want %q", got["compilerVersion"], module.CompilerVersion)
	}
	if got["planVersion"] != module.PlanVersion {
		t.Fatalf("planVersion = %#v, want %q", got["planVersion"], module.PlanVersion)
	}
}
