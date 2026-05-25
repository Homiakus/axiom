package tui

import (
	"context"
	"strings"
	"testing"

	"axiom/pkg/axiom"
)

func TestRuntimeSessionStartSignalPatchAndQuery(t *testing.T) {
	module, err := axiom.LoadModule([]byte(runtimeSource))
	if err != nil {
		t.Fatalf("LoadModule() error = %v", err)
	}
	session := NewRuntimeSession(module)
	ctx := context.Background()

	out, err := session.StartExecution(ctx, "run-1", `{"Counter":{"count":2}}`)
	if err != nil {
		t.Fatalf("StartExecution() error = %v", err)
	}
	if !strings.Contains(out, `"count": 2`) {
		t.Fatalf("start output = %s", out)
	}

	out, err = session.SendSignal(ctx, "Increment", `{"by":3}`)
	if err != nil {
		t.Fatalf("SendSignal() error = %v", err)
	}
	if !strings.Contains(out, "SignalReceived") {
		t.Fatalf("signal output = %s", out)
	}

	out, err = session.PatchContext(ctx, `{"Counter":{"count":10}}`)
	if err != nil {
		t.Fatalf("PatchContext() error = %v", err)
	}
	if !strings.Contains(out, `"count": 10`) {
		t.Fatalf("patch output = %s", out)
	}

	out, err = session.Query(ctx, "counterView")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !strings.Contains(out, `"count": 10`) {
		t.Fatalf("query output = %s", out)
	}
}

func TestRuntimeSessionInvalidJSON(t *testing.T) {
	module, err := axiom.LoadModule([]byte(runtimeSource))
	if err != nil {
		t.Fatalf("LoadModule() error = %v", err)
	}
	session := NewRuntimeSession(module)

	if _, err := session.StartExecution(context.Background(), "bad-json", `[]`); err == nil {
		t.Fatalf("StartExecution() expected JSON object error")
	}
}

func TestRuntimeSessionUnregisteredActivityReportsDiagnostic(t *testing.T) {
	module, err := axiom.LoadModule([]byte(pendingActivitySource))
	if err != nil {
		t.Fatalf("LoadModule() error = %v", err)
	}
	session := NewRuntimeSession(module)
	ctx := context.Background()

	if _, err := session.StartExecution(ctx, "pending-1", ""); err != nil {
		t.Fatalf("StartExecution() error = %v", err)
	}
	if _, err := session.SendSignal(ctx, "Go", ""); err != nil {
		t.Fatalf("SendSignal() error = %v", err)
	}
	out, err := session.RunUntilIdle(ctx)
	if err == nil {
		t.Fatalf("RunUntilIdle() expected unregistered activity error, output = %s", out)
	}
	if !strings.Contains(err.Error(), "AX501") || !strings.Contains(err.Error(), "DoWork") {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
}

const runtimeSource = `
domain RuntimeOK

signal Increment:
  by: Int

context Counter:
  count: Int = 0

rule add:
  on Increment
  write:
    Counter.count = Counter.count + signal.by

query counterView:
  return:
    count = Counter.count
`

const pendingActivitySource = `
domain PendingActivity

signal Go

context Job:
  done: Bool = false

policy externalPolicy:
  retry: 0
  timeout: 1s
  concurrency: once
  idempotency: required

activity DoWork:
  output:
    done: Bool
  effect: external
  idempotencyKey: "job"
  policy: externalPolicy

rule runWork:
  on Go
  run: DoWork
  write:
    Job.done = output.done
`
