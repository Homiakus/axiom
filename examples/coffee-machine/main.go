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

// Цены и рецептура ингредиентов.
// Деньги хранятся целыми копейками (14000 = 140,00 ₽). Дробные float64 для денег не используются.
const (
	espressoPriceKopecks   = 9000  // Цена эспрессо (90,00 ₽)
	cappuccinoPriceKopecks = 14000 // Цена капучино (140,00 ₽)

	espressoWaterML = 40 // Расход воды на эспрессо (мл)
	espressoBeansG  = 8  // Расход кофе на эспрессо (г)

	cappuccinoWaterML = 60  // Расход воды на капучино (мл)
	cappuccinoBeansG  = 10  // Расход кофе на капучино (г)
	cappuccinoMilkML  = 120 // Расход молока на капучино (мл)
)

// Machine содержит всё долговременное состояние одного физического автомата.
// В приложении одному автомату соответствует один execution ID.
type Machine struct {
	// Деньги текущего покупателя, ещё не признанные выручкой.
	CreditKopecks int `json:"creditKopecks"`

	// Накопительные денежные счётчики.
	AcceptedKopecks int `json:"acceptedKopecks"` // Всего принятых денег
	ReturnedKopecks int `json:"returnedKopecks"` // Всего выданной сдачи и возвратов
	RevenueKopecks  int `json:"revenueKopecks"`  // Признанная выручка от проданных напитков
	CashboxKopecks  int `json:"cashboxKopecks"`  // Физические деньги в кассе автомата

	// Остатки ингредиентов и расходных материалов.
	WaterML int `json:"waterML"` // Вода (мл)
	BeansG  int `json:"beansG"`  // Зёрна кофе (г)
	MilkML  int `json:"milkML"`  // Молоко (мл)
	Cups    int `json:"cups"`    // Одноразовые стаканчики (шт)

	// Данные для экрана, телеметрии и технического журнала.
	DrinksServed      int    `json:"drinksServed"`      // Количество приготовленных напитков
	LastDrink         string `json:"lastDrink"`         // Название последнего проданного напитка
	LastChangeKopecks int    `json:"lastChangeKopecks"` // Сдача в последней операции
	LastDispensed     bool   `json:"lastDispensed"`     // Флаг успешности последней выдачи
}

// MoneyInserted поступает от монетоприёмника или платёжного терминала.
type MoneyInserted struct {
	AmountKopecks int `json:"amountKopecks"` // Внесённая сумма в копейках
}

func (MoneyInserted) AxiomEventName() string { return "MoneyInserted" }

// EspressoRequested — запрос на покупку эспрессо.
type EspressoRequested struct {
	PurchaseID string `json:"purchaseId"` // Уникальный ID операции для идемпотентности
}

func (EspressoRequested) AxiomEventName() string { return "EspressoRequested" }

// CappuccinoRequested — запрос на покупку капучино.
type CappuccinoRequested struct {
	PurchaseID string `json:"purchaseId"` // Уникальный ID операции для идемпотентности
}

func (CappuccinoRequested) AxiomEventName() string { return "CappuccinoRequested" }

// CancelRequested возвращает весь неиспользованный кредит покупателю.
type CancelRequested struct {
	OperationID string `json:"operationId"` // Уникальный ID операции отмены
}

func (CancelRequested) AxiomEventName() string { return "CancelRequested" }

// DTO для входных и выходных параметров внешних физических операций (Activities).
type DispenseInput struct {
	PurchaseID    string `json:"purchaseId"`    // Ключ покупки
	PriceKopecks  int    `json:"priceKopecks"`  // Фиксированная цена напитка
	ChangeKopecks int    `json:"changeKopecks"` // Рассчитанная сдача
}

type DispenseOutput struct {
	Dispensed     bool `json:"dispensed"`     // Подтверждение выдачи
	PriceKopecks  int  `json:"priceKopecks"`  // Подтверждённая цена
	ChangeKopecks int  `json:"changeKopecks"` // Подтверждённая сдача
}

type ReturnInput struct {
	OperationID   string `json:"operationId"`   // Ключ операции отмены
	AmountKopecks int    `json:"amountKopecks"` // Возвращаемая сумма
}

type ReturnOutput struct {
	Returned      bool `json:"returned"`      // Подтверждение возврата
	AmountKopecks int  `json:"amountKopecks"` // Подтверждённая сумма возврата
}

func main() {
	ctx := context.Background()

	// ---------------------------------------------------------------------
	// 1. Создание модели и привязка схем (100% типизация Go Generics)
	// ---------------------------------------------------------------------

	// Создаём описание доменной модели "CoffeeMachine" версии "1"
	definition := model.New("CoffeeMachine").Version("1")

	// Связываем структуру Machine со схемой состояния и задаём начальные значения (defaults)
	machine := model.Bind[Machine](definition, "Machine").
		Default("CreditKopecks", 0).        // Начальный кредит: 0 коп.
		Default("AcceptedKopecks", 0).      // Принято: 0 коп.
		Default("ReturnedKopecks", 0).      // Возвращено: 0 коп.
		Default("RevenueKopecks", 0).       // Выручка: 0 коп.
		Default("CashboxKopecks", 0).       // Касса: 0 коп.
		Default("WaterML", 2000).           // Запас воды: 2000 мл
		Default("BeansG", 500).             // Запас кофе: 500 г
		Default("MilkML", 1000).            // Запас молока: 1000 мл
		Default("Cups", 50).                // Запас стаканов: 50 шт
		Default("DrinksServed", 0).         // Выдано напитков: 0
		Default("LastDrink", "").           // Последний напиток: пустая строка
		Default("LastChangeKopecks", 0).    // Последняя сдача: 0
		Default("LastDispensed", false)     // Последняя выдача: false

	// Регистрируем типизированные события, авто-выводя имя из имени Go-структуры
	moneyInserted := model.EventOf[MoneyInserted](definition)         // Событие внесения денег
	espressoRequested := model.EventOf[EspressoRequested](definition)   // Событие заказа эспрессо
	cappuccinoRequested := model.EventOf[CappuccinoRequested](definition) // Событие заказа капучино
	cancelRequested := model.EventOf[CancelRequested](definition)       // Событие отмены

	// ---------------------------------------------------------------------
	// 2. Политика выполнения внешних операций (Hardware Policy)
	// ---------------------------------------------------------------------
	// Определяет правила повторов, тайм-аут и требование идемпотентности
	definition.Policy("hardwarePolicy").
		Retry(2).                     // До 2 повторных попыток при физическом сбое
		Timeout(10 * time.Second).     // Таймаут одной попытки — 10 секунд
		Concurrency("once").           // Строго последовательное выполнение операций
		Idempotency("required")        // Требовать обязательный ключ идемпотентности

	// ---------------------------------------------------------------------
	// 3. Описание внешних операций (Activities)
	// ---------------------------------------------------------------------

	// Выдача эспрессо
	definition.Activity("DispenseEspresso").
		Input("purchaseId", espressoRequested.String("PurchaseID")).                              // Вход: ID покупки из события
		Input("priceKopecks", espressoPriceKopecks).                                             // Вход: цена эспрессо
		Input("changeKopecks", machine.Int("CreditKopecks").Sub(espressoPriceKopecks)).            // Вход: расчет сдачи (Кредит - Цена)
		Output("dispensed", "Bool").                                                             // Выход: флаг успеха
		Output("priceKopecks", "Int").                                                            // Выход: сумма выручки
		Output("changeKopecks", "Int").                                                           // Выход: сумма сдачи
		Effect("external").                                                                      // Внешнее физическое действие
		IdempotencyKey(espressoRequested.String("PurchaseID")).                                  // Ключ идемпотентности
		Policy("hardwarePolicy")                                                                 // Применение политики оборудования

	// Выдача капучино
	definition.Activity("DispenseCappuccino").
		Input("purchaseId", cappuccinoRequested.String("PurchaseID")).                            // Вход: ID покупки из события
		Input("priceKopecks", cappuccinoPriceKopecks).                                           // Вход: цена капучино
		Input("changeKopecks", machine.Int("CreditKopecks").Sub(cappuccinoPriceKopecks)).          // Вход: расчет сдачи (Кредит - Цена)
		Output("dispensed", "Bool").                                                             // Выход: флаг успеха
		Output("priceKopecks", "Int").                                                            // Выход: сумма выручки
		Output("changeKopecks", "Int").                                                           // Выход: сумма сдачи
		Effect("external").                                                                      // Внешнее физическое действие
		IdempotencyKey(cappuccinoRequested.String("PurchaseID")).                                // Ключ идемпотентности
		Policy("hardwarePolicy")                                                                 // Применение политики оборудования

	// Возврат денег при отмене
	definition.Activity("ReturnMoney").
		Input("operationId", cancelRequested.String("OperationID")).                              // Вход: ID операции отмены
		Input("amountKopecks", machine.Int("CreditKopecks")).                                    // Вход: весь текущий кредит
		Output("returned", "Bool").                                                              // Выход: флаг успешного возврата
		Output("amountKopecks", "Int").                                                           // Выход: возвращённая сумма
		Effect("external").                                                                      // Внешнее физическое действие
		IdempotencyKey(cancelRequested.String("OperationID")).                                    // Ключ идемпотентности
		Policy("hardwarePolicy")                                                                 // Применение политики оборудования

	// ---------------------------------------------------------------------
	// 4. Правила переходов состояния (Fluent DSL)
	// ---------------------------------------------------------------------

	// Правило приёма денег
	definition.Rule("acceptMoney").
		On(moneyInserted.Trigger()).                                                              // Триггер: внесены деньги
		When(moneyInserted.Int("AmountKopecks").GT(0)).                                           // Условие: сумма внесенных денег > 0
		Set(machine.Int("CreditKopecks"), machine.Int("CreditKopecks").Add(moneyInserted.Int("AmountKopecks"))). // Увеличиваем кредит покупателя
		Set(machine.Int("AcceptedKopecks"), machine.Int("AcceptedKopecks").Add(moneyInserted.Int("AmountKopecks"))). // Увеличиваем общий счетчик принятого
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Add(moneyInserted.Int("AmountKopecks")))   // Увеличиваем физическую кассу

	// Правило продажи эспрессо
	definition.Rule("sellEspresso").
		On(espressoRequested.Trigger()).                                                          // Триггер: нажата кнопка эспрессо
		When(                                                                                     // Гарды (проверки ресурсов):
			machine.Int("CreditKopecks").GTE(espressoPriceKopecks),                               //   - Кредит >= 90 ₽
			machine.Int("WaterML").GTE(espressoWaterML),                                          //   - Вода >= 40 мл
			machine.Int("BeansG").GTE(espressoBeansG),                                            //   - Кофе >= 8 г
			machine.Int("Cups").GTE(1),                                                            //   - Стаканчики >= 1 шт
		).
		Run("DispenseEspresso").                                                                  // Запуск Activity вызова оборудования
		Set(machine.Int("CreditKopecks"), 0).                                                     // Обнуляем текущий кредит
		Set(machine.Int("ReturnedKopecks"), machine.Int("ReturnedKopecks").Add(model.OutputInt("changeKopecks"))). // Учитываем выданную сдачу
		Set(machine.Int("RevenueKopecks"), machine.Int("RevenueKopecks").Add(model.OutputInt("priceKopecks"))).     // Признаём выручку
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Sub(model.OutputInt("changeKopecks"))).   // Уменьшаем кассу на выданную сдачу
		Set(machine.Int("WaterML"), machine.Int("WaterML").Sub(espressoWaterML)).                 // Списываем воду
		Set(machine.Int("BeansG"), machine.Int("BeansG").Sub(espressoBeansG)).                     // Списываем кофе
		Set(machine.Int("Cups"), machine.Int("Cups").Sub(1)).                                     // Списываем 1 стакан
		Set(machine.Int("DrinksServed"), machine.Int("DrinksServed").Add(1)).                     // Увеличиваем счётчик выданных напитков
		Set(machine.String("LastDrink"), "espresso").                                             // Записываем имя последнего напитка
		Set(machine.Int("LastChangeKopecks"), model.OutputInt("changeKopecks")).                  // Записываем сдачу для журнала
		Set(machine.Bool("LastDispensed"), model.OutputBool("dispensed"))                         // Записываем флаг успеха для журнала

	// Правило продажи капучино
	definition.Rule("sellCappuccino").
		On(cappuccinoRequested.Trigger()).                                                        // Триггер: нажата кнопка капучино
		When(                                                                                     // Гарды (проверки ресурсов):
			machine.Int("CreditKopecks").GTE(cappuccinoPriceKopecks),                             //   - Кредит >= 140 ₽
			machine.Int("WaterML").GTE(cappuccinoWaterML),                                        //   - Вода >= 60 мл
			machine.Int("BeansG").GTE(cappuccinoBeansG),                                          //   - Кофе >= 10 г
			machine.Int("MilkML").GTE(cappuccinoMilkML),                                          //   - Молоко >= 120 мл
			machine.Int("Cups").GTE(1),                                                            //   - Стаканчики >= 1 шт
		).
		Run("DispenseCappuccino").                                                                // Запуск Activity вызова оборудования
		Set(machine.Int("CreditKopecks"), 0).                                                     // Обнуляем текущий кредит
		Set(machine.Int("ReturnedKopecks"), machine.Int("ReturnedKopecks").Add(model.OutputInt("changeKopecks"))). // Учитываем выданную сдачу
		Set(machine.Int("RevenueKopecks"), machine.Int("RevenueKopecks").Add(model.OutputInt("priceKopecks"))).     // Признаём выручку
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Sub(model.OutputInt("changeKopecks"))).   // Уменьшаем кассу на выданную сдачу
		Set(machine.Int("WaterML"), machine.Int("WaterML").Sub(cappuccinoWaterML)).               // Списываем воду
		Set(machine.Int("BeansG"), machine.Int("BeansG").Sub(cappuccinoBeansG)).                   // Списываем кофе
		Set(machine.Int("MilkML"), machine.Int("MilkML").Sub(cappuccinoMilkML)).                   // Списываем молоко
		Set(machine.Int("Cups"), machine.Int("Cups").Sub(1)).                                     // Списываем 1 стакан
		Set(machine.Int("DrinksServed"), machine.Int("DrinksServed").Add(1)).                     // Увеличиваем счётчик выданных напитков
		Set(machine.String("LastDrink"), "cappuccino").                                           // Записываем имя последнего напитка
		Set(machine.Int("LastChangeKopecks"), model.OutputInt("changeKopecks")).                  // Записываем сдачу для журнала
		Set(machine.Bool("LastDispensed"), model.OutputBool("dispensed"))                         // Записываем флаг успеха для журнала

	// Правило отмены покупки
	definition.Rule("cancelPurchase").
		On(cancelRequested.Trigger()).                                                            // Триггер: нажата кнопка отмены
		When(machine.Int("CreditKopecks").GT(0)).                                                 // Условие: текущий кредит > 0
		Run("ReturnMoney").                                                                       // Запуск Activity возврата денег
		Set(machine.Int("CashboxKopecks"), machine.Int("CashboxKopecks").Sub(model.OutputInt("amountKopecks"))).   // Уменьшаем кассу на возвращённую сумму
		Set(machine.Int("ReturnedKopecks"), machine.Int("ReturnedKopecks").Add(model.OutputInt("amountKopecks"))). // Увеличиваем счётчик возвратов
		Set(machine.Int("LastChangeKopecks"), model.OutputInt("amountKopecks")).                  // Записываем сумму возврата в журнал
		Set(machine.Int("CreditKopecks"), 0)                                                      // Обнуляем кредит

	// ---------------------------------------------------------------------
	// 5. Денежные и системные инварианты (Claims)
	// ---------------------------------------------------------------------

	// Закон сохранения денег: Принято = Возвращено + Выручка + Текущий Кредит
	definition.Claim(
		"moneyIsConserved",
		machine.Int("AcceptedKopecks").EQ(
			machine.Int("ReturnedKopecks").Add(
				machine.Int("RevenueKopecks").Add(machine.Int("CreditKopecks")),
			),
		),
	)

	// Сверка кассы: Касса = Выручка + Текущий Кредит
	definition.Claim(
		"cashboxMatchesAccounting",
		machine.Int("CashboxKopecks").EQ(
			machine.Int("RevenueKopecks").Add(machine.Int("CreditKopecks")),
		),
	)

	// Запрет отрицательных значений
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

	// ---------------------------------------------------------------------
	// 6. Компиляция плана, открытие Pebble БД и сборка Engine
	// ---------------------------------------------------------------------

	// Компилируем декларативную модель в executable Plan (проверка корректности типов и связей)
	plan, err := definition.Compile()
	if err != nil {
		log.Fatal(err)
	}

	// Создаём временный каталог для хранилища Pebble
	dir, err := os.MkdirTemp("", "axiom-coffee-machine-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Открываем транзакционное хранилище Pebble
	store, err := axiom.OpenPebble(filepath.Join(dir, "machine"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Инициализируем исполнительный движок Engine с типизированными обработчиками ActTyped
	engine, err := plan.New(
		axiom.WithStore(store),
		axiom.ActTyped("DispenseEspresso", dispenseDrinkTyped("espresso")),
		axiom.ActTyped("DispenseCappuccino", dispenseDrinkTyped("cappuccino")),
		axiom.ActTyped("ReturnMoney", returnMoneyTyped),
	)
	if err != nil {
		log.Fatal(err)
	}

	// ---------------------------------------------------------------------
	// 7. Исполнение пользовательских операций
	// ---------------------------------------------------------------------

	// Создаём или загружаем исполнение для конкретного автомата "coffee-machine-01"
	run := engine.Execution("coffee-machine-01")

	// Покупка №1: Вносим 200 ₽ (20000 коп.) и заказываем капучино (140 ₽)
	must(run.Dispatch(ctx, MoneyInserted{AmountKopecks: 20000}))
	must(run.Dispatch(ctx, CappuccinoRequested{PurchaseID: "sale-0001"}))

	// Покупка №2: Вносим 100 ₽ (10000 коп.) и заказываем эспрессо (90 ₽)
	must(run.Dispatch(ctx, MoneyInserted{AmountKopecks: 10000}))
	must(run.Dispatch(ctx, EspressoRequested{PurchaseID: "sale-0002"}))

	// Покупка №3 (Отмена): Вносим 50 ₽ (5000 коп.) и отменяем операцию
	must(run.Dispatch(ctx, MoneyInserted{AmountKopecks: 5000}))
	must(run.Dispatch(ctx, CancelRequested{OperationID: "cancel-0003"}))

	// ---------------------------------------------------------------------
	// 8. Чтение состояния, аудит истории и детерминированный Replay
	// ---------------------------------------------------------------------

	// Считываем текущее итоговое состояние автомата из базы данных
	var current Machine
	must(run.State(ctx, &current))

	// Получаем полную транзакционную историю событий
	history, err := run.History(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Восстанавливаем состояние из журнала с помощью Replay Engine
	replayed, err := axiom.ReplayFromHistory(plan.Module(), history)
	if err != nil {
		log.Fatal(err)
	}

	// Выводим результаты работы в консоль
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

// dispenseDrinkTyped возвращает типизированный обработчик приготовления напитка
func dispenseDrinkTyped(name string) func(context.Context, DispenseInput) (DispenseOutput, error) {
	return func(_ context.Context, input DispenseInput) (DispenseOutput, error) {
		fmt.Printf(
			"готовим %s: цена %s, сдача %s, операция %v\n",
			name,
			rubles(input.PriceKopecks),
			rubles(input.ChangeKopecks),
			input.PurchaseID,
		)
		return DispenseOutput{
			Dispensed:     true,
			PriceKopecks:  input.PriceKopecks,
			ChangeKopecks: input.ChangeKopecks,
		}, nil
	}
}

// returnMoneyTyped возвращает типизированный обработчик возврата денег
func returnMoneyTyped(_ context.Context, input ReturnInput) (ReturnOutput, error) {
	fmt.Printf(
		"возвращаем %s, операция %v\n",
		rubles(input.AmountKopecks),
		input.OperationID,
	)
	return ReturnOutput{
		Returned:      true,
		AmountKopecks: input.AmountKopecks,
	}, nil
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func rubles(kopecks int) string {
	return fmt.Sprintf("%d,%02d ₽", kopecks/100, kopecks%100)
}
