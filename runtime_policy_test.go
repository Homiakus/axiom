package axiom

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

const retryActivitySource = `domain RetryActivity

signal Start:
  value: Int

context State:
  value: Int = 0

policy retryPolicy:
  retry: 2
  timeout: 1s
  idempotency: required

activity Unstable:
  input:
    value = signal.value
  output:
    value: Int
  effect: external
  idempotencyKey: "retry-test"
  policy: retryPolicy

rule execute:
  on Start
  run: Unstable
  write:
    State.value = output.value
`

func TestRunEnforcesActivityRetryPolicy(t *testing.T) {
	module, err := Compile([]byte(retryActivitySource))
	if err != nil {
		t.Fatal(err)
	}

	var attempts atomic.Int64
	engine, err := New(module, Act("Unstable", func(_ context.Context, input Input) (Output, error) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			return nil, errors.New("temporary failure")
		}
		return Output{"value": input["value"]}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	run := engine.Execution("retry-1")
	if err := run.Signal(context.Background(), "Start", map[string]any{"value": 7}); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("activity attempts = %d, want 3", got)
	}

	var state struct {
		Value int `json:"value"`
	}
	if err := run.State(context.Background(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Value != 7 {
		t.Fatalf("state.Value = %d, want 7", state.Value)
	}

	history, err := run.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	retries := 0
	for _, entry := range history {
		if entry.Type == "ActivityRetryScheduled" {
			retries++
		}
	}
	if retries != 2 {
		t.Fatalf("retry history entries = %d, want 2", retries)
	}
}

const timeoutActivitySource = `domain TimeoutActivity

signal Start:
  id: String

context State:
  done: Bool = false

policy timeoutPolicy:
  retry: 0
  timeout: 10ms
  idempotency: required

activity Slow:
  input:
    id = signal.id
  output:
    done: Bool
  effect: external
  idempotencyKey: signal.id
  policy: timeoutPolicy

rule execute:
  on Start
  run: Slow
  write:
    State.done = output.done
`

func TestRunEnforcesActivityTimeoutPolicy(t *testing.T) {
	module, err := Compile([]byte(timeoutActivitySource))
	if err != nil {
		t.Fatal(err)
	}

	engine, err := New(module, Act("Slow", func(ctx context.Context, _ Input) (Output, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}

	run := engine.Execution("timeout-1")
	started := time.Now()
	err = run.Signal(context.Background(), "Start", map[string]any{"id": "one"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Signal() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("activity timeout took too long: %s", elapsed)
	}
	status, statusErr := run.Status(context.Background())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status != StatusFailed {
		t.Fatalf("status = %s, want %s", status, StatusFailed)
	}
}

func TestReplayFromHistoryContextCancellation(t *testing.T) {
	module, err := Compile([]byte("domain ReplayCancellation\n"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ReplayFromHistoryContext(ctx, module, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReplayFromHistoryContext() error = %v, want context canceled", err)
	}
}
