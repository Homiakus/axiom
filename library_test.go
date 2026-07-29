package axiom

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNewHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "welcome.axm")
	if err := os.WriteFile(path, []byte(welcomeRuntimeSource), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	engine, err := app.New(Register("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
		return Output{"sent": true}, nil
	}))
	if err != nil {
		t.Fatalf("App.New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "load-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "load-1", "UserRegistered", Input{"userId": "u1", "email": "user@example.com"}); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if err := engine.RunUntilIdle(ctx, "load-1"); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
}

func TestWelcomeFlowSchedulesAndCompletesActivity(t *testing.T) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	store := NewMemoryStore()
	calls := 0
	engine, err := New(module, WithStore(store), WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
		calls++
		if input["userId"] != "u1" || input["email"] != "user@example.com" {
			t.Fatalf("activity input = %#v", input)
		}
		return Output{"sent": true}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "welcome-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "welcome-1", "UserRegistered", map[string]any{"userId": "u1", "email": "user@example.com"}); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	pending, err := engine.Query(ctx, "welcome-1", "pendingActivities")
	if err != nil {
		t.Fatalf("Query(pendingActivities) error = %v", err)
	}
	if got := len(pending["pendingActivities"].([]ActivityTask)); got != 1 {
		t.Fatalf("pending activities = %d", got)
	}
	if err := engine.RunUntilIdle(ctx, "welcome-1"); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("activity calls = %d", calls)
	}
	state, err := engine.Query(ctx, "welcome-1", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	user := state["context"].(map[string]map[string]any)["User"]
	if user["welcomeSent"] != true {
		t.Fatalf("welcomeSent = %#v", user["welcomeSent"])
	}
	if err := engine.RunUntilIdle(ctx, "welcome-1"); err != nil {
		t.Fatalf("second RunUntilIdle() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("completed activity re-executed, calls = %d", calls)
	}
	recovered, err := New(module, WithStore(store), WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
		calls++
		return Output{"sent": true}, nil
	}))
	if err != nil {
		t.Fatalf("New(recovered) error = %v", err)
	}
	if err := recovered.RunUntilIdle(ctx, "welcome-1"); err != nil {
		t.Fatalf("recovered RunUntilIdle() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("completed activity re-executed after recovery, calls = %d", calls)
	}
}

func TestReplayFromHistoryMatchesLiveState(t *testing.T) {
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
	if err := engine.Start(ctx, "replay-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "replay-1", "UserRegistered", Input{"userId": "u1", "email": "user@example.com"}); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if err := engine.RunUntilIdle(ctx, "replay-1"); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}
	live, err := engine.Query(ctx, "replay-1", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	history, err := store.ListHistory(ctx, "replay-1")
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	replayed, err := ReplayFromHistory(module, history)
	if err != nil {
		t.Fatalf("ReplayFromHistory() error = %v", err)
	}
	liveUser := live["context"].(map[string]map[string]any)["User"]
	if replayed.Context["User"]["id"] != liveUser["id"] ||
		replayed.Context["User"]["email"] != liveUser["email"] ||
		replayed.Context["User"]["welcomeSent"] != liveUser["welcomeSent"] {
		t.Fatalf("replayed context = %#v, live = %#v", replayed.Context["User"], liveUser)
	}
	if replayed.ModuleHash != module.CompiledHash {
		t.Fatalf("replayed ModuleHash = %q, want %q", replayed.ModuleHash, module.CompiledHash)
	}
	if len(replayed.RuntimeState.ActiveAtoms) == 0 {
		t.Fatalf("replayed RuntimeState.ActiveAtoms is empty")
	}
}

func TestReplayRejectsLegacyPatchWithoutValues(t *testing.T) {
	module, err := Compile([]byte(`
domain ReplayLegacy

context User:
  id: String?
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = ReplayFromHistory(module, []HistoryEntry{
		{Seq: 1, Type: "ExecutionStarted", Payload: map[string]any{"executionID": "legacy-1", "domain": "ReplayLegacy", "moduleHash": module.CompiledHash}},
		{Seq: 2, Type: "ContextPatched", Payload: map[string]any{"changed": []any{"User.id"}}},
	})
	if err == nil {
		t.Fatalf("ReplayFromHistory() expected legacy history error")
	}
	var diagnostic *Error
	if !errors.As(err, &diagnostic) {
		t.Fatalf("ReplayFromHistory() error type = %T, want *axiom.Error", err)
	}
	if diagnostic.Code != "AX902" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestReplayRejectsModuleHashMismatch(t *testing.T) {
	module, err := Compile([]byte(`
domain ReplayHash

context User:
  id: String?
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = ReplayFromHistory(module, []HistoryEntry{
		{Seq: 1, Type: "ExecutionStarted", Payload: map[string]any{"executionID": "hash-1", "domain": "ReplayHash", "moduleHash": "different"}},
	})
	if err == nil {
		t.Fatalf("ReplayFromHistory() expected hash mismatch")
	}
	var diagnostic *Error
	if !errors.As(err, &diagnostic) {
		t.Fatalf("ReplayFromHistory() error type = %T, want *axiom.Error", err)
	}
	if diagnostic.Code != "AX901" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestProductionModeRejectsNonTransactionalStore(t *testing.T) {
	module, err := Compile([]byte(`
domain Production

context User:
  id: String?
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = New(module, WithProductionMode())
	if err == nil {
		t.Fatalf("New() expected production transactional store error")
	}
	var diagnostics Errors
	if !errors.As(err, &diagnostics) {
		t.Fatalf("New() error type = %T, want axiom.Errors", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "AX506" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestMemoryStorePollTaskWithLeaseSkipsCompleted(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	completed := &ActivityTask{ID: "mem:rule:Activity:1", ExecutionID: "mem", RuleName: "rule", ActivityName: "Activity", Status: "completed"}
	pending := &ActivityTask{ID: "mem:rule:Activity:2", ExecutionID: "mem", RuleName: "rule", ActivityName: "Activity", Status: "pending"}
	if err := store.EnqueueTask(ctx, completed); err != nil {
		t.Fatalf("EnqueueTask(completed) error = %v", err)
	}
	if err := store.EnqueueTask(ctx, pending); err != nil {
		t.Fatalf("EnqueueTask(pending) error = %v", err)
	}
	task, err := store.PollTaskWithLease(ctx, "mem", "worker", 0)
	if err != nil {
		t.Fatalf("PollTaskWithLease() error = %v", err)
	}
	if task == nil || task.ID != pending.ID || task.Status != "running" || task.LockedBy != "worker" {
		t.Fatalf("leased task = %#v", task)
	}
}

func TestNewReportsUnregisteredActivity(t *testing.T) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = New(module)
	if err == nil {
		t.Fatalf("New() expected unregistered activity error")
	}
	var diagnostics Errors
	if !errors.As(err, &diagnostics) {
		t.Fatalf("New() error type = %T, want axiom.Errors", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "AX501" || diagnostics[0].Entity != "SendWelcomeEmail" || diagnostics[0].Kind != "config" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestClaimFailureBlocksWrite(t *testing.T) {
	module, err := Compile([]byte(`
domain Claims

signal Pay

context Payment:
  status: String = "idle"
  id: String?
  paidCount: Int = 0

rule badPayment:
  on Pay
  write:
    Payment.status = "paid"

claim paymentHasId:
  always:
    Payment.status == "paid" implies Payment.id exists
`))
	if err != nil {
		t.Fatalf("LoadModule() error = %v", err)
	}
	ctx := context.Background()
	engine, err := New(module)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := engine.Start(ctx, "claims-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "claims-1", "Pay", nil); err == nil {
		t.Fatalf("Signal() expected claim failure")
	}
	state, err := engine.Query(ctx, "claims-1", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	payment := state["context"].(map[string]map[string]any)["Payment"]
	if payment["status"] != "idle" {
		t.Fatalf("claim failure did not block write, status = %#v", payment["status"])
	}
}

func TestCheckoutFlowTriggersInventoryRiskAndPaymentIncrementally(t *testing.T) {
	module, err := Compile([]byte(checkoutRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	ctx := context.Background()
	calls := map[string]int{}
	engine, err := New(module, WithActivities(ActivityRegistry{
		"CheckInventory": func(ctx context.Context, input Input) (Output, error) {
			calls["inventory"]++
			return Output{"status": "available", "unavailable": []any{}}, nil
		},
		"CalculateRisk": func(ctx context.Context, input Input) (Output, error) {
			calls["risk"]++
			return Output{"status": "ok", "score": 0.1}, nil
		},
		"ChargeCard": func(ctx context.Context, input Input) (Output, error) {
			calls["payment"]++
			return Output{"paymentId": "pay-1", "status": "paid"}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	initial := map[string]any{
		"User": map[string]any{"id": "u1", "country": "US"},
		"Cart": map[string]any{"items": []any{"sku-1"}, "total": 25.0},
		"Payment": map[string]any{
			"method":   map[string]any{"kind": "card", "token": "tok_1"},
			"intentId": "intent-1",
		},
	}
	if err := engine.Start(ctx, "checkout-1", initial); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "checkout-1", "CheckoutRequested", nil); err != nil {
		t.Fatalf("CheckoutRequested error = %v", err)
	}
	if err := engine.RunUntilIdle(ctx, "checkout-1"); err != nil {
		t.Fatalf("RunUntilIdle checks error = %v", err)
	}
	if calls["inventory"] != 1 || calls["risk"] != 1 {
		t.Fatalf("check calls = %#v", calls)
	}
	if err := engine.Signal(ctx, "checkout-1", "CheckoutConfirmed", nil); err != nil {
		t.Fatalf("CheckoutConfirmed error = %v", err)
	}
	if err := engine.RunUntilIdle(ctx, "checkout-1"); err != nil {
		t.Fatalf("RunUntilIdle payment error = %v", err)
	}
	state, err := engine.Query(ctx, "checkout-1", "state")
	if err != nil {
		t.Fatalf("Query(state) error = %v", err)
	}
	payment := state["context"].(map[string]map[string]any)["Payment"]
	if payment["status"] != "paid" || payment["id"] != "pay-1" || payment["paidCount"] != 1 {
		t.Fatalf("payment context = %#v", payment)
	}
	if calls["payment"] != 1 {
		t.Fatalf("payment calls = %d", calls["payment"])
	}
}

func TestActivityOutputValidation(t *testing.T) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module, WithActivity("SendWelcomeEmail", func(ctx context.Context, input Input) (Output, error) {
		return Output{"sent": "yes"}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "bad-output-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := engine.Signal(ctx, "bad-output-1", "UserRegistered", Input{"userId": "u1", "email": "user@example.com"}); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	err = engine.RunUntilIdle(ctx, "bad-output-1")
	if err == nil {
		t.Fatalf("RunUntilIdle() expected output validation error")
	}
	var diagnostic *Error
	if !errors.As(err, &diagnostic) {
		t.Fatalf("RunUntilIdle() error type = %T, want *axiom.Error", err)
	}
	if diagnostic.Code != "AX504" || diagnostic.Entity != "SendWelcomeEmail.sent" || diagnostic.Kind != "activity" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestPatchUnknownFieldDiagnostic(t *testing.T) {
	module, err := Compile([]byte(`
domain Patch

context User:
  id: String?
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	engine, err := New(module)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx, "patch-1", nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	err = engine.Patch(ctx, "patch-1", Patch{"User.missing": "u1"})
	if err == nil {
		t.Fatalf("Patch() expected unknown field error")
	}
	var diagnostic *Error
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Patch() error type = %T, want *axiom.Error", err)
	}
	if diagnostic.Code != "AX405" || diagnostic.Entity != "User.missing" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestCompileDiagnosticIncludesEntityAndLine(t *testing.T) {
	_, err := Compile([]byte(`
domain Invalid

context User:
  id: String?

rule bad:
  on MissingSignal
  write:
    User.id = "u1"
`), WithSourceName("invalid.axm"))
	if err == nil {
		t.Fatalf("Compile() expected error")
	}
	var diagnostics Errors
	if !errors.As(err, &diagnostics) {
		t.Fatalf("Compile() error type = %T, want axiom.Errors", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	diag := diagnostics[0]
	if diag.Code != "AX001" || diag.Entity != "bad" || diag.Line != 7 || diag.File != "invalid.axm" {
		t.Fatalf("diagnostic = %#v", diag)
	}
	if !strings.Contains(err.Error(), "invalid.axm:7") {
		t.Fatalf("error string = %q", err.Error())
	}
}

func TestWithStrictFastRuntimeRejectsSlowGuard(t *testing.T) {
	module, err := Compile([]byte(`
domain Strict

context User:
  id: String?
  email: String?

claim same:
  always:
    User.id == User.email
`))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = New(module, WithStrictFastRuntime())
	if err == nil {
		t.Fatalf("New() expected strict fast runtime error")
	}
	var diagnostics Errors
	if !errors.As(err, &diagnostics) {
		t.Fatalf("New() error type = %T, want axiom.Errors", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "AX701" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

const welcomeRuntimeSource = `
domain Welcome

signal UserRegistered:
  userId: String
  email: String

context User:
  id: String?
  email: String?
  welcomeSent: Bool = false

computed userReady: Bool =
  User.id exists and User.email exists

fact RegisteredUser when:
  userReady
expose:
  id = User.id
  email = User.email

policy emailPolicy:
  retry: 2
  timeout: 5s
  concurrency: once
  idempotency: required

activity SendWelcomeEmail:
  require:
    RegisteredUser
  input:
    userId = RegisteredUser.id
    email = RegisteredUser.email
  output:
    sent: Bool
  effect: external
  idempotencyKey: RegisteredUser.id
  policy: emailPolicy

rule captureRegistration:
  on UserRegistered
  write:
    User.id = signal.userId
    User.email = signal.email

rule sendWelcomeEmail:
  on changed(User.email)
  when:
    User.welcomeSent == false
  require:
    RegisteredUser
  run: SendWelcomeEmail
  write:
    User.welcomeSent = output.sent

claim welcomeSentRequiresEmail:
  always:
    User.welcomeSent == true implies User.email exists
`

const checkoutRuntimeSource = `
domain Checkout

signal CheckoutRequested
signal CheckoutConfirmed

context User:
  id: String?
  country: String?

context Cart:
  items: List<String> = []
  total: Float = 0

context Inventory:
  status: String = "unknown"
  unavailable: List<String> = []

context Risk:
  status: String = "unknown"
  score: Float?

context Payment:
  method: Object?
  status: String = "idle"
  id: String?
  paidCount: Int = 0
  intentId: String?

computed cartPayable: Bool =
  Cart.items.length > 0 and Cart.total > 0

fact AuthenticatedUser when:
  User.id exists

fact PayableCart when:
  cartPayable
expose:
  items = Cart.items
  total = Cart.total

fact InventoryAvailable when:
  Inventory.status == "available"

fact RiskApproved when:
  Risk.status == "ok"

fact CardPayment when:
  Payment.method.kind == "card"
expose:
  token = Payment.method.token

fact CanCheckout when:
  AuthenticatedUser
  PayableCart
  InventoryAvailable
  RiskApproved

fact CanPayByCard when:
  CanCheckout
  CardPayment

policy externalCall:
  retry: 2
  timeout: 3s
  concurrency: latest
  idempotency: required

policy paymentCritical:
  retry: 0
  timeout: 10s
  concurrency: once
  idempotency: required
  audit: required

activity CheckInventory:
  require:
    AuthenticatedUser
    PayableCart
  input:
    items = PayableCart.items
    country = User.country
  output:
    status: String
    unavailable: List<String>
  effect: external
  idempotencyKey: hash(User.id, Cart.items, User.country)
  policy: externalCall

activity CalculateRisk:
  require:
    AuthenticatedUser
    PayableCart
  input:
    userId = User.id
    amount = Cart.total
    country = User.country
  output:
    status: String
    score: Float
  effect: external
  idempotencyKey: hash(User.id, Cart.total, User.country)
  policy: externalCall

activity ChargeCard:
  require:
    CanPayByCard
  input:
    userId = User.id
    amount = Cart.total
    token = CardPayment.token
  output:
    paymentId: String
    status: String
  effect: external
  idempotencyKey: Payment.intentId
  policy: paymentCritical

rule prepareInventory:
  on:
    CheckoutRequested
    changed(Cart.items)
    changed(User.country)
  when:
    Inventory.status in ["unknown", "expired"]
  run: CheckInventory
  write:
    Inventory.status = output.status
    Inventory.unavailable = output.unavailable

rule prepareRisk:
  on:
    CheckoutRequested
    changed(Cart.total)
    changed(User.id)
  when:
    Risk.status in ["unknown", "expired"]
  run: CalculateRisk
  write:
    Risk.status = output.status
    Risk.score = output.score

rule payByCard:
  on CheckoutConfirmed
  when:
    Payment.status != "paid"
  require:
    CanPayByCard
  run: ChargeCard
  write:
    Payment.id = output.paymentId
    Payment.status = output.status
    Payment.paidCount = Payment.paidCount + 1

claim paymentHasId:
  always:
    Payment.status == "paid" implies Payment.id exists

claim noDoublePayment:
  always:
    Payment.paidCount <= 1
`

type typedEmailInput struct {
	UserId string `json:"userId"`
	Email  string `json:"email"`
}

type typedEmailOutput struct {
	Sent bool `json:"sent"`
}

func TestActTypedAndRunPatch(t *testing.T) {
	module, err := Compile([]byte(welcomeRuntimeSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	calls := 0
	engine, err := New(module, ActTyped("SendWelcomeEmail", func(ctx context.Context, in typedEmailInput) (typedEmailOutput, error) {
		calls++
		if in.UserId != "u1" || in.Email != "user@example.com" {
			t.Fatalf("unexpected input: %#v", in)
		}
		return typedEmailOutput{Sent: true}, nil
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	run := engine.Execution("typed-1")
	if err := run.Patch(ctx, Patch{"User.id": "u1", "User.email": "user@example.com"}); err != nil {
		t.Fatalf("run.Patch() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("ActTyped calls = %d, want 1", calls)
	}
}

