package axiom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type transactionalWithoutDurability struct {
	Store
	tx TransactionalStore
}

func (s transactionalWithoutDurability) BeginTransaction(ctx context.Context) (StoreTransaction, error) {
	return s.tx.BeginTransaction(ctx)
}

func productionDurabilityModule(t *testing.T) *Module {
	t.Helper()
	module, err := Compile([]byte("domain ProductionDurability\n"))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return module
}

func assertProductionConfigError(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatal("New() expected production durability error")
	}
	var diagnostics Errors
	if !errors.As(err, &diagnostics) {
		t.Fatalf("New() error type = %T, want axiom.Errors", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "AX506" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, contains) {
		t.Fatalf("message = %q, want substring %q", diagnostics[0].Message, contains)
	}
}

func TestProductionModeAcceptsSynchronousPebble(t *testing.T) {
	store, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := store.Durability(); got != StoreDurabilitySynchronous {
		t.Fatalf("Durability() = %q, want %q", got, StoreDurabilitySynchronous)
	}
	if _, err := New(productionDurabilityModule(t), WithStore(store), WithProductionMode()); err != nil {
		t.Fatalf("New() rejected synchronous Pebble: %v", err)
	}
}

func TestProductionModeRejectsTransactionalStoreWithoutDurabilityDeclaration(t *testing.T) {
	store, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	wrapped := transactionalWithoutDurability{Store: store, tx: store}
	_, err = New(productionDurabilityModule(t), WithStore(wrapped), WithProductionMode())
	assertProductionConfigError(t, err, "declare durability")
}

func TestProductionModeRejectsPebbleNoSync(t *testing.T) {
	store, err := OpenPebble(t.TempDir(), PebbleNoSync())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := store.Durability(); got != StoreDurabilityBestEffort {
		t.Fatalf("Durability() = %q, want %q", got, StoreDurabilityBestEffort)
	}
	_, err = New(productionDurabilityModule(t), WithStore(store), WithProductionMode())
	assertProductionConfigError(t, err, "synchronous durability")
}

func TestProductionModeRejectsPebbleBufferedSync(t *testing.T) {
	store, err := OpenPebble(t.TempDir(), PebbleSyncEvery(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if got := store.Durability(); got != StoreDurabilityBuffered {
		t.Fatalf("Durability() = %q, want %q", got, StoreDurabilityBuffered)
	}
	_, err = New(productionDurabilityModule(t), WithStore(store), WithProductionMode())
	assertProductionConfigError(t, err, "synchronous durability")
}

func TestMemoryStoreDeclaresEphemeralDurability(t *testing.T) {
	store := NewMemoryStore()
	provider, ok := store.(DurabilityProvider)
	if !ok {
		t.Fatal("memory store does not implement DurabilityProvider")
	}
	if got := provider.Durability(); got != StoreDurabilityEphemeral {
		t.Fatalf("Durability() = %q, want %q", got, StoreDurabilityEphemeral)
	}
}
