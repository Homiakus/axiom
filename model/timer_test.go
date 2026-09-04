package model

import (
	"strings"
	"testing"
	"time"
)

type timerModelState struct {
	CreatedAt string `json:"createdAt"`
	Deadline  string `json:"deadline"`
	Expired   bool   `json:"expired"`
}

func TestTypedTimerHelpersRenderAndCompile(t *testing.T) {
	definition := New("TimerModel")
	state := Bind[timerModelState](definition, "State")
	state.Default("CreatedAt", "2026-08-07T12:00:00Z")
	state.Default("Deadline", "2026-08-07T13:00:00Z")

	definition.Rule("absolute").
		On(OnTimerAt(state.String("Deadline"))).
		Set(state.Bool("Expired"), true)
	definition.Rule("relative").
		On(OnTimerAfter(15*time.Minute, state.String("CreatedAt"))).
		Set(state.Bool("Expired"), true)

	source := definition.Source()
	for _, expected := range []string{
		"on timer(State.deadline)",
		"on timer(15m0s after State.createdAt)",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("Source() = %q, missing %q", source, expected)
		}
	}
	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile() error = %v\nsource:\n%s", err, source)
	}
}

func TestTimerAfterRuntimeCreatedAtCompiles(t *testing.T) {
	definition := New("TimerRuntimeMetadata")
	state := Bind[timerModelState](definition, "State")
	definition.Rule("expire").
		On(OnTimerAfter(30*time.Minute, Runtime.CreatedAt())).
		Set(state.Bool("Expired"), true)

	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile() error = %v\nsource:\n%s", err, definition.Source())
	}
}
