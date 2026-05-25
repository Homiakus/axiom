package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestModelTabNavigation(t *testing.T) {
	model := InitialModel(t.TempDir())

	next, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = next.(Model)
	if model.activeTab != tabValidation {
		t.Fatalf("active tab after tab = %v", model.activeTab)
	}

	next, _ = model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}))
	model = next.(Model)
	if model.activeTab != tabFiles {
		t.Fatalf("active tab after shift+tab = %v", model.activeTab)
	}
}

func TestModelValidationSuccessMessagePreparesRuntime(t *testing.T) {
	model := InitialModel(t.TempDir())
	report := ValidateFile(writeAXM(t, validSource))

	next, _ := model.Update(validationFinishedMsg{Report: report})
	model = next.(Model)

	if model.activeTab != tabSummary {
		t.Fatalf("active tab = %v", model.activeTab)
	}
	if model.runtime == nil {
		t.Fatalf("runtime session was not prepared")
	}
	if model.report == nil || !model.report.OK() {
		t.Fatalf("report not stored: %#v", model.report)
	}
}

func TestModelValidationFailureMessageStaysOnValidation(t *testing.T) {
	model := InitialModel(t.TempDir())
	report := ValidateFile(writeAXM(t, "domain Bad\n  signal Nope\n"))

	next, _ := model.Update(validationFinishedMsg{Report: report})
	model = next.(Model)

	if model.activeTab != tabValidation {
		t.Fatalf("active tab = %v", model.activeTab)
	}
	if model.runtime != nil {
		t.Fatalf("runtime session should not be prepared")
	}
	if model.report == nil || model.report.OK() {
		t.Fatalf("failure report not stored: %#v", model.report)
	}
}

func TestModelFileSelectionStartsValidation(t *testing.T) {
	model := InitialModel(t.TempDir())
	path := writeAXM(t, validSource)

	next, cmd := model.startValidation(path)
	model = next.(Model)

	if cmd == nil {
		t.Fatalf("validation command is nil")
	}
	if model.activeTab != tabValidation || !model.validating {
		t.Fatalf("validation state: tab=%v validating=%v", model.activeTab, model.validating)
	}
	if model.selectedPath == "" {
		t.Fatalf("selected path not stored")
	}
}

func TestModelRetryValidation(t *testing.T) {
	model := InitialModel(t.TempDir())
	model.selectedPath = writeAXM(t, validSource)
	model.activeTab = tabValidation

	next, cmd := model.Update(tea.KeyPressMsg(tea.Key{Text: "r", Code: 'r'}))
	model = next.(Model)

	if cmd == nil {
		t.Fatalf("retry command is nil")
	}
	if !model.validating {
		t.Fatalf("retry did not enter validating state")
	}
}
