package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func withSecurityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	fmt.Fprint(w, `{"ok":true}`)
}

type PageData struct {
	Model       ProjectModel
	Selected    Block
	Rule        RuleInfo
	HasRule     bool
	Action      ActionInfo
	HasAction   bool
	Graph       ProjectGraph
	GraphFilter string
	FocusNode   string
	Msg         string
	Assumptions string
	EventName   string
	MockOutputs string
	Simulation  []SimLine
	SimReport   SimulationReport
}

type SimLine struct{ Rule, Verdict, Why string }

func handleIndex(w http.ResponseWriter, r *http.Request) {
	m, msg := currentProject(true)
	assumptions := r.URL.Query().Get("assumptions")
	if assumptions == "" {
		assumptions = defaultAssumptions
	}
	eventName := r.URL.Query().Get("event")
	mockOutputs := r.URL.Query().Get("mockOutputs")
	graphFilter := r.URL.Query().Get("graph")
	focusNode := r.URL.Query().Get("focus")
	sel, _ := selectedBlock(m, r.URL.Query().Get("id"))
	rule, hasRule := selectedRule(m, sel.ID)
	action, hasAction := selectedAction(m, r.URL.Query().Get("action"))
	if !hasAction && (sel.Kind == "function" || sel.Kind == "action") {
		action, hasAction = selectedAction(m, sel.Name)
	}
	selectedNode := selectedNodeID(m, sel.ID)
	if hasAction {
		selectedNode = graphNodeID("action", action.Name)
	}
	mocks, mockErr := parseMockOutputs(mockOutputs)
	if mockErr != nil {
		if msg != "" {
			msg += " "
		}
		msg += "Mock outputs JSON error: " + mockErr.Error()
	}
	data := PageData{
		Model:       m,
		Selected:    sel,
		Rule:        rule,
		HasRule:     hasRule,
		Action:      action,
		HasAction:   hasAction,
		GraphFilter: graphFilter,
		FocusNode:   focusNode,
		Msg:         msg,
		Assumptions: assumptions,
		EventName:   eventName,
		MockOutputs: mockOutputs,
	}
	if eventName != "" {
		data.Simulation = simulateEvent(m, eventName, assumptions)
		data.SimReport = simulateSystemWithMocks(m, eventName, assumptions, mocks)
	}
	data.Graph = buildFilteredProjectGraph(m, data.SimReport, graphFilter, focusNode, selectedNode)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func handleLoad(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	path := r.FormValue("path")
	b, err := os.ReadFile(path)
	state.mu.Lock()
	defer state.mu.Unlock()
	if err != nil {
		state.msg = "Ошибка загрузки: " + err.Error()
	} else {
		state.source = string(b)
		state.path = path
		invalidateProjectLocked()
		state.msg = "Файл загружен: " + path
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f, hdr, err := r.FormFile("file")
	state.mu.Lock()
	defer state.mu.Unlock()
	if err != nil {
		state.msg = "Upload error: " + err.Error()
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	state.source = string(b)
	state.path = hdr.Filename
	invalidateProjectLocked()
	state.msg = "Загружено из браузера: " + hdr.Filename
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	state.mu.Lock()
	state.source = r.FormValue("source")
	invalidateProjectLocked()
	state.msg = "Source обновлён в памяти."
	state.mu.Unlock()
	http.Redirect(w, r, "/?id="+r.FormValue("selected"), http.StatusSeeOther)
}

func handleSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	path := r.FormValue("path")
	source := r.FormValue("source")
	if path == "" {
		path = "axiom_rules_saved.axm"
	}
	err := os.WriteFile(path, []byte(source), 0644)
	state.mu.Lock()
	defer state.mu.Unlock()
	if err != nil {
		state.msg = "Save error: " + err.Error()
	} else {
		state.source = source
		state.path = path
		invalidateProjectLocked()
		state.msg = "Сохранено: " + path
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleStubs(w http.ResponseWriter, r *http.Request) {
	m, _ := currentProject(false)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="actions_stubs.go"`)
	fmt.Fprint(w, generateGoStubs(m))
}

func handleReport(w http.ResponseWriter, r *http.Request) {
	m, _ := currentProject(false)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="axiom_rule_report.md"`)
	fmt.Fprint(w, generateReport(m))
}

func handleDownloadSource(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	src := state.source
	path := state.path
	state.mu.Unlock()
	name := filepath.Base(path)
	if name == "." || name == "/" || name == "" {
		name = "rules.axm"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	fmt.Fprint(w, src)
}

func handleZip(w http.ResponseWriter, r *http.Request) {
	zipPath, err := makeProjectZip()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.ServeFile(w, r, zipPath)
}
