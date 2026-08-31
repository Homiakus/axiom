package adgo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
