package adgo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestHelperProcess serves as the entrypoint when running as a child process.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("AXIOM_TEST_SUBPROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "no subcommand provided\n")
		os.Exit(2)
	}

	cmd := args[0]
	switch cmd {
	case "hold-lock":
		// hold-lock <locksDir> <filename> <holdDurationMs>
		if len(args) < 4 {
			os.Exit(2)
		}
		locksDir := args[1]
		filename := args[2]
		durationMs, _ := strconv.Atoi(args[3])
		ctx := context.Background()
		_ = withOwnedFileLock(ctx, locksDir, filename, time.Minute, func() error {
			// Signal readiness to parent
			fmt.Printf("READY\n")
			_ = os.Stdout.Sync()
			time.Sleep(time.Duration(durationMs) * time.Millisecond)
			return nil
		})

	case "commit-increment":
		// commit-increment <storeDir> <execID> <attempts>
		if len(args) < 4 {
			os.Exit(2)
		}
		storeDir := args[1]
		execID := args[2]
		attempts, _ := strconv.Atoi(args[3])
		store, err := NewFileStore(storeDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "store open err: %v\n", err)
			os.Exit(1)
		}
		ctx := context.Background()
		for i := 0; i < attempts; i++ {
			for {
				exec, err := store.Load(ctx, execID)
				if err != nil {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				_, err = store.Commit(ctx, execID, exec.Version, func(e *Execution) error {
					if e.Data == nil {
						e.Data = make(map[string]json.RawMessage)
					}
					var current int
					if raw, ok := e.Data["count"]; ok {
						_ = json.Unmarshal(raw, &current)
					}
					nextVal, _ := json.Marshal(current + 1)
					e.Data["count"] = nextVal
					appendHistory(e, "child", "", "increment", map[string]any{"step": current + 1})
					return nil
				})
				if err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}

	case "put-inbox":
		// put-inbox <storeDir> <execID> <eventID>
		if len(args) < 4 {
			os.Exit(2)
		}
		storeDir := args[1]
		execID := args[2]
		eventID := args[3]
		store, err := NewFileStore(storeDir)
		if err != nil {
			os.Exit(1)
		}
		ctx := context.Background()
		if err := store.PutInbox(ctx, execID, Event{ID: eventID, Type: "Signal", At: time.Now().UTC()}); err != nil {
			fmt.Fprintf(os.Stderr, "put inbox err: %v\n", err)
			os.Exit(1)
		}

	case "admission-acquire":
		// admission-acquire <dir> <resource> <maxPermits> <holdDurationMs>
		if len(args) < 5 {
			os.Exit(2)
		}
		dir := args[1]
		resource := args[2]
		maxPermits, _ := strconv.Atoi(args[3])
		durationMs, _ := strconv.Atoi(args[4])
		ctrl, err := NewFileAdmissionController(dir)
		if err != nil {
			os.Exit(1)
		}
		ctx := context.Background()
		lease, err := ctrl.Acquire(ctx, resource, AdmissionPolicy{MaxConcurrent: maxPermits}, 150*time.Millisecond)
		if err != nil {
			fmt.Fprintf(os.Stderr, "acquire err: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("ACQUIRED:%s\n", lease.Token)
		_ = os.Stdout.Sync()
		time.Sleep(time.Duration(durationMs) * time.Millisecond)
		_ = ctrl.Release(ctx, lease)

	default:
		os.Exit(2)
	}
}

func runSubprocess(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "AXIOM_TEST_SUBPROCESS=1")
	return cmd
}

// 1 & 5: Subprocess Competing Committers (10 concurrent child processes preserve monotonic versions)
func TestSubprocessCompetingCommitters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	execID := "exec-subprocess-competing"
	initial := newTestExecution(execID, 1)
	initial.Data = map[string]json.RawMessage{
		"count": json.RawMessage(`0`),
	}
	if err := store.Create(ctx, initial); err != nil {
		t.Fatal(err)
	}

	numProcs := 10
	incrementsPerProc := 5
	var wg sync.WaitGroup
	errorsChan := make(chan error, numProcs)

	for p := 0; p < numProcs; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := runSubprocess(t, "commit-increment", dir, execID, strconv.Itoa(incrementsPerProc))
			output, err := cmd.CombinedOutput()
			if err != nil {
				errorsChan <- fmt.Errorf("subprocess error: %v, output: %s", err, string(output))
			}
		}()
	}

	wg.Wait()
	close(errorsChan)

	for err := range errorsChan {
		t.Errorf("competing committer failure: %v", err)
	}

	finalExec, err := store.Load(ctx, execID)
	if err != nil {
		t.Fatalf("failed to load final execution: %v", err)
	}

	expectedCount := numProcs * incrementsPerProc
	var countVal int
	if raw, ok := finalExec.Data["count"]; ok {
		_ = json.Unmarshal(raw, &countVal)
	}
	if countVal != expectedCount {
		t.Fatalf("final count = %v, want %d", countVal, expectedCount)
	}

	// Final version must be initial (1) + total commits
	expectedVersion := uint64(1 + expectedCount)
	if finalExec.Version != expectedVersion {
		t.Fatalf("final version = %d, want %d", finalExec.Version, expectedVersion)
	}

	// Verify all commit version files exist on disk monotonically
	commitsDir := filepath.Join(dir, "executions", EncodeDurableName(execID), "commits")
	entries, err := os.ReadDir(commitsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != int(expectedVersion) {
		t.Fatalf("found %d commit files on disk, want %d", len(entries), expectedVersion)
	}
}

// 2 & 3: Process death while holding lock & stale recovery after process death
func TestSubprocessProcessDeathAndStaleRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	locksDir := filepath.Join(t.TempDir(), "locks")
	filename := "death_test.lock"

	cmd := runSubprocess(t, "hold-lock", locksDir, filename, "30000")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait for child to acquire lock
	buf := make([]byte, 6)
	if _, err := stdout.Read(buf); err != nil || string(buf) != "READY\n" {
		_ = cmd.Process.Kill()
		t.Fatalf("child failed to become ready: %v (buf=%q)", err, string(buf))
	}

	lockPath := filepath.Join(locksDir, filename)
	if _, err := os.Stat(lockPath); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("lock file not found after child ready: %v", err)
	}

	// Kill child process abruptly while holding lock
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	// Make lockfile appear stale by backdating ModTime
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Attempt acquisition by parent with 100ms stale threshold: must reclaim and succeed
	acquired := false
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = withOwnedFileLock(ctx, locksDir, filename, 100*time.Millisecond, func() error {
		acquired = true
		return nil
	})
	if err != nil {
		t.Fatalf("parent failed to acquire lock after child process death: %v", err)
	}
	if !acquired {
		t.Fatal("parent lock action was not executed")
	}
}

// 6: Subprocess inbox dedup under contention
func TestSubprocessInboxContentionAndDedup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	execID := "exec-inbox-contention"
	if err := store.Create(ctx, newTestExecution(execID, 1)); err != nil {
		t.Fatal(err)
	}

	numProcs := 8
	var wg sync.WaitGroup
	for p := 0; p < numProcs; p++ {
		wg.Add(1)
		eventID := fmt.Sprintf("event-item-%d", p%3) // 3 distinct event IDs across 8 concurrent writers
		go func(ev string) {
			defer wg.Done()
			cmd := runSubprocess(t, "put-inbox", dir, execID, ev)
			_ = cmd.Run()
		}(eventID)
	}
	wg.Wait()

	inbox, err := store.ListInbox(ctx, execID)
	if err != nil {
		t.Fatalf("ListInbox error: %v", err)
	}
	if len(inbox) != 3 {
		t.Fatalf("expected 3 deduplicated inbox items, got %d (%+v)", len(inbox), inbox)
	}

	// Acknowledge all
	ackIDs := []string{"event-item-0", "event-item-1", "event-item-2"}
	if err := store.AckInbox(ctx, execID, ackIDs); err != nil {
		t.Fatalf("AckInbox error: %v", err)
	}

	inboxAfter, err := store.ListInbox(ctx, execID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inboxAfter) != 0 {
		t.Fatalf("inbox not empty after AckInbox: %+v", inboxAfter)
	}
}

// 8: Subprocess File Admission Lock Recovery
func TestSubprocessAdmissionLockRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	dir := t.TempDir()
	cmd := runSubprocess(t, "admission-acquire", dir, "resource-db", "1", "30000")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Read ACQUIRED signal
	buf := make([]byte, 20)
	n, err := stdout.Read(buf)
	if err != nil || n == 0 {
		_ = cmd.Process.Kill()
		t.Fatalf("failed to read child admission acquire signal: %v", err)
	}

	// Kill child holding the only permit
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	// Backdate all lock and lease files in dir to make them stale
	locksDir := filepath.Join(dir, "locks")
	_ = filepath.Walk(locksDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			old := time.Now().Add(-10 * time.Minute)
			_ = os.Chtimes(path, old, old)
		}
		return nil
	})

	// Wait for the 150ms lease TTL to lapse so purgeAdmission reclaims the dead lease
	time.Sleep(200 * time.Millisecond)

	// Parent should now be able to acquire the permit with short timeout/stale reclamation
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	recoveredCtrl, err := NewFileAdmissionController(dir, WithFileAdmissionLockStaleAfter(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	lease, err := recoveredCtrl.Acquire(ctx, "resource-db", AdmissionPolicy{MaxConcurrent: 1}, time.Minute)
	if err != nil {
		t.Fatalf("parent failed to acquire admission permit after dead child: %v", err)
	}
	if lease.Token == "" {
		t.Fatalf("invalid lease acquired: %+v", lease)
	}
	_ = recoveredCtrl.Release(ctx, lease)
}

// 4: Owner A cleanup never removes Owner B lock across stale takeover
func TestSubprocessOwnerCleanupNeverRemovesOtherOwnerLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	locksDir := filepath.Join(t.TempDir(), "locks")
	filename := "takeover.lock"

	// Start child A holding lock for 300ms
	cmd := runSubprocess(t, "hold-lock", locksDir, filename, "300")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 6)
	if _, err := stdout.Read(buf); err != nil || string(buf) != "READY\n" {
		_ = cmd.Process.Kill()
		t.Fatalf("child A failed to acquire lock: %v", err)
	}

	lockPath := filepath.Join(locksDir, filename)

	// Kill child A to release open file handle (cross-platform compatible on Windows & POSIX)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	// Stale takeover occurs: lock is removed and Owner B acquires the lock
	_ = os.Remove(lockPath)
	writeLockForTest(t, lockPath, "owner-b-parent")

	// Child A's cleanup routine attempts to release lock with its own stale identity
	if err := releaseFileLock(lockPath, "owner-a-stale"); err != nil {
		t.Fatal(err)
	}

	// Verify lockPath STILL exists and still belongs to owner-b-parent
	record, err := readFileLockRecord(lockPath)
	if err != nil {
		t.Fatalf("owner-b lock was deleted by child A cleanup: %v", err)
	}
	if record.Owner != "owner-b-parent" {
		t.Fatalf("lock owner changed: got %q, want owner-b-parent", record.Owner)
	}
}

// 7: Cancellation leaves no lock leak under contention
func TestSubprocessCancellationLeavesNoLockLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	execID := "exec-cancel-leak-test"
	if err := store.Create(ctx, newTestExecution(execID, 1)); err != nil {
		t.Fatal(err)
	}

	// Lock the execution with parent
	locksDir := filepath.Join(dir, "locks")
	lockPath := filepath.Join(locksDir, EncodeDurableName(execID)+".lock")
	writeLockForTest(t, lockPath, "parent-owner")

	// Spawn children that will contend on lock and time out
	numProcs := 5
	var wg sync.WaitGroup
	for p := 0; p < numProcs; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := runSubprocess(t, "commit-increment", dir, execID, "1")
			_ = cmd.Run()
		}()
	}

	// Release parent lock after 50ms
	time.Sleep(50 * time.Millisecond)
	_ = releaseFileLock(lockPath, "parent-owner")

	wg.Wait()

	// Parent should now be able to commit without any orphan lock blocking it
	commitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	latest, err := store.Load(commitCtx, execID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	_, err = store.Commit(commitCtx, execID, latest.Version, func(e *Execution) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Commit after child cancellations failed (lock leaked): %v", err)
	}
}

