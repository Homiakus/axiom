package syncx

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestKeyedLockerBasicSerialisation verifies that Lock() on the same key
// serialises operations, while independent keys can run concurrently.
// ──────────────────────────────────────────────────────────────────────────────

func TestKeyedLockerBasicSerialisation(t *testing.T) {
	kl := NewKeyedLocker()

	// Acquire lock on key "A".
	unlock := kl.Lock("A")

	// Start a goroutine that tries to lock "A" — it should block.
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		u := kl.Lock("A")
		u()
		close(done)
	}()

	<-started
	runtime.Gosched() // give the goroutine a chance to block

	// The goroutine should still be waiting.
	select {
	case <-done:
		t.Fatal("second Lock(A) should block while first is held")
	default:
		// expected: still blocked
	}

	// Release — second goroutine should proceed.
	unlock()
	<-done
}

func TestKeyedLockerIndependentKeysDoNotBlock(t *testing.T) {
	kl := NewKeyedLocker()

	unlockA := kl.Lock("A")
	defer unlockA()

	// Locking "B" should not block.
	done := make(chan struct{})
	go func() {
		u := kl.Lock("B")
		u()
		close(done)
	}()

	<-done // should return immediately
}

// TestKeyedLockerConcurrentCorrectness runs many goroutines competing
// for the same key and verifies that a shared counter is correctly
// incremented (no lost updates).
func TestKeyedLockerConcurrentCorrectness(t *testing.T) {
	kl := NewKeyedLocker()
	const workers = 8
	const perWorker = 500
	var counter int64

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perWorker; j++ {
				unlock := kl.Lock("shared")
				// Critical section: non-atomic increment.
				c := atomic.LoadInt64(&counter)
				c++
				atomic.StoreInt64(&counter, c)
				unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&counter); got != int64(workers*perWorker) {
		t.Fatalf("counter = %d, want %d", got, workers*perWorker)
	}
}

// TestKeyedLockerCleanup verifies that releasing a lock removes the
// internal map entry when no other goroutine is waiting (refs == 0).
func TestKeyedLockerCleanup(t *testing.T) {
	kl := NewKeyedLocker()

	unlock := kl.Lock("temp")
	unlock()

	// After unlock with no waiters, the map entry should be cleaned up.
	kl.mu.Lock()
	_, exists := kl.locks["temp"]
	kl.mu.Unlock()

	if exists {
		t.Fatal("lock entry should be cleaned up after last unlock")
	}
}

// TestKeyedLockerMultipleKeys verifies correctness with many distinct keys.
func TestKeyedLockerMultipleKeys(t *testing.T) {
	kl := NewKeyedLocker()
	const keys = 100
	const opsPerKey = 50

	counters := make([]int64, keys)

	var wg sync.WaitGroup
	for k := 0; k < keys; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			key := string(rune('A' + k%26)) + string(rune('0'+k/26))
			for i := 0; i < opsPerKey; i++ {
				unlock := kl.Lock(key)
				counters[k]++
				unlock()
			}
		}(k)
	}
	wg.Wait()

	for k := 0; k < keys; k++ {
		if counters[k] != int64(opsPerKey) {
			t.Fatalf("counter[%d] = %d, want %d", k, counters[k], opsPerKey)
		}
	}
}

// TestKeyedLockerReentrantDifferentKeys ensures acquiring multiple
// different keys from the same goroutine does not deadlock.
func TestKeyedLockerReentrantDifferentKeys(t *testing.T) {
	kl := NewKeyedLocker()

	unlockA := kl.Lock("A")
	unlockB := kl.Lock("B")
	unlockC := kl.Lock("C")

	unlockC()
	unlockB()
	unlockA()
}
