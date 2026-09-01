package adgo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWithOwnedFileLockRejectsTraversalFilename(t *testing.T) {
	locksDir := filepath.Join(t.TempDir(), "locks")
	cases := []string{
		"",
		".",
		"..",
		filepath.Join("sub", "escape.lock"),
		filepath.Join("..", "escape.lock"),
		`sub\escape.lock`,
	}

	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			called := false
			err := withOwnedFileLock(context.Background(), locksDir, name, time.Minute, func() error {
				called = true
				return nil
			})
			if err == nil {
				t.Fatalf("withOwnedFileLock(%q) unexpectedly accepted a non-leaf filename", name)
			}
			if called {
				t.Fatalf("withOwnedFileLock(%q) called the protected function after rejecting the filename", name)
			}
		})
	}
}

func TestFileStoreIgnoresSymlinkedCommitAndInboxRecords(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	execID := "safe"
	if err := os.MkdirAll(store.commitsDir(execID), privateStateDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.inboxDir(execID), privateStateDirMode); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	commitTarget := filepath.Join(outside, "commit.json")
	if err := os.WriteFile(commitTarget, []byte(`{"id":"escaped","version":1}`), privateLockFileMode); err != nil {
		t.Fatal(err)
	}
	commitLink := filepath.Join(store.commitsDir(execID), "00000000000000000001.json")
	if err := os.Symlink(commitTarget, commitLink); err != nil {
		t.Skipf("symlink creation is not available on this platform: %v", err)
	}

	if _, err := store.loadUnlocked(execID); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("loadUnlocked followed a symlinked commit: got %v, want %v", err, ErrExecutionNotFound)
	}
	ids, err := store.ListExecutionIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("ListExecutionIDs accepted a symlinked commit: %v", ids)
	}

	eventTarget := filepath.Join(outside, "event.json")
	if err := os.WriteFile(eventTarget, []byte(`{"id":"escaped-event","type":"signal"}`), privateLockFileMode); err != nil {
		t.Fatal(err)
	}
	eventLink := filepath.Join(store.inboxDir(execID), "escaped-event.json")
	if err := os.Symlink(eventTarget, eventLink); err != nil {
		t.Skipf("second symlink creation is not available on this platform: %v", err)
	}
	events, err := store.ListInbox(context.Background(), execID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("ListInbox accepted a symlinked event: %v", events)
	}
}

func TestFileStoreExecutionLockContainsTraversalLikeID(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}

	id := "../../outside"
	locksDir := filepath.Join(root, "locks")
	expected := filepath.Join(locksDir, EncodeDurableName(id)+".lock")
	if !IsContainedPath(locksDir, expected) {
		t.Fatalf("encoded lock path is not contained: %s", expected)
	}

	err = store.withExecutionLock(context.Background(), id, func() error {
		if _, err := os.Stat(expected); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
