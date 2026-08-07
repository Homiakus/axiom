package adgo

import (
	"context"
	"testing"
)

func TestPebbleStoreDurabilityCatalogInboxAndVersions(t *testing.T) {
	root := t.TempDir()
	store, err := OpenPebbleStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	execution := &Execution{ID: "pebble/one", PlanID: "p", PlanVersion: "1", PlanDigest: "digest", Version: 1, Status: StatusRunning}
	ensureExecution(execution)
	if err := store.Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	current, err := store.Load(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(ctx, current.ID, current.Version, func(x *Execution) error {
		x.Status = StatusWaiting
		appendHistory(x, "test", "", "commit", nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutInbox(ctx, execution.ID, Event{ID: "event-1", Type: "Wake"}); err != nil {
		t.Fatal(err)
	}
	ids, err := store.ListExecutionIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != execution.ID {
		t.Fatalf("ids=%v", ids)
	}
	versions, err := store.ListVersions(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("versions=%v", versions)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenPebbleStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.Load(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusWaiting || loaded.Version != 2 {
		t.Fatalf("loaded status=%s version=%d", loaded.Status, loaded.Version)
	}
	inbox, err := reopened.ListInbox(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 || inbox[0].ID != "event-1" {
		t.Fatalf("inbox=%v", inbox)
	}
	if err := reopened.AckInbox(ctx, execution.ID, []string{"event-1"}); err != nil {
		t.Fatal(err)
	}
	inbox, err = reopened.ListInbox(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 0 {
		t.Fatalf("inbox not acknowledged: %v", inbox)
	}
}
