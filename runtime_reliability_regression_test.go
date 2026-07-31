package axiom

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

const durableActivityFailureSource = `domain DurableFailure

signal Start:
  id: String

context State:
  completed: Bool = false

policy externalPolicy:
  idempotency: required

activity ExternalOperation:
  input:
    id = signal.id
  output:
    completed: Bool
  effect: external
  idempotencyKey: signal.id
  policy: externalPolicy

rule execute:
  on Start
  run: ExternalOperation
  write:
    State.completed = output.completed
`

func TestPebbleActivityFailureIsDurable(t *testing.T) {
	module, err := Compile([]byte(durableActivityFailureSource))
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	activityErr := errors.New("external failure")
	store, err := OpenPebble(directory, PebbleNoSync())
	if err != nil {
		t.Fatal(err)
	}

	engine, err := New(
		module,
		WithStore(store),
		Act("ExternalOperation", func(context.Context, Input) (Output, error) {
			return nil, activityErr
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	run := engine.Execution("failure-1")
	err = run.Signal(context.Background(), "Start", map[string]any{"id": "operation-1"})
	if !errors.Is(err, activityErr) {
		t.Fatalf("Signal() error = %v, want %v", err, activityErr)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenPebble(directory, PebbleNoSync())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	engine, err = New(
		module,
		WithStore(reopened),
		Act("ExternalOperation", func(context.Context, Input) (Output, error) {
			t.Fatal("failed activity must not be executed after reopen")
			return nil, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	run = engine.Execution("failure-1")
	status, err := run.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusFailed {
		t.Fatalf("status = %s, want %s", status, StatusFailed)
	}

	history, err := run.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range history {
		if entry.Type == "ActivityFailed" {
			return
		}
	}
	t.Fatal("ActivityFailed history entry is missing")
}

type failingRecoveryStore struct {
	Store
	failure error
	active  atomic.Int64
	calls   atomic.Int64
}

func (s *failingRecoveryStore) RecoverExpiredLeases(context.Context, string, time.Duration) (int, error) {
	s.active.Add(1)
	defer s.active.Add(-1)
	s.calls.Add(1)
	return 0, s.failure
}

func TestStartWorkerStopsSiblingGoroutinesOnFailure(t *testing.T) {
	module, err := Compile([]byte("domain WorkerFailure\n"))
	if err != nil {
		t.Fatal(err)
	}

	workerErr := errors.New("recovery failure")
	store := &failingRecoveryStore{Store: NewMemoryStore(), failure: workerErr}
	engine, err := New(module, WithStore(store))
	if err != nil {
		t.Fatal(err)
	}

	err = engine.StartWorker(context.Background(), WorkerOptions{
		ExecutionID:  "execution-1",
		Concurrency:  8,
		PollInterval: time.Millisecond,
		LeaseTTL:     time.Second,
	})
	if !errors.Is(err, workerErr) {
		t.Fatalf("StartWorker() error = %v, want %v", err, workerErr)
	}
	if active := store.active.Load(); active != 0 {
		t.Fatalf("active workers after return = %d, want 0", active)
	}

	calls := store.calls.Load()
	time.Sleep(20 * time.Millisecond)
	if after := store.calls.Load(); after != calls {
		t.Fatalf("worker calls continued after return: before=%d after=%d", calls, after)
	}
}
