package axiom

import (
	"strings"
	"testing"
)

func TestCompilePlanRejectsUnknownRuntimeProjection(t *testing.T) {
	source := []byte(`domain RuntimeValidation

context State:
  value: Int = 0

query Metadata:
  return:
    status = runtime.statuz
`)

	_, err := CompilePlan(source)
	if err == nil {
		t.Fatal("CompilePlan() succeeded, want unknown runtime projection error")
	}
	if !strings.Contains(err.Error(), "AX001") || !strings.Contains(err.Error(), "runtime.statuz") {
		t.Fatalf("CompilePlan() error = %v, want AX001 for runtime.statuz", err)
	}
}

func TestNewPlanRejectsUnknownRuntimeProjectionFromCompiledModule(t *testing.T) {
	source := []byte(`domain RuntimeValidation

query Metadata:
  return:
    typo = runtime.noSuchField
`)
	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = NewPlan(module, "axm", "test", AnalysisStatic)
	if err == nil {
		t.Fatal("NewPlan() succeeded, want unknown runtime projection error")
	}
	if !strings.Contains(err.Error(), "runtime.noSuchField") {
		t.Fatalf("NewPlan() error = %v, want runtime.noSuchField", err)
	}
}

func TestNewRejectsUnknownRuntimeProjectionFromCompiledModule(t *testing.T) {
	module, err := Compile([]byte(`domain RuntimeValidation

query Metadata:
  return:
    typo = runtime.noSuchField
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = New(module)
	if err == nil {
		t.Fatal("New() succeeded, want unknown runtime projection error")
	}
	if !strings.Contains(err.Error(), "AX001") || !strings.Contains(err.Error(), "runtime.noSuchField") {
		t.Fatalf("New() error = %v, want AX001 for runtime.noSuchField", err)
	}
}

func TestRuntimeQueryProjectionNamesAreStableAndSorted(t *testing.T) {
	got := RuntimeQueryProjectionNames()
	want := []string{
		"runtime.compilerVersion",
		"runtime.createdAt",
		"runtime.domain",
		"runtime.id",
		"runtime.moduleHash",
		"runtime.planVersion",
		"runtime.status",
		"runtime.updatedAt",
		"runtime.version",
	}
	if len(got) != len(want) {
		t.Fatalf("RuntimeQueryProjectionNames() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("RuntimeQueryProjectionNames()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}
