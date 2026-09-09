package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/runtime"
	"github.com/Homiakus/axiom/store/postgres"
)

func TestPublicPostgresStore(t *testing.T) {
	dbName := fmt.Sprintf("test_pub_%d", time.Now().UnixNano())
	store, err := postgres.Open("pgsim", dbName, postgres.WithLeaseTTL(time.Minute))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	exec := &runtime.Execution{
		ID:        "pub-exec-1",
		Status:    runtime.StatusRunning,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}

	loaded, err := store.GetExecution(ctx, "pub-exec-1")
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if loaded.ID != "pub-exec-1" {
		t.Fatalf("unexpected ID: %s", loaded.ID)
	}
}
