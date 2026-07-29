package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
)

type TestMachine struct {
	CreditKopecks int    `json:"creditKopecks"`
	WaterML       int    `json:"waterML"`
	LastDrink     string `json:"lastDrink"`
	LastDispensed bool   `json:"lastDispensed"`
}

type TestMoneyInserted struct {
	AmountKopecks int `json:"amountKopecks"`
}

type TestCoffeeRequested struct {
	PurchaseID string `json:"purchaseId"`
}

type DispenseReq struct {
	PurchaseID   string `json:"purchaseId"`
	PriceKopecks int    `json:"priceKopecks"`
}

type DispenseResp struct {
	Dispensed    bool `json:"dispensed"`
	PriceKopecks int  `json:"priceKopecks"`
}

func TestTypedModelBuilder(t *testing.T) {
	definition := model.New("TestCoffeeMachine").Version("1")

	// 1. Bind State & Events with strong Go types
	machine := model.Bind[TestMachine](definition, "Machine").
		Default("CreditKopecks", 0).
		Default("WaterML", 1000).
		Default("LastDrink", "").
		Default("LastDispensed", false)

	moneyInserted := model.EventOf[TestMoneyInserted](definition)
	coffeeRequested := model.EventOf[TestCoffeeRequested](definition)

	// 2. Policy & Activity
	definition.Policy("hardwarePolicy").
		Retry(1).
		Timeout(5 * time.Second).
		Idempotency("required")

	definition.Activity("DispenseCoffee").
		Input("purchaseId", coffeeRequested.String("PurchaseID")).
		Input("priceKopecks", 100).
		Output("dispensed", "Bool").
		Output("priceKopecks", "Int").
		Effect("external").
		IdempotencyKey(coffeeRequested.String("PurchaseID")).
		Policy("hardwarePolicy")

	// 3. Rules using Fluent Expression Methods and direct Values
	definition.Rule("acceptMoney").
		On(moneyInserted.Trigger()).
		When(moneyInserted.Int("AmountKopecks").GT(0)).
		Set(machine.Int("CreditKopecks"), machine.Int("CreditKopecks").Add(moneyInserted.Int("AmountKopecks")))

	definition.Rule("sellCoffee").
		On(coffeeRequested.Trigger()).
		When(
			machine.Int("CreditKopecks").GTE(100),
			machine.Int("WaterML").GTE(50),
		).
		Run("DispenseCoffee").
		Set(machine.Int("CreditKopecks"), machine.Int("CreditKopecks").Sub(model.OutputInt("priceKopecks"))).
		Set(machine.Int("WaterML"), machine.Int("WaterML").Sub(50)).
		Set(machine.String("LastDrink"), "espresso").
		Set(machine.Bool("LastDispensed"), model.OutputBool("dispensed"))

	// 4. Claims with fluent expressions
	definition.Claim(
		"nonNegative",
		machine.Int("CreditKopecks").GTE(0),
		machine.Int("WaterML").GTE(0),
	)

	// 5. Compile Plan
	plan, err := definition.Compile()
	if err != nil {
		t.Fatalf("failed to compile plan: %v", err)
	}

	// 6. Instantiate Engine with ActTyped
	engine, err := plan.New(
		axiom.ActTyped("DispenseCoffee", func(ctx context.Context, req DispenseReq) (DispenseResp, error) {
			if req.PurchaseID == "" {
				t.Errorf("expected non-empty purchaseId")
			}
			return DispenseResp{
				Dispensed:    true,
				PriceKopecks: req.PriceKopecks,
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// 7. Dispatch events & verify state
	run := engine.Execution("machine-1")

	if err := run.Dispatch(context.Background(), TestMoneyInserted{AmountKopecks: 200}); err != nil {
		t.Fatalf("dispatch money failed: %v", err)
	}

	if err := run.Dispatch(context.Background(), TestCoffeeRequested{PurchaseID: "p-1"}); err != nil {
		t.Fatalf("dispatch coffee failed: %v", err)
	}

	var state TestMachine
	if err := run.State(context.Background(), &state); err != nil {
		t.Fatalf("failed to read state: %v", err)
	}

	if state.CreditKopecks != 100 {
		t.Errorf("expected credit 100, got %d", state.CreditKopecks)
	}
	if state.WaterML != 950 {
		t.Errorf("expected water 950, got %d", state.WaterML)
	}
	if state.LastDrink != "espresso" {
		t.Errorf("expected lastDrink 'espresso', got %q", state.LastDrink)
	}
	if !state.LastDispensed {
		t.Errorf("expected lastDispensed true")
	}
}
