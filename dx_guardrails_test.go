package axiom

import (
	"context"
	"strings"
	"testing"
)

type typedGuardInput struct {
	Value int `json:"value"`
}

type typedGuardOutput struct {
	Value int `json:"value"`
}

func TestActTypedRejectsScalarInputAtEngineConstruction(t *testing.T) {
	module, err := Compile([]byte("domain TypedGuard\n"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = New(module, ActTyped("Bad", func(context.Context, int) (typedGuardOutput, error) {
		return typedGuardOutput{}, nil
	}))
	if err == nil {
		t.Fatal("expected typed activity shape validation error")
	}
	if !strings.Contains(err.Error(), "AX507") || !strings.Contains(err.Error(), "input") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestActTypedRejectsScalarOutputAtEngineConstruction(t *testing.T) {
	module, err := Compile([]byte("domain TypedGuard\n"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = New(module, ActTyped("Bad", func(context.Context, typedGuardInput) (int, error) {
		return 1, nil
	}))
	if err == nil {
		t.Fatal("expected typed activity shape validation error")
	}
	if !strings.Contains(err.Error(), "AX507") || !strings.Contains(err.Error(), "output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestActTypedRejectsNilHandlerAtEngineConstruction(t *testing.T) {
	module, err := Compile([]byte("domain TypedGuard\n"))
	if err != nil {
		t.Fatal(err)
	}

	var handler func(context.Context, typedGuardInput) (typedGuardOutput, error)
	_, err = New(module, ActTyped("Bad", handler))
	if err == nil {
		t.Fatal("expected nil typed activity validation error")
	}
	if !strings.Contains(err.Error(), "AX507") || !strings.Contains(err.Error(), "must not be nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStructToOutputSupportsNamedStringKeyMaps(t *testing.T) {
	type namedOutput map[string]string

	got := structToOutput(namedOutput{"status": "ok"})
	if got["status"] != "ok" {
		t.Fatalf("status = %#v, want %q", got["status"], "ok")
	}
}

func TestProductionModeAcceptsImplementedPolicyModes(t *testing.T) {
	for _, mode := range []string{"parallel", "once", "latest", "first"} {
		t.Run(mode, func(t *testing.T) {
			module, err := Compile([]byte(`domain ProductionGuard

policy delivery:
  retry: 3
  timeout: 2s
  concurrency: ` + mode + `
`))
			if err != nil {
				t.Fatal(err)
			}

			store, err := OpenPebble(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			if _, err = New(module, WithStore(store), WithProductionMode()); err != nil {
				t.Fatalf("production mode rejected implemented policy %s: %v", mode, err)
			}
		})
	}
}

func TestProductionModeRejectsUnknownConcurrencyMode(t *testing.T) {
	module, err := Compile([]byte(`domain ProductionGuard

policy delivery:
  concurrency: newest
`))
	if err != nil {
		t.Fatal(err)
	}

	store, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, err = New(module, WithStore(store), WithProductionMode())
	if err == nil {
		t.Fatal("expected production concurrency validation error")
	}
	if !strings.Contains(err.Error(), "AX508") || !strings.Contains(err.Error(), "delivery.concurrency") {
		t.Fatalf("unexpected error: %v", err)
	}
}
