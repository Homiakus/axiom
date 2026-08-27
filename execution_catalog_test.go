package axiom

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func catalogFixtureActivity(context.Context, Input) (Output, error) {
	return Output{"ok": true}, nil
}

func TestExecutionCatalogSurvivesRetryWrapperAndPebbleReopen(t *testing.T) {
	module := compileDurableRetryModule(t)
	path := t.TempDir()
	ctx := context.Background()

	store, err := OpenPebble(path)
	if err != nil {
		t.Fatalf("OpenPebble(first) error = %v", err)
	}
	engine, err := New(module, WithStore(store), Act("Work", catalogFixtureActivity))
	if err != nil {
		_ = store.Close()
		t.Fatalf("New(first) error = %v", err)
	}
	for _, id := range []string{"catalog:b", "catalog:a"} {
		if err := engine.Start(ctx, id, nil); err != nil {
			_ = store.Close()
			t.Fatalf("Start(%q) error = %v", id, err)
		}
	}
	ids, err := engine.ListExecutionIDs(ctx)
	if err != nil {
		_ = store.Close()
		t.Fatalf("ListExecutionIDs(first) error = %v", err)
	}
	if want := []string{"catalog:a", "catalog:b"}; !reflect.DeepEqual(ids, want) {
		_ = store.Close()
		t.Fatalf("ids = %#v, want %#v", ids, want)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	reopened, err := OpenPebble(path)
	if err != nil {
		t.Fatalf("OpenPebble(reopen) error = %v", err)
	}
	defer reopened.Close()
	engine, err = New(module, WithStore(reopened), Act("Work", catalogFixtureActivity))
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	ids, err = engine.ListExecutionIDs(ctx)
	if err != nil {
		t.Fatalf("ListExecutionIDs(reopen) error = %v", err)
	}
	if want := []string{"catalog:a", "catalog:b"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("reopened ids = %#v, want %#v", ids, want)
	}
}

func TestExecutionCatalogMemoryIsSortedAndHonorsCancellation(t *testing.T) {
	module := compileDurableRetryModule(t)
	engine, err := New(module, WithStore(NewMemoryStore()), Act("Work", catalogFixtureActivity))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	for _, id := range []string{"z", "a", "m"} {
		if err := engine.Start(ctx, id, nil); err != nil {
			t.Fatalf("Start(%q) error = %v", id, err)
		}
	}
	ids, err := engine.ListExecutionIDs(ctx)
	if err != nil {
		t.Fatalf("ListExecutionIDs() error = %v", err)
	}
	if want := []string{"a", "m", "z"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %#v, want %#v", ids, want)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := engine.ListExecutionIDs(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListExecutionIDs(cancelled) error = %v, want context.Canceled", err)
	}
}
