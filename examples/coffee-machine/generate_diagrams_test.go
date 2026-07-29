package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/diagram"
	"github.com/Homiakus/axiom/model"
)

func TestGenerateCoffeeMachineDiagrams(t *testing.T) {
	ctx := context.Background()

	definition := model.New("CoffeeMachine").Version("1")

	machine := model.Bind[Machine](definition, "Machine").
		Default("CreditKopecks", 0).
		Default("AcceptedKopecks", 0).
		Default("ReturnedKopecks", 0).
		Default("RevenueKopecks", 0).
		Default("CashboxKopecks", 0).
		Default("WaterML", 2000).
		Default("BeansG", 500).
		Default("MilkML", 1000).
		Default("Cups", 50).
		Default("DrinksServed", 0).
		Default("LastDrink", "").
		Default("LastChangeKopecks", 0).
		Default("LastDispensed", false)

	moneyInserted := model.EventOf[MoneyInserted](definition)
	espressoRequested := model.EventOf[EspressoRequested](definition)
	cappuccinoRequested := model.EventOf[CappuccinoRequested](definition)
	cancelRequested := model.EventOf[CancelRequested](definition)

	definition.Policy("hardwarePolicy").
		Retry(2).
		Timeout(10 * time.Second).
		Concurrency("once").
		Idempotency("required")

	definition.Activity("DispenseEspresso").
		Input("purchaseId", espressoRequested.String("PurchaseID")).
		Input("priceKopecks", espressoPriceKopecks).
		Input("changeKopecks", machine.Int("CreditKopecks").Sub(espressoPriceKopecks)).
		Output("dispensed", "Bool").
		Output("priceKopecks", "Int").
		Output("changeKopecks", "Int").
		Effect("external").
		IdempotencyKey(espressoRequested.String("PurchaseID")).
		Policy("hardwarePolicy")

	definition.Activity("DispenseCappuccino").
		Input("purchaseId", cappuccinoRequested.String("PurchaseID")).
		Input("priceKopecks", cappuccinoPriceKopecks).
		Input("changeKopecks", machine.Int("CreditKopecks").Sub(cappuccinoPriceKopecks)).
		Output("dispensed", "Bool").
		Output("priceKopecks", "Int").
		Output("changeKopecks", "Int").
		Effect("external").
		IdempotencyKey(cappuccinoRequested.String("PurchaseID")).
		Policy("hardwarePolicy")

	definition.Activity("ReturnMoney").
		Input("operationId", cancelRequested.String("OperationID")).
		Input("amountKopecks", machine.Int("CreditKopecks")).
		Output("returned", "Bool").
		Output("amountKopecks", "Int").
		Effect("external").
		IdempotencyKey(cancelRequested.String("OperationID")).
		Policy("hardwarePolicy")

	definition.Rule("acceptMoney").
		On(moneyInserted.Trigger()).
		When(moneyInserted.Int("AmountKopecks").GT(0)).
		Set(machine.Int("CreditKopecks"), machine.Int("CreditKopecks").Add(moneyInserted.Int("AmountKopecks"))).
		Set(machine.Int("AcceptedKopecks"), machine.Int("AcceptedKopecks").Add(moneyInserted.Int("AmountKopecks"))).
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Add(moneyInserted.Int("AmountKopecks")))

	definition.Rule("sellEspresso").
		On(espressoRequested.Trigger()).
		When(
			machine.Int("CreditKopecks").GTE(espressoPriceKopecks),
			machine.Int("WaterML").GTE(espressoWaterML),
			machine.Int("BeansG").GTE(espressoBeansG),
			machine.Int("Cups").GTE(1),
		).
		Run("DispenseEspresso").
		Set(machine.Int("CreditKopecks"), 0).
		Set(machine.Int("ReturnedKopecks"), machine.Int("ReturnedKopecks").Add(model.OutputInt("changeKopecks"))).
		Set(machine.Int("RevenueKopecks"), machine.Int("RevenueKopecks").Add(model.OutputInt("priceKopecks"))).
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Sub(model.OutputInt("changeKopecks"))).
		Set(machine.Int("WaterML"), machine.Int("WaterML").Sub(espressoWaterML)).
		Set(machine.Int("BeansG"), machine.Int("BeansG").Sub(espressoBeansG)).
		Set(machine.Int("Cups"), machine.Int("Cups").Sub(1)).
		Set(machine.Int("DrinksServed"), machine.Int("DrinksServed").Add(1)).
		Set(machine.String("LastDrink"), "espresso").
		Set(machine.Int("LastChangeKopecks"), model.OutputInt("changeKopecks")).
		Set(machine.Bool("LastDispensed"), model.OutputBool("dispensed"))

	definition.Rule("sellCappuccino").
		On(cappuccinoRequested.Trigger()).
		When(
			machine.Int("CreditKopecks").GTE(cappuccinoPriceKopecks),
			machine.Int("WaterML").GTE(cappuccinoWaterML),
			machine.Int("BeansG").GTE(cappuccinoBeansG),
			machine.Int("MilkML").GTE(cappuccinoMilkML),
			machine.Int("Cups").GTE(1),
		).
		Run("DispenseCappuccino").
		Set(machine.Int("CreditKopecks"), 0).
		Set(machine.Int("ReturnedKopecks"), machine.Int("ReturnedKopecks").Add(model.OutputInt("changeKopecks"))).
		Set(machine.Int("RevenueKopecks"), machine.Int("RevenueKopecks").Add(model.OutputInt("priceKopecks"))).
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Sub(model.OutputInt("changeKopecks"))).
		Set(machine.Int("WaterML"), machine.Int("WaterML").Sub(cappuccinoWaterML)).
		Set(machine.Int("BeansG"), machine.Int("BeansG").Sub(cappuccinoBeansG)).
		Set(machine.Int("MilkML"), machine.Int("MilkML").Sub(cappuccinoMilkML)).
		Set(machine.Int("Cups"), machine.Int("Cups").Sub(1)).
		Set(machine.Int("DrinksServed"), machine.Int("DrinksServed").Add(1)).
		Set(machine.String("LastDrink"), "cappuccino").
		Set(machine.Int("LastChangeKopecks"), model.OutputInt("changeKopecks")).
		Set(machine.Bool("LastDispensed"), model.OutputBool("dispensed"))

	definition.Rule("cancelPurchase").
		On(cancelRequested.Trigger()).
		When(machine.Int("CreditKopecks").GT(0)).
		Run("ReturnMoney").
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Sub(model.OutputInt("amountKopecks"))).
		Set(machine.Int("ReturnedKopecks"), machine.Int("ReturnedKopecks").Add(model.OutputInt("amountKopecks"))).
		Set(machine.Int("LastChangeKopecks"), model.OutputInt("amountKopecks")).
		Set(machine.Int("CreditKopecks"), 0)

	definition.Claim(
		"moneyIsConserved",
		machine.Int("AcceptedKopecks").EQ(
			machine.Int("ReturnedKopecks").Add(
				machine.Int("RevenueKopecks").Add(machine.Int("CreditKopecks")),
			),
		),
	)

	definition.Claim(
		"cashboxMatchesAccounting",
		machine.Int("CashboxKopecks").EQ(
			machine.Int("RevenueKopecks").Add(machine.Int("CreditKopecks")),
		),
	)

	definition.Claim(
		"valuesAreNonNegative",
		machine.Int("CreditKopecks").GTE(0),
		machine.Int("AcceptedKopecks").GTE(0),
		machine.Int("ReturnedKopecks").GTE(0),
		machine.Int("RevenueKopecks").GTE(0),
		machine.Int("CashboxKopecks").GTE(0),
		machine.Int("WaterML").GTE(0),
		machine.Int("BeansG").GTE(0),
		machine.Int("MilkML").GTE(0),
		machine.Int("Cups").GTE(0),
	)

	plan, err := definition.Compile()
	if err != nil {
		t.Fatalf("failed to compile plan: %v", err)
	}

	mermaidFlowchart := diagram.ToMermaidFlowchart(plan.Module())
	plantUML := diagram.ToPlantUML(plan.Module())

	engine, err := plan.New(
		axiom.ActTyped("DispenseEspresso", dispenseDrinkTyped("espresso")),
		axiom.ActTyped("DispenseCappuccino", dispenseDrinkTyped("cappuccino")),
		axiom.ActTyped("ReturnMoney", returnMoneyTyped),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	run := engine.Execution("coffee-machine-01")
	_ = run.Dispatch(ctx, MoneyInserted{AmountKopecks: 20000})
	_ = run.Dispatch(ctx, CappuccinoRequested{PurchaseID: "sale-0001"})
	_ = run.Dispatch(ctx, MoneyInserted{AmountKopecks: 10000})
	_ = run.Dispatch(ctx, EspressoRequested{PurchaseID: "sale-0002"})
	_ = run.Dispatch(ctx, MoneyInserted{AmountKopecks: 5000})
	_ = run.Dispatch(ctx, CancelRequested{OperationID: "cancel-0003"})

	history, err := run.History(ctx)
	if err != nil {
		t.Fatalf("failed to get history: %v", err)
	}

	mermaidSequence := diagram.HistoryToMermaidSequence(history)

	targetDir := "."
	if err := os.WriteFile(filepath.Join(targetDir, "diagram_flowchart.mmd"), []byte(mermaidFlowchart), 0644); err != nil {
		t.Fatalf("failed to write diagram_flowchart.mmd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "diagram_sequence.mmd"), []byte(mermaidSequence), 0644); err != nil {
		t.Fatalf("failed to write diagram_sequence.mmd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "diagram.puml"), []byte(plantUML), 0644); err != nil {
		t.Fatalf("failed to write diagram.puml: %v", err)
	}

	doc := fmt.Sprintf("# Coffee Machine Process Diagrams\n\n## 1. Flowchart Diagram (Mermaid)\n```mermaid\n%s```\n\n## 2. Execution Sequence Diagram (Mermaid)\n```mermaid\n%s```\n\n## 3. State Diagram (PlantUML)\n```plantuml\n%s```\n", mermaidFlowchart, mermaidSequence, plantUML)
	if err := os.WriteFile(filepath.Join(targetDir, "DIAGRAMS.md"), []byte(doc), 0644); err != nil {
		t.Fatalf("failed to write DIAGRAMS.md: %v", err)
	}
}
