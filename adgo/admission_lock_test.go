package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileAdmissionLockCleanupDoesNotDeleteReplacementOwner(t *testing.T) {
	root := t.TempDir()
	controller, err := NewFileAdmissionController(root)
	if err != nil {
		t.Fatal(err)
	}

	const key = "tenant-a"
	locks := filepath.Join(root, "locks")
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(locks, "admission-"+hex.EncodeToString(sum[:12])+".lock")

	var replacementOwner string
	err = controller.withLock(context.Background(), key, func() error {
		current, err := readFileLockRecord(path)
		if err != nil {
			return err
		}
		if current.Owner == "" {
			t.Fatal("admission lock owner is empty")
		}

		// Simulate another process reclaiming the first owner's lock and
		// installing a replacement before the first owner's deferred cleanup.
		if err := os.Remove(path); err != nil {
			return err
		}
		replacementOwner, err = newFileLockOwner()
		if err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		record := fileLockRecord{Owner: replacementOwner, AcquiredAt: time.Now().UTC()}
		if err := writeFileLockRecord(file, record); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
	if err != nil {
		t.Fatal(err)
	}

	record, err := readFileLockRecord(path)
	if err != nil {
		t.Fatalf("replacement admission lock was removed by previous owner cleanup: %v", err)
	}
	if record.Owner != replacementOwner {
		t.Fatalf("replacement owner = %q, want %q", record.Owner, replacementOwner)
	}
}

func TestFileAdmissionLockWritesOwnershipRecord(t *testing.T) {
	controller, err := NewFileAdmissionController(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const key = "tenant-b"
	var owner string
	if err := controller.withLock(context.Background(), key, func() error {
		locks := filepath.Join(controller.root, "locks")
		sum := sha256.Sum256([]byte(key))
		path := filepath.Join(locks, "admission-"+hex.EncodeToString(sum[:12])+".lock")
		record, err := readFileLockRecord(path)
		if err != nil {
			return err
		}
		owner = record.Owner
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if owner == "" {
		t.Fatal("admission lock did not persist an ownership token")
	}
}
