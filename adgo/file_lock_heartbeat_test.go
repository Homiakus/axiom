package adgo

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRefreshFileLockTouchesOnlyOwnedInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.lock")
	writeLockForTest(t, path, "owner-a")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err := refreshFileLock(file, path, "owner-a"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().After(before.ModTime()) {
		t.Fatalf("heartbeat did not refresh mtime: before=%v after=%v", before.ModTime(), after.ModTime())
	}
	record, err := readFileLockRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	if record.Owner != "owner-a" {
		t.Fatalf("owner=%q want owner-a", record.Owner)
	}
}

func TestRefreshFileLockCannotReviveReplacementOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replaced.lock")
	writeLockForTest(t, path, "owner-a")
	oldFile, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer oldFile.Close()

	if runtime.GOOS == "windows" {
		// Windows does not permit unlinking this open lock handle. Simulate the
		// ownership-change half of takeover on the same inode; refresh must still
		// reject the old owner rather than extending the new lease.
		if err := oldFile.Truncate(0); err != nil {
			t.Fatal(err)
		}
		if _, err := oldFile.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		if err := writeFileLockRecord(oldFile, fileLockRecord{Owner: "owner-b", AcquiredAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		if err := oldFile.Sync(); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		writeLockForTest(t, path, "owner-b")
	}

	if err := refreshFileLock(oldFile, path, "owner-a"); !errors.Is(err, errFileLockLost) {
		t.Fatalf("refresh err=%v want %v", err, errFileLockLost)
	}
	record, err := readFileLockRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	if record.Owner != "owner-b" {
		t.Fatalf("replacement owner=%q want owner-b", record.Owner)
	}
}

func TestFileLockHeartbeatIntervalMaintainsLeaseMargin(t *testing.T) {
	staleAfter := 30 * time.Second
	interval := fileLockHeartbeatInterval(staleAfter)
	if interval <= 0 || interval >= staleAfter {
		t.Fatalf("interval=%v staleAfter=%v", interval, staleAfter)
	}
	if interval != 10*time.Second {
		t.Fatalf("interval=%v want 10s", interval)
	}
}
