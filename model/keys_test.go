package model

import (
	"strings"
	"testing"
)

type keyedCounter struct {
	Count int     `json:"count"`
	Label *string `json:"label,omitempty"`
}

type keyedIncrement struct {
	By int `json:"by"`
}

func TestFieldKeysCompileAndReuse(t *testing.T) {
	definition := New("KeyedCounter")
	counter := Bind[keyedCounter](definition, "Counter")
	increment := EventOf[keyedIncrement](definition)

	countKey := Key[keyedCounter, int]("Count")
	labelKey := Key[keyedCounter, string]("label")
	byKey := Key[keyedIncrement, int]("By")

	StateDefault(counter, countKey, 0)

	count := StateField(counter, countKey)
	label := StateField(counter, labelKey)
	by := EventField(increment, byKey)

	definition.Rule("increment").
		On(increment.Trigger()).
		Set(count, count.PlusField(by))
	definition.Claim("nonNegative", count.GreaterOrEqual(0))
	definition.Query("view", map[string]Expr{
		"count": count.Expr(),
		"label": label.Expr(),
	})

	if got := StateChanged(counter, countKey).text; got != "changed(Counter.count)" {
		t.Fatalf("StateChanged: got %q", got)
	}
	if got := countKey.Name(); got != "Count" {
		t.Fatalf("Name: got %q", got)
	}

	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func TestFieldKeyRejectsWrongValueType(t *testing.T) {
	definition := New("WrongKeyType")
	counter := Bind[keyedCounter](definition, "Counter")

	wrong := Key[keyedCounter, string]("Count")
	_ = StateField(counter, wrong)

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected typed field key diagnostic")
	}
	if !strings.Contains(err.Error(), "typed field key") || !strings.Contains(err.Error(), "Count") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFieldKeyRejectsUnknownField(t *testing.T) {
	definition := New("UnknownKey")
	counter := Bind[keyedCounter](definition, "Counter")

	missing := Key[keyedCounter, int]("Missing")
	_ = StateField(counter, missing)

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected unknown field diagnostic")
	}
	if !strings.Contains(err.Error(), "unknown state field Counter.Missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}
