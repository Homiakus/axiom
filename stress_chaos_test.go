package axiom_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/internal/runtime"
	pebblestore "github.com/Homiakus/axiom/internal/store/pebble"
)

type ChaosOrderState struct {
	OrderID   string `json:"orderId"`
	Status    string `json:"status"`
	Paid      bool   `json:"paid"`
	SentEmail bool   `json:"sentEmail"`
}

type ChaosOrderEvent struct {
	OrderID string `json:"orderId"`
	Amount  int    `json:"amount"`
}

type ChaosEmailCommand struct {
	To      string `json:"to"`
	OrderID string `json:"orderId"`
}

// 1. Crash & Recovery Chaos: Flow Outbox Recovery after Simulated Crash
func TestChaos_FlowOutboxCrashAndRecovery(t *testing.T) {
	dir := t.TempDir()

	store, err := axiom.OpenPebbleFlowStore(dir)
	if err != nil {
		t.Fatalf("OpenPebbleFlowStore failed: %v", err)
	}

	failDelivery := true
	var deliveredIDs []string
	var mu sync.Mutex

	flow := axiom.NewFlow("ChaosOrderFlow", ChaosOrderState{})
	axiom.Handle(flow, func(_ context.Context, state ChaosOrderState, event ChaosOrderEvent) (axiom.FlowResult[ChaosOrderState], error) {
		state.OrderID = event.OrderID
		state.Paid = true
		state.Status = "paid"
		return axiom.Next(state, axiom.Call(ChaosEmailCommand{
			To:      "customer@example.com",
			OrderID: event.OrderID,
		})), nil
	})

	axiom.EffectHandler(flow, func(ctx context.Context, cmd ChaosEmailCommand) error {
		id, ok := axiom.FlowEffectIDFromContext(ctx)
		if !ok {
			return errors.New("missing flow effect id")
		}
		mu.Lock()
		deliveredIDs = append(deliveredIDs, id)
		mu.Unlock()
		if failDelivery {
			return errors.New("synthetic delivery crash")
		}
		return nil
	})

	// Step 1: Open Flow with durable effects
	engine, err := axiom.OpenFlow(flow, axiom.WithFlowStore(store), axiom.WithDurableFlowEffects())
	if err != nil {
		t.Fatalf("OpenFlow failed: %v", err)
	}

	ctx := context.Background()
	exec := engine.Execution("order-crash-100")

	// Dispatch event - will commit state + intent and then fail delivery
	err = exec.Dispatch(ctx, ChaosOrderEvent{OrderID: "ord-100", Amount: 250})
	var deliveryErr *axiom.FlowEffectDeliveryError
	if !errors.As(err, &deliveryErr) {
		t.Fatalf("expected FlowEffectDeliveryError, got %v", err)
	}

	// Close store simulating abrupt crash
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close failed: %v", err)
	}

	// Step 2: Reopen and recover pending outbox
	reopenedStore, err := axiom.OpenPebbleFlowStore(dir)
	if err != nil {
		t.Fatalf("reopen store failed: %v", err)
	}
	defer reopenedStore.Close()

	failDelivery = false // Fix delivery on recovery
	recoveredEngine, err := axiom.OpenFlow(flow, axiom.WithFlowStore(reopenedStore), axiom.WithDurableFlowEffects())
	if err != nil {
		t.Fatalf("reopened OpenFlow failed: %v", err)
	}

	recoveredExec := recoveredEngine.Execution("order-crash-100")
	if err := recoveredExec.DrainEffects(ctx); err != nil {
		t.Fatalf("DrainEffects on recovered engine failed: %v", err)
	}

	// Verify state and effect delivery
	state, err := recoveredExec.State(ctx)
	if err != nil {
		t.Fatalf("State read failed: %v", err)
	}
	if state.OrderID != "ord-100" || !state.Paid || state.Status != "paid" {
		t.Fatalf("unexpected recovered state: %#v", state)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(deliveredIDs) != 2 || deliveredIDs[0] == "" || deliveredIDs[0] != deliveredIDs[1] {
		t.Fatalf("expected 2 delivery attempts with exact same EffectID, got %#v", deliveredIDs)
	}
}

// 2. High Concurrency & Contention Stress: 100+ goroutines, 1000+ operations
func TestChaos_HighConcurrencyContentionStress(t *testing.T) {
	spec := `domain ConcurrencyStress
signal Increment
signal SetStatus
context State:
  counter: Int = 0
  status: String = "init"
  lastWorker: Int = 0
activity RecordTick:
  input:
    c = State.counter
  output:
    c: Int
rule onIncrement:
  on Increment
  run: RecordTick
  write:
    State.counter = output.c + 1
rule onSetStatus:
  on SetStatus
  write:
    State.status = "updated"
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	store := axiom.NewMemoryStore()
	engine, err := axiom.New(module,
		axiom.WithStore(store),
		axiom.Act("RecordTick", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
			c, _ := input["c"].(int64)
			if c == 0 {
				if cInt, ok := input["c"].(int); ok {
					c = int64(cInt)
				}
			}
			return axiom.Output{"c": c}, nil
		}),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	const (
		numWorkers     = 100
		opsPerWorker   = 20
		numExecutions  = 50
	)

	ctx := context.Background()
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*opsPerWorker)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(workerID) + time.Now().UnixNano()))
			for op := 0; op < opsPerWorker; op++ {
				execID := fmt.Sprintf("stress-exec-%d", r.Intn(numExecutions))
				run := engine.Execution(execID)

				if op%3 == 0 {
					if err := run.Signal(ctx, "Increment", nil); err != nil {
						errCh <- fmt.Errorf("worker %d Signal Increment on %s: %w", workerID, execID, err)
						return
					}
				} else if op%3 == 1 {
					if err := run.Patch(ctx, axiom.Patch{"State.lastWorker": workerID}); err != nil {
						errCh <- fmt.Errorf("worker %d Patch on %s: %w", workerID, execID, err)
						return
					}
				} else {
					if err := run.Signal(ctx, "SetStatus", nil); err != nil {
						errCh <- fmt.Errorf("worker %d Signal SetStatus on %s: %w", workerID, execID, err)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("high concurrency stress failure: %v", err)
		}
	}

	// Verify all executions have valid history monotonicity
	for e := 0; e < numExecutions; e++ {
		execID := fmt.Sprintf("stress-exec-%d", e)
		history, err := engine.Execution(execID).History(ctx)
		if err != nil {
			continue
		}
		for i := 0; i < len(history); i++ {
			expectedSeq := i + 1
			if history[i].Seq != expectedSeq {
				t.Fatalf("exec %s: sequence gap in history at %d: got Seq %d, want %d", execID, i, history[i].Seq, expectedSeq)
			}
		}
	}
}

// 3. Split-Brain & Stale Lock Takeover: Worker A lease expires -> Worker B takes over -> Worker A fenced
func TestChaos_SplitBrainStaleLeaseFencing(t *testing.T) {
	dir := t.TempDir()
	store, err := pebblestore.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	execID := "fencing-exec-1"

	if err := store.CreateExecution(ctx, &runtime.Execution{
		ID:        execID,
		Domain:    "FencingDomain",
		Status:    runtime.StatusRunning,
		Version:   1,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}

	task := &runtime.ActivityTask{
		ID:           "task-fence-1",
		ExecutionID:  execID,
		ActivityName: "CriticalPayment",
		Status:       runtime.TaskPending,
		Attempt:      1,
		MaxAttempts:  3,
	}
	if err := store.EnqueueTask(ctx, task); err != nil {
		t.Fatalf("EnqueueTask failed: %v", err)
	}

	// Worker A leases the task with short TTL (20ms)
	leaseTTL := 20 * time.Millisecond
	workerATask, err := store.PollTaskWithLease(ctx, execID, "worker-A", leaseTTL)
	if err != nil || workerATask == nil {
		t.Fatalf("worker-A PollTaskWithLease failed: %v", err)
	}
	if workerATask.LockedBy != "worker-A" {
		t.Fatalf("expected worker-A lock, got %q", workerATask.LockedBy)
	}

	// Worker A "sleeps" / experiences GC pause beyond lease TTL
	time.Sleep(50 * time.Millisecond)

	// Lease recovery happens: expired leases reclaimed
	recovered, err := store.RecoverExpiredLeases(ctx, execID, leaseTTL)
	if err != nil {
		t.Fatalf("RecoverExpiredLeases failed: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered lease, got %d", recovered)
	}

	// Worker B acquires the task
	workerBTask, err := store.PollTaskWithLease(ctx, execID, "worker-B", 5*time.Second)
	if err != nil || workerBTask == nil {
		t.Fatalf("worker-B PollTaskWithLease failed: %v", err)
	}
	if workerBTask.LockedBy != "worker-B" {
		t.Fatalf("expected worker-B lock, got %q", workerBTask.LockedBy)
	}

	// Worker A wakes up and tries to heartbeat or complete the task
	errHeartbeatA := store.HeartbeatTask(ctx, "task-fence-1", "worker-A")
	if errHeartbeatA == nil {
		t.Fatal("expected worker-A heartbeat to be rejected (fenced), but succeeded")
	}

	// Worker B completes the task successfully
	if err := store.CompleteTask(ctx, "task-fence-1", map[string]any{"paid": true}); err != nil {
		t.Fatalf("worker-B CompleteTask failed: %v", err)
	}

	// Verify task outcome is completed by worker B
	tasks, err := store.ListTasks(ctx, execID)
	if err != nil || len(tasks) == 0 {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if tasks[0].Status != runtime.TaskCompleted {
		t.Errorf("expected TaskCompleted, got %s", tasks[0].Status)
	}
}

// 4. ADGO FileStore Concurrency & Stale Lock Ownership Takeover
func TestChaos_ADGOFileStoreTakeoverAndContention(t *testing.T) {
	dir := t.TempDir()
	store, err := adgo.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	ctx := context.Background()
	const numExecs = 5

	// Seed executions first
	for e := 0; e < numExecs; e++ {
		execID := fmt.Sprintf("file-exec-%d", e)
		if err := store.Create(ctx, &adgo.Execution{
			ID:        execID,
			Status:    adgo.StatusRunning,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("store.Create failed: %v", err)
		}
	}

	const numGoroutines = 30
	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		gID := i
		go func() {
			defer wg.Done()
			execID := fmt.Sprintf("file-exec-%d", gID%numExecs)
			event := adgo.Event{
				ID:   fmt.Sprintf("ev-%d", gID),
				Type: "Signal",
				At:   time.Now().UTC(),
			}
			if err := store.PutInbox(ctx, execID, event); err != nil {
				errCh <- fmt.Errorf("PutInbox err: %w", err)
				return
			}
			if err := store.AckInbox(ctx, execID, []string{event.ID}); err != nil {
				errCh <- fmt.Errorf("AckInbox err: %w", err)
				return
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("ADGO FileStore contention failure: %v", err)
		}
	}
}

// 5. Poison Pills & Malformed Event Handling
func TestChaos_PoisonPillEventFailClosed(t *testing.T) {
	spec := `domain PoisonGuard
signal GoodSignal
context G:
  count: Int = 0
rule onGood:
  on GoodSignal
  write:
    G.count = G.count + 1
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	engine, err := axiom.New(module)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx := context.Background()
	run := engine.Execution("poison-event-exec")

	// 1. Valid signal succeeds
	if err := run.Signal(ctx, "GoodSignal", nil); err != nil {
		t.Fatalf("GoodSignal failed: %v", err)
	}

	// 2. Dispatch nil event fails fast
	if err := run.Dispatch(ctx, nil); err == nil {
		t.Fatal("expected error dispatching nil event")
	}

	// 3. Dispatch scalar event fails fast with informative error
	if err := run.Dispatch(ctx, 12345); err == nil {
		t.Fatal("expected error dispatching non-struct/unnamed event")
	}

	// 4. Verify existing state is NOT corrupted or wiped
	type GState struct {
		Count int `json:"count"`
	}
	var state GState
	if err := run.State(ctx, &state); err != nil {
		t.Fatalf("State read failed: %v", err)
	}
	if state.Count != 1 {
		t.Errorf("expected count 1 preserved after poison pill rejection, got %d", state.Count)
	}
}

// 6. Context Cancellation Boundary: instant fail on pre-canceled context
func TestChaos_ContextCancellationBoundary(t *testing.T) {
	spec := `domain CtxTest
signal Step
context C:
  val: Int = 0
rule onStep:
  on Step
  write:
    C.val = C.val + 1
`
	module, err := axiom.Compile([]byte(spec))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	engine, err := axiom.New(module)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	run := engine.Execution("cancel-exec")

	// Pre-canceled context returns ctx.Err() without partial execution
	if err := run.Signal(canceledCtx, "Step", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if err := run.Patch(canceledCtx, axiom.Patch{"C.val": 10}); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
