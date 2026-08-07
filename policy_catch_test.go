package axiom

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func exactCatchSource(retry int) []byte {
	return []byte(`domain CatchRuntime

signal Start

signal PaymentDeclinedSignal:
  errorCode: String
  error: String
  attempt: Int

signal GenericFailureSignal:
  errorCode: String
  error: String

context State:
  caught: Bool = false
  target: String = ""

policy payment:
  retry: ` + strconv.Itoa(retry) + `
  backoff: 1ms
  timeout: 1s
  concurrency: parallel
  idempotency: optional
  catch:
    PaymentDeclined -> PaymentDeclinedSignal
    * -> GenericFailureSignal

activity Charge:
  output:
    ok: Bool
  effect: local
  policy: payment

rule charge:
  on Start
  run: Charge

rule exactCaught:
  on PaymentDeclinedSignal
  write:
    State.caught = true
    State.target = "exact"

rule wildcardCaught:
  on GenericFailureSignal
  write:
    State.caught = true
    State.target = "wildcard"
`)
}

func TestPolicyCatchRunsAfterRetryBudgetIsExhausted(t *testing.T) {
	module, err := Compile(exactCatchSource(1))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	var attempts atomic.Int32
	engine, err := New(module, WithStore(store), Act("Charge", func(context.Context, Input) (Output, error) {
		attempts.Add(1)
		return nil, FailActivity("PaymentDeclined", errors.New("card declined"))
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Execution("catch-retry").Signal(ctx, "Start", nil); err != nil {
		t.Fatalf("Run.Signal() error = %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("handler attempts = %d, want 2", attempts.Load())
	}

	stateResult, err := engine.Query(ctx, "catch-retry", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	state := stateResult["context"].(map[string]map[string]any)["State"]
	if state["caught"] != true || state["target"] != "exact" {
		t.Fatalf("caught state = %#v", state)
	}
	task := onlyRetryTask(t, store, "catch-retry")
	if task.Status != TaskFailed || task.Attempt != 2 || task.MaxAttempts != 2 {
		t.Fatalf("terminal caught task = %#v", task)
	}
	history, err := store.ListHistory(ctx, "catch-retry")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if countHistoryType(history, "ActivityRetryScheduled") != 1 {
		t.Fatalf("history missing retry checkpoint: %#v", history)
	}
	if countHistoryType(history, "ActivityRetryExhausted") != 1 {
		t.Fatalf("history missing retry exhaustion: %#v", history)
	}
	if countHistoryType(history, "ActivityCaught") != 1 {
		t.Fatalf("history missing ActivityCaught: %#v", history)
	}
	if countHistoryType(history, "ActivityFailed") != 1 {
		t.Fatalf("caught terminal failure should have one ActivityFailed audit record: %#v", history)
	}
	status, err := engine.Execution("catch-retry").Status(ctx)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status != StatusWaiting {
		t.Fatalf("execution status = %s, want Waiting", status)
	}
}

func TestPolicyCatchWildcardHandlesUncodedTerminalFailure(t *testing.T) {
	module, err := Compile(exactCatchSource(0))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module, Act("Charge", func(context.Context, Input) (Output, error) {
		return nil, errors.New("gateway disconnected")
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Execution("catch-wildcard").Signal(ctx, "Start", nil); err != nil {
		t.Fatalf("Run.Signal() error = %v", err)
	}
	stateResult, err := engine.Query(ctx, "catch-wildcard", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	state := stateResult["context"].(map[string]map[string]any)["State"]
	if state["target"] != "wildcard" {
		t.Fatalf("State.target = %#v, want wildcard", state["target"])
	}
}

func TestPolicyCatchExactMappingWinsOverWildcard(t *testing.T) {
	module, err := Compile(exactCatchSource(0))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module, Act("Charge", func(context.Context, Input) (Output, error) {
		return nil, FailActivity("PaymentDeclined", errors.New("declined"))
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Execution("catch-priority").Signal(ctx, "Start", nil); err != nil {
		t.Fatalf("Run.Signal() error = %v", err)
	}
	stateResult, err := engine.Query(ctx, "catch-priority", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	state := stateResult["context"].(map[string]map[string]any)["State"]
	if state["target"] != "exact" {
		t.Fatalf("State.target = %#v, want exact", state["target"])
	}
}

func TestPolicyCatchDoesNotMatchDifferentCodedFailureWithoutWildcard(t *testing.T) {
	source := []byte(`domain CatchNoMatch

signal Start
signal PaymentDeclinedSignal

context State:
  caught: Bool = false

policy payment:
  retry: 0
  timeout: 1s
  concurrency: parallel
  idempotency: optional
  catch:
    PaymentDeclined -> PaymentDeclinedSignal

activity Charge:
  output:
    ok: Bool
  effect: local
  policy: payment

rule charge:
  on Start
  run: Charge

rule caught:
  on PaymentDeclinedSignal
  write:
    State.caught = true
`)
	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	engine, err := New(module, WithStore(store), Act("Charge", func(context.Context, Input) (Output, error) {
		return nil, FailActivity("GatewayTimeout", errors.New("timeout"))
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	err = engine.Execution("catch-no-match").Signal(ctx, "Start", nil)
	if err == nil || !strings.Contains(err.Error(), "AX505") {
		t.Fatalf("Run.Signal() error = %v, want AX505", err)
	}
	history, historyErr := store.ListHistory(ctx, "catch-no-match")
	if historyErr != nil {
		t.Fatalf("ListHistory() error = %v", historyErr)
	}
	if countHistoryType(history, "ActivityCaught") != 0 {
		t.Fatalf("unexpected ActivityCaught: %#v", history)
	}
}

type customCodedFailure struct{}

func (customCodedFailure) Error() string             { return "custom decline" }
func (customCodedFailure) ActivityErrorCode() string { return "PaymentDeclined" }

func TestPolicyCatchAcceptsCustomActivityErrorCoder(t *testing.T) {
	module, err := Compile(exactCatchSource(0))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module, Act("Charge", func(context.Context, Input) (Output, error) {
		return nil, customCodedFailure{}
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Execution("catch-custom").Signal(ctx, "Start", nil); err != nil {
		t.Fatalf("Run.Signal() error = %v", err)
	}
	stateResult, err := engine.Query(ctx, "catch-custom", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	if got := stateResult["context"].(map[string]map[string]any)["State"]["target"]; got != "exact" {
		t.Fatalf("State.target = %#v, want exact", got)
	}
}

func TestPolicyCatchFailureRollsBackCatchTransaction(t *testing.T) {
	source := []byte(`domain CatchRollback

signal Start
signal FailureSignal

context State:
  caught: Bool = false

claim stayUncaught:
  always:
    not State.caught

policy failurePolicy:
  retry: 0
  timeout: 1s
  concurrency: parallel
  idempotency: optional
  catch:
    * -> FailureSignal

activity Work:
  output:
    ok: Bool
  effect: local
  policy: failurePolicy

rule work:
  on Start
  run: Work

rule invalidCatch:
  on FailureSignal
  write:
    State.caught = true
`)
	module, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store, err := OpenPebble(t.TempDir())
	if err != nil {
		t.Fatalf("OpenPebble() error = %v", err)
	}
	defer store.Close()
	engine, err := New(module, WithStore(store), WithProductionMode(), Act("Work", func(context.Context, Input) (Output, error) {
		return nil, errors.New("boom")
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	err = engine.Execution("catch-rollback").Signal(ctx, "Start", nil)
	if err == nil || !strings.Contains(err.Error(), "AX511") {
		t.Fatalf("Run.Signal() error = %v, want AX511", err)
	}

	history, historyErr := store.ListHistory(ctx, "catch-rollback")
	if historyErr != nil {
		t.Fatalf("ListHistory() error = %v", historyErr)
	}
	if countHistoryType(history, "ActivityCaught") != 0 {
		t.Fatalf("ActivityCaught was committed despite catch rollback: %#v", history)
	}
	for _, entry := range history {
		if entry.Type == "SignalReceived" && entry.Payload["source"] == "policy.catch" {
			t.Fatalf("catch SignalReceived was committed despite rollback: %#v", entry)
		}
	}
	stateResult, stateErr := engine.Query(ctx, "catch-rollback", "state")
	if stateErr != nil {
		t.Fatalf("Query(state) error = %v", stateErr)
	}
	state := stateResult["context"].(map[string]map[string]any)["State"]
	if state["caught"] != false {
		t.Fatalf("State.caught = %#v, want false after rollback", state["caught"])
	}
	tasks, taskErr := store.ListTasks(ctx, "catch-rollback")
	if taskErr != nil {
		t.Fatalf("ListTasks() error = %v", taskErr)
	}
	if len(tasks) != 1 || tasks[0].Status != TaskRunning {
		t.Fatalf("task after rollback = %#v, want original leased task still running", tasks)
	}
}
