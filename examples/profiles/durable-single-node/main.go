// Command durable-single-node demonstrates crash-durable execution with
// synchronous Pebble WAL persistence using Axiom's Durable Single Node profile.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/profile"
)

type BankAccountState struct {
	AccountID string  `json:"account_id"`
	Balance   float64 `json:"balance"`
}

type DepositEvent struct {
	Amount float64 `json:"amount"`
}

type AuditCmd struct {
	AccountID string  `json:"account_id"`
	NewAmount float64 `json:"new_amount"`
}

func main() {
	dir := "./data/durable-node"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	// 1. Initialize Durable Single Node profile
	p, err := profile.OpenDurableSingleNode(profile.DurableSingleNodeConfig{
		Dir:        dir,
		SyncWrites: true, // synchronous fsync WAL
	})
	if err != nil {
		log.Fatalf("open profile: %v", err)
	}
	defer p.Close()

	// 2. Define Flow with effects
	bankFlow := axiom.NewFlow("bank", BankAccountState{AccountID: "ACC-101", Balance: 0.0})
	axiom.Handle(bankFlow, func(ctx context.Context, s BankAccountState, e DepositEvent) (axiom.FlowResult[BankAccountState], error) {
		s.Balance += e.Amount
		return axiom.Next(s, axiom.Call(AuditCmd{AccountID: s.AccountID, NewAmount: s.Balance})), nil
	})
	axiom.EffectHandler(bankFlow, func(ctx context.Context, c AuditCmd) error {
		fmt.Printf("[AUDIT] Account %s updated balance: $%.2f\n", c.AccountID, c.NewAmount)
		return nil
	})

	// 3. Open FlowEngine with durable transactional outbox
	flowEngine, err := profile.OpenDurableFlow(p, bankFlow)
	if err != nil {
		log.Fatalf("open durable flow: %v", err)
	}

	ctx := context.Background()
	exec := flowEngine.Execution("acc-101")

	if err := exec.Dispatch(ctx, DepositEvent{Amount: 150.00}); err != nil {
		log.Fatalf("dispatch: %v", err)
	}

	st, err := exec.State(ctx)
	if err != nil {
		log.Fatalf("get state: %v", err)
	}

	fmt.Printf("Committed state: Account %s, Balance: $%.2f\n", st.AccountID, st.Balance)
}
