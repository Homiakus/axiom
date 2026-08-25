package adgo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLockForTest(t *testing.T, path, owner string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileLockRecord(file, fileLockRecord{Owner: owner, AcquiredAt: time.Now().UTC()}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseFileLockDoesNotDeleteReplacementOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.lock")
	writeLockForTest(t, path, "owner-a")

	// Simulate stale takeover: A's pathname disappears and B acquires the same
	// lock pathname before A finally runs its deferred release.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeLockForTest(t, path, "owner-b")

	if err := releaseFileLock(path, "owner-a"); err != nil {
		t.Fatal(err)
	}
	record, err := readFileLockRecord(path)
	if err != nil {
		t.Fatalf("replacement lock was deleted: %v", err)
	}
	if record.Owner != "owner-b" {
		t.Fatalf("owner=%q want owner-b", record.Owner)
	}

	if err := releaseFileLock(path, "owner-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("owner-b lock still exists: %v", err)
	}
}

func TestRemoveStaleFileLockAcceptsLegacyTimestampRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.lock")
	if err := os.WriteFile(path, []byte("1724600000000000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := removeStaleFileLock(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("legacy stale lock was not reclaimed")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy lock still exists: %v", err)
	}
}

func TestRemoveStaleFileLockKeepsFreshOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.lock")
	writeLockForTest(t, path, "owner-fresh")

	removed, err := removeStaleFileLock(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("fresh lock was reclaimed")
	}
	record, err := readFileLockRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	if record.Owner != "owner-fresh" {
		t.Fatalf("owner=%q want owner-fresh", record.Owner)
	}
}
