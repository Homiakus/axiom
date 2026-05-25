package axiom

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPebbleStoreReopensScheduledActivity(t *testing.T) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	dir := t.TempDir()
	store, err := OpenPebble(dir)
	if err != nil {
		t.Fatalf("OpenPebble() error = %v", err)
	}
	engine, err := New(module, WithStore(store), WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
		return Output{"sent": true}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "pebble-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "pebble-1", "UserRegistered", Input{"userId": "u1", "email": "user@example.com"}); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := OpenPebble(dir)
	if err != nil {
		t.Fatalf("reopen OpenPebble() error = %v", err)
	}
	defer reopened.Close()
	recovered, err := New(module, WithStore(reopened), WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
		return Output{"sent": true}, nil
	}))
	if err != nil {
		t.Fatalf("New(recovered) error = %v", err)
	}
	pending, err := recovered.Query(ctx, "pebble-1", "pendingActivities")
	if err != nil {
		t.Fatalf("Query(pendingActivities) error = %v", err)
	}
	if got := len(pending["pendingActivities"].([]ActivityTask)); got != 1 {
		t.Fatalf("pending activities after reopen = %d", got)
	}
	if err := recovered.RunUntilIdle(ctx, "pebble-1"); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
	replayed, err := recovered.Replay(ctx, "pebble-1")
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if replayed.Context["User"]["welcomeSent"] != true {
		t.Fatalf("replayed welcomeSent = %#v", replayed.Context["User"]["welcomeSent"])
	}
}

func TestPebbleLeaseRecovery(t *testing.T) {
	store, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatalf("OpenPebble() error = %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	task := &ActivityTask{ID: "exec-1:rule:Activity:1", ExecutionID: "exec-1", RuleName: "rule", ActivityName: "Activity", Status: "pending", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := store.EnqueueTask(ctx, task); err != nil {
		t.Fatalf("EnqueueTask() error = %v", err)
	}
	leased, err := store.LeaseTask(ctx, "exec-1", "owner", time.Hour)
	if err != nil {
		t.Fatalf("LeaseTask() error = %v", err)
	}
	if leased.Status != "running" || leased.Attempt != 1 {
		t.Fatalf("leased task = %#v", leased)
	}
	if _, err := store.LeaseTask(ctx, "exec-1", "other", time.Hour); err == nil {
		t.Fatalf("second LeaseTask() expected no task error")
	}
	time.Sleep(2 * time.Millisecond)
	recovered, err := store.RecoverExpiredLeases(ctx, "exec-1", time.Nanosecond)
	if err != nil {
		t.Fatalf("RecoverExpiredLeases() error = %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d", recovered)
	}
}

func TestPebbleTransactionStoreMethodsDoNotDeadlock(t *testing.T) {
	store, err := OpenPebble(t.TempDir(), PebbleNoSync())
	if err != nil {
		t.Fatalf("OpenPebble() error = %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	task := &ActivityTask{ID: "exec-1:rule:Activity:1", ExecutionID: "exec-1", RuleName: "rule", ActivityName: "Activity", Status: "pending", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := store.EnqueueTask(ctx, task); err != nil {
		t.Fatalf("EnqueueTask() error = %v", err)
	}
	tx, err := store.BeginTransaction(ctx)
	if err != nil {
		t.Fatalf("BeginTransaction() error = %v", err)
	}
	defer tx.Rollback()
	done := make(chan error, 1)
	go func() {
		if err := tx.AppendHistory(ctx, "exec-1", "TestEntry", Input{"ok": true}); err != nil {
			done <- err
			return
		}
		if _, err := tx.ListHistory(ctx, "exec-1"); err != nil {
			done <- err
			return
		}
		leased, err := tx.PollTaskWithLease(ctx, "exec-1", "owner", time.Nanosecond)
		if err != nil {
			done <- err
			return
		}
		if leased == nil {
			done <- errNoLeasedTask{}
			return
		}
		if err := tx.HeartbeatTask(ctx, leased.ID, "owner"); err != nil {
			done <- err
			return
		}
		time.Sleep(2 * time.Millisecond)
		if _, err := tx.RecoverExpiredLeases(ctx, "exec-1", time.Nanosecond); err != nil {
			done <- err
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("transaction method error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("transaction store methods deadlocked")
	}
}

type errNoLeasedTask struct{}

func (errNoLeasedTask) Error() string { return "expected leased task" }

func TestAggregateHistoryDefault(t *testing.T) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	engine, err := New(module, WithStore(store), WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
		return Output{"sent": true}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "trace-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "trace-1", "UserRegistered", Input{"userId": "u1", "email": "user@example.com"}); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	history, err := store.ListHistory(ctx, "trace-1")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if !historyHasType(history, "RulesEvaluated") {
		t.Fatalf("history missing RulesEvaluated: %#v", history)
	}
	if historyHasType(history, "RuleSkipped") {
		t.Fatalf("aggregate history should not include per-rule RuleSkipped: %#v", history)
	}
}

func TestBundleGovernance(t *testing.T) {
	previous, err := CompileBundle([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("CompileBundle(previous) error = %v", err)
	}
	next, err := CompileBundle([]byte(stringsReplaceOnce(welcomeRuntimeSource, "welcomeSent: Bool = false", "welcomeSent: Bool = false\n  unsubscribed: Bool = false")))
	if err != nil {
		t.Fatalf("CompileBundle(next) error = %v", err)
	}
	if previous.SourceHash == next.SourceHash || previous.CompiledHash == next.CompiledHash {
		t.Fatalf("bundle hashes did not change")
	}
	diff := previous.Diff(next)
	if !containsString(diff.AddedFields, "User.unsubscribed") {
		t.Fatalf("diff = %#v", diff)
	}
	impact := previous.Impact([]string{"User.email"})
	if !containsString(impact.Rules, "sendWelcomeEmail") {
		t.Fatalf("impact = %#v", impact)
	}
	if err := next.ValidateCompatibility(previous); err != nil {
		t.Fatalf("ValidateCompatibility(additive) error = %v", err)
	}
}

func historyHasType(history []HistoryEntry, entryType string) bool {
	for _, entry := range history {
		if entry.Type == entryType {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringsReplaceOnce(source string, old string, next string) string {
	return strings.Replace(source, old, next, 1)
}
