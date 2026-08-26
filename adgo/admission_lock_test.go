package adgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileAdmissionLockCleanupDoesNotDeleteReplacementOwner(t *testing.T) {
	root := t.TempDir()
	_, err := NewFileAdmissionController(root)
	if err != nil {
		t.Fatal(err)
	}

	const key = "tenant-a"
	locks := filepath.Join(root, "locks")
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(locks, "admission-"+hex.EncodeToString(sum[:12])+".lock")

	ownerA, err := newFileLockOwner()
	if err != nil {
		t.Fatal(err)
	}
	writeLockForTest(t, path, ownerA)

	// Simulate stale takeover: A's pathname disappears and B acquires the same
	// lock pathname before A runs its delayed release (ABA scenario).
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ownerB, err := newFileLockOwner()
	if err != nil {
		t.Fatal(err)
	}
	writeLockForTest(t, path, ownerB)

	if err := releaseFileLock(path, ownerA); err != nil {
		t.Fatal(err)
	}

	record, err := readFileLockRecord(path)
	if err != nil {
		t.Fatalf("replacement admission lock was removed by previous owner cleanup: %v", err)
	}
	if record.Owner != ownerB {
		t.Fatalf("replacement owner = %q, want %q", record.Owner, ownerB)
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

func TestFileAdmissionLockReclaimsStaleLock(t *testing.T) {
	root := t.TempDir()
	controller, err := NewFileAdmissionController(root)
	if err != nil {
		t.Fatal(err)
	}
	controller.lockStaleAfter = 50 * time.Millisecond

	const key = "tenant-stale"
	locks := filepath.Join(root, "locks")
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(locks, "admission-"+hex.EncodeToString(sum[:12])+".lock")

	writeLockForTest(t, path, "dead-owner")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	var executed bool
	err = controller.withLock(context.Background(), key, func() error {
		executed = true
		record, err := readFileLockRecord(path)
		if err != nil {
			return err
		}
		if record.Owner == "dead-owner" || record.Owner == "" {
			t.Fatalf("stale owner was not replaced: %q", record.Owner)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withLock failed on stale lock: %v", err)
	}
	if !executed {
		t.Fatal("withLock body was not executed")
	}
}

func TestFileAdmissionLockLiveOwnerSafety(t *testing.T) {
	root := t.TempDir()
	controller, err := NewFileAdmissionController(root)
	if err != nil {
		t.Fatal(err)
	}
	controller.lockStaleAfter = time.Hour

	const key = "tenant-live"
	acquired := make(chan struct{})
	release := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = controller.withLock(context.Background(), key, func() error {
			close(acquired)
			<-release
			return nil
		})
	}()

	<-acquired
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = controller.withLock(ctx, key, func() error {
		t.Fatal("acquired lock while live owner holds it")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context timeout/cancellation, got %v", err)
	}

	close(release)
	wg.Wait()
}
