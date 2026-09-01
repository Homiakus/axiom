package adgo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOwnedFileLockIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not the Windows access-control contract")
	}

	locksDir := t.TempDir()
	const filename = "private.lock"
	lockPath := filepath.Join(locksDir, filename)

	err := withOwnedFileLock(context.Background(), locksDir, filename, time.Minute, func() error {
		info, err := os.Stat(lockPath)
		if err != nil {
			return err
		}
		if got := info.Mode().Perm(); got != privateLockFileMode {
			t.Fatalf("lock mode = %04o, want %04o", got, privateLockFileMode)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFileBackedStateDirectoriesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not the Windows access-control contract")
	}

	assertPrivateDir := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != privateStateDirMode {
			t.Fatalf("directory %s mode = %04o, want %04o", path, got, privateStateDirMode)
		}
	}

	base := t.TempDir()

	storeRoot := filepath.Join(base, "store")
	store, err := NewFileStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(filepath.Join(storeRoot, "executions"))
	assertPrivateDir(filepath.Join(storeRoot, "locks"))
	execution := newTestExecution("private-dir-mode", 1)
	if err := store.Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(store.commitsDir(execution.ID))
	assertPrivateDir(store.inboxDir(execution.ID))

	cacheRoot := filepath.Join(base, "cache")
	if _, err := NewFileActivityCache(cacheRoot); err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(filepath.Join(cacheRoot, "activity-cache"))
	assertPrivateDir(filepath.Join(cacheRoot, "locks"))

	scheduleRoot := filepath.Join(base, "schedule")
	if _, err := NewFileScheduleStore(scheduleRoot); err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(filepath.Join(scheduleRoot, "schedules"))
	assertPrivateDir(filepath.Join(scheduleRoot, "locks"))

	routerRoot := filepath.Join(base, "router")
	if _, err := NewFileProviderHealthStore(routerRoot); err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(filepath.Join(routerRoot, "provider-health"))
	assertPrivateDir(filepath.Join(routerRoot, "locks"))

	admissionRoot := filepath.Join(base, "admission")
	if _, err := NewFileAdmissionController(admissionRoot); err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(filepath.Join(admissionRoot, "admission"))
	assertPrivateDir(filepath.Join(admissionRoot, "locks"))

	ownedLocks := filepath.Join(base, "owned-locks")
	if err := withOwnedFileLock(context.Background(), ownedLocks, "test.lock", time.Minute, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(ownedLocks)

	artifactRoot := filepath.Join(base, "artifacts")
	artifacts, err := NewContentAddressedStore(artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(filepath.Join(artifactRoot, "sha256"))
	ref, err := artifacts.Put("private.txt", "text/plain", strings.NewReader("private artifact"))
	if err != nil {
		t.Fatal(err)
	}
	artifactPath, err := artifacts.path(ref)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateDir(filepath.Dir(artifactPath))
}
