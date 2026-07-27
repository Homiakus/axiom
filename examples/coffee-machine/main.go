package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/model"
)

// Деньги хранятся целыми копейками. Значение 14000 означает 140,00 ₽.
// Для денежных расчётов намеренно не используется float64.
const (
	espressoPriceKopecks   = 9000
	cappuccinoPriceKopecks = 14000

	espressoWaterML = 40
	espressoBeansG  = 8

	cappuccinoWaterML = 60
	cappuccinoBeansG  = 10
	cappuccinoMilkML  = 120
)

// Machine содержит всё долговременное состояние одного физического автомата.
// В приложении одному автомату соответствует один execution ID.
type Machine struct {
	// Деньги текущего покупателя, ещё не признанные выручкой.
	CreditKopecks int `json:"creditKopecks"`

	// Накопительные денежные счётчики.
	AcceptedKopecks int `json:"acceptedKopecks"`
	ReturnedKopecks int `json:"returnedKopecks"`
	RevenueKopecks  int `json:"revenueKopecks"`
	CashboxKopecks  int `json:"cashboxKopecks"`

	// Остатки ингредиентов и расходных материалов.
	WaterML int `json:"waterML"`
	BeansG  int `json:"beansG"`
	MilkML  int `json:"milkML"`
	Cups    int `json:"cups"`

	// Данные для экрана, телеметрии и технического журнала.
	DrinksServed      int    `json:"drinksServed"`
	LastDrink         string `json:"lastDrink"`
	LastChangeKopecks int    `json:"lastChangeKopecks"`
	LastDispensed     bool   `json:"lastDispensed"`
}

// MoneyInserted поступает от монетоприёмника или платёжного терминала.
type MoneyInserted struct {
	AmountKopecks int `json:"amountKopecks"`
}

func (MoneyInserted) AxiomEventName() string { return "MoneyInserted" }

// Для каждого напитка используется отдельный тип события. Клиент передаёт
// только идентификатор покупки и не может подменить цену или рецепт.
type EspressoRequested struct {
	PurchaseID string `json:"purchaseId"`
}

func (EspressoRequested) AxiomEventName() string { return "EspressoRequested" }

type CappuccinoRequested struct {
	PurchaseID string `json:"purchaseId"`
}

func (CappuccinoRequested) AxiomEventName() string { return "CappuccinoRequested" }

// CancelRequested возвращает весь неиспользованный кредит покупателю.
type CancelRequested struct {
	OperationID string `json:"operationId"`
}

func (CancelRequested) AxiomEventName() string { return "CancelRequested" }

// add и sub строят арифметические выражения декларативной модели.
func add(left, right model.Expr) model.Expr {
	return model.Raw(fmt.Sprintf("(%s + %s)", left, right))
}

func sub(left, right model.Expr) model.Expr {
	return model.Raw(fmt.Sprintf("(%s - %s)", left, right))
}

func main() {
	ctx := context.Background()

	// ---------------------------------------------------------------------
	// 1. Состояние и события
	// ---------------------------------------------------------------------
	definition := model.New("CoffeeMachine").Version("1")

	machine := model.State[Machine](definition, "Machine").
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

	moneyInserted := model.Event[MoneyInserted](definition, "MoneyInserted")
	espressoRequested := model.Event[EspressoRequested](definition, "EspressoRequested")
	cappuccinoRequested := model.Event[CappuccinoRequested](definition, "CappuccinoRequested")
	cancelRequested := model.Event[CancelRequested](definition, "CancelRequested")

	// ---------------------------------------------------------------------
	// 2. Политика работы с оборудованием
	// ---------------------------------------------------------------------
	// Приготовление напитка и возврат денег являются внешними действиями.
	// Политика задаёт повторы, тайм-аут, последовательное выполнение и
	// обязательный ключ идемпотентности.
	definition.Policy("hardwarePolicy").
		Retry(2).
		Timeout(10 * time.Second).
		Concurrency("once").
		Idempotency("required")

	// ---------------------------------------------------------------------
	// 3. Внешние операции
	// ---------------------------------------------------------------------
	// Цена и сдача вычисляются до обращения к оборудованию. Обработчик
	// возвращает подтверждённые значения, которые затем учитываются в кассе.
	definition.Activity("DispenseEspresso").
		Input("purchaseId", espressoRequested.Field("PurchaseID")).
		Input("priceKopecks", model.Lit(espressoPriceKopecks)).
		Input("changeKopecks", sub(machine.Field("CreditKopecks"), model.Lit(espressoPriceKopecks))).
		Output("dispensed", "Bool").
		Output("priceKopecks", "Int").
		Output("changeKopecks", "Int").
		Effect("external").
		IdempotencyKey(espressoRequested.Field("PurchaseID")).
		Policy("hardwarePolicy")

	definition.Activity("DispenseCappuccino").
		Input("purchaseId", cappuccinoRequested.Field("PurchaseID")).
		Input("priceKopecks", model.Lit(cappuccinoPriceKopecks)).
		Input("changeKopecks", sub(machine.Field("CreditKopecks"), model.Lit(cappuccinoPriceKopecks))).
		Output("dispensed", "Bool").
		Output("priceKopecks", "Int").
		Output("changeKopecks", "Int").
		Effect("external").
		IdempotencyKey(cappuccinoRequested.Field("PurchaseID")).
		Policy("hardwarePolicy")

	definition.Activity("ReturnMoney").
		Input("operationId", cancelRequested.Field("OperationID")).
		Input("amountKopecks", machine.Field("CreditKopecks")).
		Output("returned", "Bool").
		Output("amountKopecks", "Int").
		Effect("external").
		IdempotencyKey(cancelRequested.Field("OperationID")).
		Policy("hardwarePolicy")

	// ---------------------------------------------------------------------
	// 4. Правила переходов
	// ---------------------------------------------------------------------
	// Принятые деньги увеличивают кредит покупателя, физическую кассу и
	// общий счётчик принятых средств.
	definition.Rule("acceptMoney").
		On(moneyInserted.Trigger()).
		When(model.GT(moneyInserted.Field("AmountKopecks"), model.Lit(0))).
		Set(machine.Field("CreditKopecks"), add(machine.Field("CreditKopecks"), moneyInserted.Field("AmountKopecks"))).
		Set(machine.Field("AcceptedKopecks"), add(machine.Field("AcceptedKopecks"), moneyInserted.Field("AmountKopecks"))).
		Set(machine.Field("CashboxKopecks"), add(machine.Field("CashboxKopecks"), moneyInserted.Field("AmountKopecks")))

	// Эспрессо выдаётся только при достаточном кредите и остатках.
	definition.Rule("sellEspresso").
		On(espressoRequested.Trigger()).
		When(
			model.GTE(machine.Field("CreditKopecks"), model.Lit(espressoPriceKopecks)),
			model.GTE(machine.Field("WaterML"), model.Lit(espressoWaterML)),
			model.GTE(machine.Field("BeansG"), model.Lit(espressoBeansG)),
			model.GTE(machine.Field("Cups"), model.Lit(1)),
		).
		Run("DispenseEspresso").
		Set(machine.Field("CreditKopecks"), model.Lit(0)).
		Set(machine.Field("ReturnedKopecks"), add(machine.Field("ReturnedKopecks"), model.Ref("output.changeKopecks"))).
		Set(machine.Field("RevenueKopecks"), add(machine.Field("RevenueKopecks"), model.Ref("output.priceKopecks"))).
		Set(machine.Field("CashboxKopecks"), sub(machine.Field("CashboxKopecks"), model.Ref("output.changeKopecks"))).
		Set(machine.Field("WaterML"), sub(machine.Field("WaterML"), model.Lit(espressoWaterML))).
		Set(machine.Field("BeansG"), sub(machine.Field("BeansG"), model.Lit(espressoBeansG))).
		Set(machine.Field("Cups"), sub(machine.Field("Cups"), model.Lit(1))).
		Set(machine.Field("DrinksServed"), add(machine.Field("DrinksServed"), model.Lit(1))).
		Set(machine.Field("LastDrink"), model.Lit("espresso")).
		Set(machine.Field("LastChangeKopecks"), model.Ref("output.changeKopecks")).
		Set(machine.Field("LastDispensed"), model.Ref("output.dispensed"))

	// Капучино дополнительно проверяет и списывает молоко.
	definition.Rule("sellCappuccino").
		On(cappuccinoRequested.Trigger()).
		When(
			model.GTE(machine.Field("CreditKopecks"), model.Lit(cappuccinoPriceKopecks)),
			model.GTE(machine.Field("WaterML"), model.Lit(cappuccinoWaterML)),
			model.GTE(machine.Field("BeansG"), model.Lit(cappuccinoBeansG)),
			model.GTE(machine.Field("MilkML"), model.Lit(cappuccinoMilkML)),
			model.GTE(machine.Field("Cups"), model.Lit(1)),
		).
		Run("DispenseCappuccino").
		Set(machine.Field("CreditKopecks"), model.Lit(0)).
		Set(machine.Field("ReturnedKopecks"), add(machine.Field("ReturnedKopecks"), model.Ref("output.changeKopecks"))).
		Set(machine.Field("RevenueKopecks"), add(machine.Field("RevenueKopecks"), model.Ref("output.priceKopecks"))).
		Set(machine.Field("CashboxKopecks"), sub(machine.Field("CashboxKopecks"), model.Ref("output.changeKopecks"))).
		Set(machine.Field("WaterML"), sub(machine.Field("WaterML"), model.Lit(cappuccinoWaterML))).
		Set(machine.Field("BeansG"), sub(machine.Field("BeansG"), model.Lit(cappuccinoBeansG))).
		Set(machine.Field("MilkML"), sub(machine.Field("MilkML"), model.Lit(cappuccinoMilkML))).
		Set(machine.Field("Cups"), sub(machine.Field("Cups"), model.Lit(1))).
		Set(machine.Field("DrinksServed"), add(machine.Field("DrinksServed"), model.Lit(1))).
		Set(machine.Field("LastDrink"), model.Lit("cappuccino")).
		Set(machine.Field("LastChangeKopecks"), model.Ref("output.changeKopecks")).
		Set(machine.Field("LastDispensed"), model.Ref("output.dispensed"))

	// Отмена возвращает весь текущий кредит. Выручка не изменяется.
	definition.Rule("cancelPurchase").
		On(cancelRequested.Trigger()).
		When(model.GT(machine.Field("CreditKopecks"), model.Lit(0))).
		Run("ReturnMoney").
		Set(machine.Field("CashboxKopecks"), sub(machine.Field("CashboxKopecks"), model.Ref("output.amountKopecks"))).
		Set(machine.Field("ReturnedKopecks"), add(machine.Field("ReturnedKopecks"), model.Ref("output.amountKopecks"))).
		Set(machine.Field("LastChangeKopecks"), model.Ref("output.amountKopecks")).
		Set(machine.Field("CreditKopecks"), model.Lit(0))

	// ---------------------------------------------------------------------
	// 5. Инварианты
	// ---------------------------------------------------------------------
	// Принятые деньги должны находиться ровно в одном из трёх мест:
	// возвращены покупателям, признаны выручкой или лежат в текущем кредите.
	definition.Claim(
		"moneyIsConserved",
		model.Eq(
			machine.Field("AcceptedKopecks"),
			add(
				machine.Field("ReturnedKopecks"),
				add(machine.Field("RevenueKopecks"), machine.Field("CreditKopecks")),
			),
		),
	)

	// При нулевом стартовом разменном фонде физическая касса равна выручке
	// плюс неиспользованный кредит текущего покупателя.
	definition.Claim(
		"cashboxMatchesAccounting",
		model.Eq(
			machine.Field("CashboxKopecks"),
			add(machine.Field("RevenueKopecks"), machine.Field("CreditKopecks")),
		),
	)

	// Деньги и ингредиенты не могут стать отрицательными.
	definition.Claim(
		"valuesAreNonNegative",
		model.GTE(machine.Field("CreditKopecks"), model.Lit(0)),
		model.GTE(machine.Field("AcceptedKopecks"), model.Lit(0)),
		model.GTE(machine.Field("ReturnedKopecks"), model.Lit(0)),
		model.GTE(machine.Field("RevenueKopecks"), model.Lit(0)),
		model.GTE(machine.Field("CashboxKopecks"), model.Lit(0)),
		model.GTE(machine.Field("WaterML"), model.Lit(0)),
		model.GTE(machine.Field("BeansG"), model.Lit(0)),
		model.GTE(machine.Field("MilkML"), model.Lit(0)),
		model.GTE(machine.Field("Cups"), model.Lit(0)),
	)

	// ---------------------------------------------------------------------
	// 6. Компиляция и хранение
	// ---------------------------------------------------------------------
	// Compile проверяет имена полей, типы выражений, правила, activities и
	// claims до того, как автомат примет первую монету.
	plan, err := definition.Compile()
	if err != nil {
		log.Fatal(err)
	}

	// Для демонстрации используется временный каталог. В реальном автомате
	// здесь будет постоянный путь, например /var/lib/coffee-machine/axiom.
	dir, err := os.MkdirTemp("", "axiom-coffee-machine-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := axiom.OpenPebble(filepath.Join(dir, "machine"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Pebble отвечает за транзакционное хранение. Строгий fast runtime здесь
	// не включён, потому что бухгалтерские Claims используют арифметику.
	engine, err := plan.New(
		axiom.WithStore(store),
		axiom.Act("DispenseEspresso", dispenseDrink("espresso")),
		axiom.Act("DispenseCappuccino", dispenseDrink("cappuccino")),
		axiom.Act("ReturnMoney", returnMoney),
	)
	if err != nil {
		log.Fatal(err)
	}

	// ---------------------------------------------------------------------
	// 7. Три пользовательских операции
	// ---------------------------------------------------------------------
	// Один execution хранит состояние одного физического автомата.
	run := engine.Execution("coffee-machine-01")

	// Покупка №1: внесено 200 ₽, капучино стоит 140 ₽, сдача 60 ₽.
	must(run.Dispatch(ctx, MoneyInserted{AmountKopecks: 20000}))
	must(run.Dispatch(ctx, CappuccinoRequested{PurchaseID: "sale-0001"}))

	// Покупка №2: внесено 100 ₽, эспрессо стоит 90 ₽, сдача 10 ₽.
	must(run.Dispatch(ctx, MoneyInserted{AmountKopecks: 10000}))
	must(run.Dispatch(ctx, EspressoRequested{PurchaseID: "sale-0002"}))

	// Покупка №3 отменена: внесённые 50 ₽ полностью возвращаются.
	must(run.Dispatch(ctx, MoneyInserted{AmountKopecks: 5000}))
	must(run.Dispatch(ctx, CancelRequested{OperationID: "cancel-0003"}))

	// ---------------------------------------------------------------------
	// 8. Состояние, история и replay
	// ---------------------------------------------------------------------
	var current Machine
	must(run.State(ctx, &current))

	history, err := run.History(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Replay строит состояние заново из журнала и проверяет хеш плана.
	replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\nИтог автомата")
	fmt.Printf("  принято:    %s\n", rubles(current.AcceptedKopecks))
	fmt.Printf("  возвращено: %s\n", rubles(current.ReturnedKopecks))
	fmt.Printf("  выручка:    %s\n", rubles(current.RevenueKopecks))
	fmt.Printf("  касса:      %s\n", rubles(current.CashboxKopecks))
	fmt.Printf("  кредит:     %s\n", rubles(current.CreditKopecks))
	fmt.Printf("  напитков:   %d\n", current.DrinksServed)
	fmt.Printf("  вода:       %d мл\n", current.WaterML)
	fmt.Printf("  кофе:       %d г\n", current.BeansG)
	fmt.Printf("  молоко:     %d мл\n", current.MilkML)
	fmt.Printf("  стаканы:    %d\n", current.Cups)
	fmt.Printf("  записей истории: %d\n", len(history))
	fmt.Printf("  статус после replay: %s\n", replayed.Status)
}

// dispenseDrink имитирует контроллер кофемолки, нагревателя, насосов и
// механизма выдачи сдачи. В реальном приложении обработчик обязан хранить
// PurchaseID и не повторять уже завершённую физическую операцию.
func dispenseDrink(name string) axiom.Activity {
	return func(_ context.Context, input axiom.Input) (axiom.Output, error) {
		fmt.Printf(
			"готовим %s: цена %s, сдача %s, операция %v\n",
			name,
			rubles(input["priceKopecks"].(int)),
			rubles(input["changeKopecks"].(int)),
			input["purchaseId"],
		)
		return axiom.Output{
			"dispensed":     true,
			"priceKopecks":  input["priceKopecks"],
			"changeKopecks": input["changeKopecks"],
		}, nil
	}
}

func returnMoney(_ context.Context, input axiom.Input) (axiom.Output, error) {
	fmt.Printf(
		"возвращаем %s, операция %v\n",
		rubles(input["amountKopecks"].(int)),
		input["operationId"],
	)
	return axiom.Output{
		"returned":      true,
		"amountKopecks": input["amountKopecks"],
	}, nil
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// Форматирование также выполняется без float64: 14000 -> "140,00 ₽".
func rubles(kopecks int) string {
	return fmt.Sprintf("%d,%02d ₽", kopecks/100, kopecks%100)
}
