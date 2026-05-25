package tui

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"axiom/pkg/axiom"
)

type StageStatus string

const (
	StagePending StageStatus = "pending"
	StageOK      StageStatus = "ok"
	StageError   StageStatus = "error"
	StageSkipped StageStatus = "skipped"
)

type ValidationStage struct {
	Name   string
	Status StageStatus
	Detail string
}

type ModuleSummary struct {
	Domain     string
	Signals    int
	Contexts   int
	Computeds  int
	Facts      int
	Activities int
	Rules      int
	Claims     int
	Queries    int
}

type ValidationReport struct {
	Path        string
	FileName    string
	Size        int64
	StartedAt   time.Time
	FinishedAt  time.Time
	Duration    time.Duration
	Stages      []ValidationStage
	Module      *axiom.Module
	Summary     ModuleSummary
	Diagnostics []axiom.Diagnostic
	RawError    string
}

func (r ValidationReport) OK() bool {
	return r.Module != nil && len(r.Diagnostics) == 0 && r.RawError == ""
}

func ValidateFile(path string) (report ValidationReport) {
	started := time.Now()
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	report = ValidationReport{
		Path:      abs,
		FileName:  filepath.Base(abs),
		StartedAt: started,
		Stages: []ValidationStage{
			{Name: "чтение файла", Status: StagePending},
			{Name: "parse / compile / validate", Status: StagePending},
			{Name: "сборка модуля", Status: StagePending},
		},
	}
	defer func() {
		report.FinishedAt = time.Now()
		report.Duration = report.FinishedAt.Sub(started)
	}()

	info, statErr := os.Stat(abs)
	if statErr != nil {
		report.Stages[0] = ValidationStage{Name: "чтение файла", Status: StageError, Detail: statErr.Error()}
		report.Stages[1].Status = StageSkipped
		report.Stages[2].Status = StageSkipped
		report.RawError = statErr.Error()
		return report
	}
	report.Size = info.Size()

	source, readErr := os.ReadFile(abs)
	if readErr != nil {
		report.Stages[0] = ValidationStage{Name: "чтение файла", Status: StageError, Detail: readErr.Error()}
		report.Stages[1].Status = StageSkipped
		report.Stages[2].Status = StageSkipped
		report.RawError = readErr.Error()
		return report
	}
	report.Stages[0] = ValidationStage{Name: "чтение файла", Status: StageOK}

	module, loadErr := axiom.LoadModule(source)
	if loadErr != nil {
		report.Stages[1] = ValidationStage{Name: "parse / compile / validate", Status: StageError, Detail: loadErr.Error()}
		report.Stages[2].Status = StageSkipped
		var diagnostics axiom.Diagnostics
		if errors.As(loadErr, &diagnostics) {
			report.Diagnostics = append(report.Diagnostics, diagnostics...)
		} else {
			report.RawError = loadErr.Error()
		}
		return report
	}

	report.Module = module
	report.Summary = summarizeModule(module)
	report.Stages[1] = ValidationStage{Name: "parse / compile / validate", Status: StageOK}
	report.Stages[2] = ValidationStage{Name: "сборка модуля", Status: StageOK}
	return report
}

func summarizeModule(module *axiom.Module) ModuleSummary {
	if module == nil {
		return ModuleSummary{}
	}
	return ModuleSummary{
		Domain:     module.Domain,
		Signals:    len(module.Signals),
		Contexts:   len(module.Contexts),
		Computeds:  len(module.Computeds),
		Facts:      len(module.Facts),
		Activities: len(module.Activities),
		Rules:      len(module.Rules),
		Claims:     len(module.Claims),
		Queries:    len(module.Queries),
	}
}
