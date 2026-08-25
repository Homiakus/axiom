package pebble

import (
	"context"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
)

func TestTransactionCreateExecutionDoesNotRetainCallerPointer(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	tx, err := store.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	createdAt := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	execution := &runtime.Execution{
		ID:        "create-isolation",
		Domain:    "original",
		Version:   3,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		Context: map[string]map[string]any{
			"State": {"value": 1},
		},
	}
	if err := tx.CreateExecution(ctx, execution); err != nil {
		t.Fatal(err)
	}

	// Mutating the caller-owned object after CreateExecution must not alter the
	// transaction's staged state.
	execution.Domain = "mutated"
	execution.Context["State"]["value"] = 99

	staged, err := tx.GetExecution(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Domain != "original" {
		t.Fatalf("staged domain = %q, want original", staged.Domain)
	}
	if got := staged.Context["State"]["value"]; got != 1 {
		t.Fatalf("staged context value = %v, want 1", got)
	}
	if staged.Version != 3 {
		t.Fatalf("staged version = %d, want 3", staged.Version)
	}
}

func TestTransactionSaveExecutionDoesNotMutateCaller(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	createdAt := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	original := &runtime.Execution{
		ID:        "save-isolation",
		Domain:    "before",
		Version:   7,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		Context: map[string]map[string]any{
			"State": {"value": 1},
		},
	}
	if err := store.CreateExecution(ctx, original); err != nil {
		t.Fatal(err)
	}

	caller, err := store.GetExecution(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	caller.Domain = "saved"
	caller.Context["State"]["value"] = 2
	callerVersion := caller.Version
	callerUpdatedAt := caller.UpdatedAt

	tx, err := store.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.SaveExecution(ctx, caller); err != nil {
		t.Fatal(err)
	}

	if caller.Version != callerVersion {
		t.Fatalf("SaveExecution mutated caller version: got %d want %d", caller.Version, callerVersion)
	}
	if !caller.UpdatedAt.Equal(callerUpdatedAt) {
		t.Fatalf("SaveExecution mutated caller UpdatedAt: got %v want %v", caller.UpdatedAt, callerUpdatedAt)
	}

	// A later caller mutation must not leak into the staged copy either.
	caller.Domain = "mutated-after-save"
	caller.Context["State"]["value"] = 99

	staged, err := tx.GetExecution(ctx, caller.ID)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Version != callerVersion+1 {
		t.Fatalf("staged version = %d, want %d", staged.Version, callerVersion+1)
	}
	if !staged.UpdatedAt.After(callerUpdatedAt) {
		t.Fatalf("staged UpdatedAt = %v, want after %v", staged.UpdatedAt, callerUpdatedAt)
	}
	if staged.Domain != "saved" {
		t.Fatalf("staged domain = %q, want saved", staged.Domain)
	}
	if got := staged.Context["State"]["value"]; got != 2 {
		t.Fatalf("staged context value = %v, want 2", got)
	}
}
