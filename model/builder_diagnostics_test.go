package model

import (
	"strings"
	"testing"
)

func TestUnknownStateFieldBecomesCompileDiagnostic(t *testing.T) {
	type State struct {
		Value int `json:"value"`
	}

	definition := New("StateFieldGuard")
	state := Bind[State](definition, "State")
	definition.Claim("bad", state.Int("Vaule").Equal(1))

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected builder diagnostic")
	}
	if !strings.Contains(err.Error(), "AX509") || !strings.Contains(err.Error(), "State.Vaule") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnknownEventFieldBecomesCompileDiagnostic(t *testing.T) {
	type Event struct {
		Value int `json:"value"`
	}

	definition := New("EventFieldGuard")
	event := EventOf[Event](definition)
	definition.Rule("bad").On(event.Trigger()).When(event.Int("Vaule").Equal(1))

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected builder diagnostic")
	}
	if !strings.Contains(err.Error(), "AX509") || !strings.Contains(err.Error(), "Event.Vaule") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNonStructStateBecomesCompileDiagnostic(t *testing.T) {
	definition := New("TypeGuard")
	State[int](definition, "State")

	_, err := definition.Compile()
	if err == nil {
		t.Fatal("expected builder diagnostic")
	}
	if !strings.Contains(err.Error(), "AX509") || !strings.Contains(err.Error(), "State") {
		t.Fatalf("unexpected error: %v", err)
	}
}
