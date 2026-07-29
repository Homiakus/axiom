package axiom_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Homiakus/axiom"
)

// ──────────────────────────────────────────────────────────────────────────────
// TestFlowComprehensive tests the Go-first Flow frontend (flow.go) for:
// - Claim verification
// - Pointer vs value event handlers
// - Panic on missing handlers or nil arguments
// - Multiple handler registrations
// ──────────────────────────────────────────────────────────────────────────────

type flowAccountState struct {
	Balance int `json:"balance"`
	Active  bool `json:"active"`
}

type depositEvent struct {
	Amount int `json:"amount"`
}

type withdrawEvent struct {
	Amount int `json:"amount"`
}

type logEffect struct {
	Msg string `json:"msg"`
}

func TestFlowDispatchHappyPath(t *testing.T) {
	flow := axiom.NewFlow("account", flowAccountState{Balance: 100, Active: true})

	axiom.Handle(flow, func(_ context.Context, state flowAccountState, event depositEvent) (axiom.FlowResult[flowAccountState], error) {
		state.Balance += event.Amount
		return axiom.Next(state), nil
	})

	axiom.Handle(flow, func(_ context.Context, state flowAccountState, event withdrawEvent) (axiom.FlowResult[flowAccountState], error) {
		if event.Amount > state.Balance {
			return axiom.Next(state), errors.New("insufficient funds")
		}
		state.Balance -= event.Amount
		return axiom.Next(state), nil
	})

	engine, err := axiom.OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}

	exec := engine.Execution("acc-1")

	// Initial state.
	st, err := exec.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Balance != 100 {
		t.Fatalf("initial balance = %d, want 100", st.Balance)
	}

	// Deposit.
	if err := exec.Dispatch(context.Background(), depositEvent{Amount: 50}); err != nil {
		t.Fatal(err)
	}
	st, _ = exec.State(context.Background())
	if st.Balance != 150 {
		t.Fatalf("balance after deposit = %d, want 150", st.Balance)
	}

	// Withdraw.
	if err := exec.Dispatch(context.Background(), withdrawEvent{Amount: 30}); err != nil {
		t.Fatal(err)
	}
	st, _ = exec.State(context.Background())
	if st.Balance != 120 {
		t.Fatalf("balance after withdraw = %d, want 120", st.Balance)
	}

	// Insufficient funds — error returned, state untouched.
	err = exec.Dispatch(context.Background(), withdrawEvent{Amount: 999})
	if err == nil {
		t.Fatal("expected insufficient funds error")
	}
	st, _ = exec.State(context.Background())
	if st.Balance != 120 {
		t.Fatalf("balance after failed withdraw = %d, want 120", st.Balance)
	}
}

func TestFlowClaimVerification(t *testing.T) {
	flow := axiom.NewFlow("claimed-account", flowAccountState{Balance: 50, Active: true})

	axiom.Handle(flow, func(_ context.Context, state flowAccountState, event withdrawEvent) (axiom.FlowResult[flowAccountState], error) {
		state.Balance -= event.Amount
		return axiom.Next(state), nil
	})

	// Add invariant claim: balance must remain non-negative.
	axiom.AddClaim(flow, func(state flowAccountState) error {
		if state.Balance < 0 {
			return errors.New("balance cannot be negative")
		}
		return nil
	})

	engine, err := axiom.OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}

	exec := engine.Execution("claimed-1")

	// Valid withdrawal.
	if err := exec.Dispatch(context.Background(), withdrawEvent{Amount: 30}); err != nil {
		t.Fatal(err)
	}

	// Over-withdrawal: claim fails.
	err = exec.Dispatch(context.Background(), withdrawEvent{Amount: 100})
	if err == nil {
		t.Fatal("expected claim error")
	}

	// State should not be committed after claim failure.
	st, _ := exec.State(context.Background())
	if st.Balance != 20 {
		t.Fatalf("balance = %d, want 20 (uncommitted after failed claim)", st.Balance)
	}
}

func TestFlowPointerEventMatching(t *testing.T) {
	flow := axiom.NewFlow("ptr-flow", flowAccountState{Balance: 0})

	// Register with value receiver.
	axiom.Handle(flow, func(_ context.Context, state flowAccountState, event depositEvent) (axiom.FlowResult[flowAccountState], error) {
		state.Balance += event.Amount
		return axiom.Next(state), nil
	})

	engine, err := axiom.OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}

	// Dispatch with pointer to event — should match value handler seamlessly.
	if err := engine.Execution("ptr-1").Dispatch(context.Background(), &depositEvent{Amount: 77}); err != nil {
		t.Fatal(err)
	}

	st, _ := engine.Execution("ptr-1").State(context.Background())
	if st.Balance != 77 {
		t.Fatalf("balance = %d, want 77", st.Balance)
	}
}

func TestFlowPanicsOnInvalidRegistration(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil handler")
		}
	}()
	axiom.Handle[flowAccountState, depositEvent](nil, nil)
}

func TestFlowUnregisteredEventError(t *testing.T) {
	flow := axiom.NewFlow("unreg", flowAccountState{})
	engine, err := axiom.OpenFlow(flow)
	if err != nil {
		t.Fatal(err)
	}

	err = engine.Execution("one").Dispatch(context.Background(), depositEvent{Amount: 10})
	if err == nil {
		t.Fatal("expected error for unregistered event")
	}
}
