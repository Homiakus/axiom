package diagram_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/diagram"
	"github.com/Homiakus/axiom/model"
)

type DiagramMachine struct {
	CreditKopecks int `json:"creditKopecks"`
}

type DiagramMoneyInserted struct {
	AmountKopecks int `json:"amountKopecks"`
}

type DiagramCoffeeRequested struct {
	PurchaseID string `json:"purchaseId"`
}

type DiagramDispenseReq struct {
	PurchaseID string `json:"purchaseId"`
}

type DiagramDispenseResp struct {
	Dispensed bool `json:"dispensed"`
}

func TestMermaidDiagramGeneration(t *testing.T) {
	definition := model.New("DiagramMachine").Version("1")

	machine := model.Bind[DiagramMachine](definition, "Machine").
		Default("CreditKopecks", 0)

	moneyInserted := model.EventOf[DiagramMoneyInserted](definition)
	coffeeRequested := model.EventOf[DiagramCoffeeRequested](definition)

	definition.Policy("hardwarePolicy").
		Retry(1).
		Timeout(5 * time.Second).
		Idempotency("required")

	definition.Activity("DispenseCoffee").
		Input("purchaseId", coffeeRequested.String("PurchaseID")).
		Output("dispensed", "Bool").
		Effect("external").
		IdempotencyKey(coffeeRequested.String("PurchaseID")).
		Policy("hardwarePolicy")

	definition.Rule("acceptMoney").
		On(moneyInserted.Trigger()).
		When(moneyInserted.Int("AmountKopecks").GT(0)).
		Set(machine.Int("CreditKopecks"), moneyInserted.Int("AmountKopecks"))

	definition.Rule("sellCoffee").
		On(coffeeRequested.Trigger()).
		When(machine.Int("CreditKopecks").GTE(100)).
		Run("DispenseCoffee").
		Set(machine.Int("CreditKopecks"), 0)

	definition.Claim("nonNegative", machine.Int("CreditKopecks").GTE(0))

	plan, err := definition.Compile()
	if err != nil {
		t.Fatalf("failed to compile plan: %v", err)
	}

	// 1. Test Mermaid Flowchart Generation
	mermaidChart := diagram.ToMermaidFlowchart(plan.Module())
	if !strings.Contains(mermaidChart, "flowchart TD") {
		t.Errorf("expected Mermaid flowchart header")
	}
	if !strings.Contains(mermaidChart, "sig_DiagramMoneyInserted") {
		t.Errorf("expected DiagramMoneyInserted signal node in flowchart")
	}
	if !strings.Contains(mermaidChart, "act_DispenseCoffee") {
		t.Errorf("expected DispenseCoffee activity node in flowchart")
	}

	// 2. Test PlantUML Generation
	plantUML := diagram.ToPlantUML(plan.Module())
	if !strings.Contains(plantUML, "@startuml") {
		t.Errorf("expected PlantUML header")
	}

	// 3. Test Runtime History Sequence Diagram Generation
	engine, err := plan.New(
		axiom.ActTyped("DispenseCoffee", func(ctx context.Context, req DiagramDispenseReq) (DiagramDispenseResp, error) {
			return DiagramDispenseResp{Dispensed: true}, nil
		}),
	)
	if err != nil {
		t.Fatalf("failed to build engine: %v", err)
	}

	run := engine.Execution("machine-diagram-1")
	_ = run.Dispatch(context.Background(), DiagramMoneyInserted{AmountKopecks: 200})
	_ = run.Dispatch(context.Background(), DiagramCoffeeRequested{PurchaseID: "p-1"})

	history, err := run.History(context.Background())
	if err != nil {
		t.Fatalf("failed to read history: %v", err)
	}

	seqDiagram := diagram.HistoryToMermaidSequence(history)
	if !strings.Contains(seqDiagram, "sequenceDiagram") {
		t.Errorf("expected sequenceDiagram header")
	}
	if !strings.Contains(seqDiagram, "DiagramMoneyInserted") {
		t.Errorf("expected DiagramMoneyInserted in sequence trace")
	}
}
