package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const studioTRIZSource = `system Switcher

state System:
  running: Bool = false

event Start(zone: Int)

profile critical:
  timeout: 10s
  retry: 0
  once
  idempotent

function TurnOn(zone: Int) -> { ok: Bool }

rule ApplyTurnOn when:
  Start
do critical:
  result = TurnOn(event.zone)
then:
  set System.running = result.ok

view Dashboard:
  running = System.running
`

func TestParseProjectUsesCompilerBackedTRIZModel(t *testing.T) {
	m := parseProject(studioTRIZSource, "switcher.axm")
	if !m.CompileOK {
		t.Fatalf("expected compiler-backed model to compile: %#v", m.CompilerDiagnostics)
	}
	if m.Format != "TRIZ DSL" {
		t.Fatalf("format = %q, want TRIZ DSL", m.Format)
	}
	if !strings.Contains(m.NormalizedSource, "domain Switcher") {
		t.Fatalf("normalized source missing domain:\n%s", m.NormalizedSource)
	}
	if !strings.Contains(m.NormalizedSource, "activity TurnOn:") {
		t.Fatalf("normalized source missing activity:\n%s", m.NormalizedSource)
	}
}

func TestGenerateGoStubsUsesCompilerActivities(t *testing.T) {
	m := parseProject(studioTRIZSource, "switcher.axm")
	stubs := generateGoStubs(m)
	if !strings.Contains(stubs, "type TurnOnInput struct") {
		t.Fatalf("stubs missing typed input:\n%s", stubs)
	}
	if !strings.Contains(stubs, "type TurnOnOutput struct") {
		t.Fatalf("stubs missing typed output:\n%s", stubs)
	}
}

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handleHealth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if strings.TrimSpace(rr.Body.String()) != `{"ok":true}` {
		t.Fatalf("body = %q", rr.Body.String())
	}
}

func TestIndexRendersCompilerBackedNormalizedView(t *testing.T) {
	state.mu.Lock()
	oldSource, oldPath, oldMsg := state.source, state.path, state.msg
	state.source = studioTRIZSource
	state.path = "switcher.axm"
	state.msg = ""
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.source, state.path, state.msg = oldSource, oldPath, oldMsg
		state.mu.Unlock()
	}()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handleIndex(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	for _, want := range []string{"TRIZ DSL", "Normalized Axiom v0", "domain Switcher", "OK"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index response missing %q", want)
		}
	}
}

func TestProjectGraphBuildsExpectedNodesAndEdges(t *testing.T) {
	m := parseProject(sampleSource, "sample.axm")
	g := buildProjectGraph(m, SimulationReport{})
	for _, id := range []string{
		graphNodeID("event", "PHMeasurementDue"),
		graphNodeID("condition", "CanUseHardware"),
		graphNodeID("rule", "MeasurePH"),
		graphNodeID("action", "MeasurePH"),
		graphNodeID("state", "Zone[zone].ph"),
		graphNodeID("safety", "emergencyStopDisablesActuators"),
	} {
		if !graphHasNode(g, id) {
			t.Fatalf("graph missing node %s", id)
		}
	}
	for _, edge := range []struct {
		from string
		to   string
		kind string
	}{
		{graphNodeID("event", "PHMeasurementDue"), graphNodeID("rule", "MeasurePH"), "trigger"},
		{graphNodeID("condition", "CanUseHardware"), graphNodeID("rule", "MeasurePH"), "condition"},
		{graphNodeID("rule", "MeasurePH"), graphNodeID("action", "MeasurePH"), "activity"},
		{graphNodeID("rule", "MeasurePH"), graphNodeID("state", "Zone[zone].ph"), "write"},
		{graphNodeID("safety", "emergencyStopDisablesActuators"), graphNodeID("state", "Safety.estop"), "protects"},
	} {
		if !graphHasEdge(g, edge.from, edge.to, edge.kind) {
			t.Fatalf("graph missing %s edge %s -> %s", edge.kind, edge.from, edge.to)
		}
	}
}

func TestSelectedGraphFilterKeepsRuleNeighborhood(t *testing.T) {
	m := parseProject(sampleSource, "sample.axm")
	g := buildFilteredProjectGraph(m, SimulationReport{}, "selected", "", graphNodeID("rule", "MeasurePH"))
	for _, id := range []string{
		graphNodeID("event", "PHMeasurementDue"),
		graphNodeID("condition", "CanUseHardware"),
		graphNodeID("rule", "MeasurePH"),
		graphNodeID("action", "MeasurePH"),
		graphNodeID("state", "Zone[zone].ph"),
	} {
		if !graphHasNode(g, id) {
			t.Fatalf("selected graph missing neighborhood node %s", id)
		}
	}
	if !graphHasNode(g, graphNodeID("rule", "MeasurePH")) {
		t.Fatalf("selected graph dropped selected rule")
	}
}

func TestGraphIncludesInferredAndDeclaredActionWriteEdges(t *testing.T) {
	m := parseProject(studioTRIZSource, "switcher.axm")
	g := buildProjectGraph(m, SimulationReport{})
	if !graphHasNode(g, graphNodeID("action", "TurnOn")) {
		t.Fatalf("graph missing declared action TurnOn")
	}
	if !graphHasEdge(g, graphNodeID("rule", "ApplyTurnOn"), graphNodeID("action", "TurnOn"), "activity") {
		t.Fatalf("graph missing declared action edge")
	}
	if !graphHasEdge(g, graphNodeID("action", "TurnOn"), graphNodeID("state", "System.running"), "output") {
		t.Fatalf("graph missing action output write edge")
	}
}

func TestGraphSimulationStatusDoesNotMutateModel(t *testing.T) {
	m := parseProject(sampleSource, "sample.axm")
	baseStatus := graphNodeStatus(m.Graph, graphNodeID("rule", "MeasurePH"))
	sim := simulateSystemWithMocks(m, "PHMeasurementDue", defaultAssumptions, MockOutputs{"MeasurePH": {"value": "7.4"}})
	g := buildProjectGraph(m, sim)
	if got := graphNodeStatus(g, graphNodeID("rule", "MeasurePH")); got != "RUNNABLE" {
		t.Fatalf("sim graph status = %q, want RUNNABLE", got)
	}
	if got := graphNodeStatus(g, graphNodeID("state", "Zone[zone].ph")); got != "WRITTEN" {
		t.Fatalf("sim write status = %q, want WRITTEN", got)
	}
	if got := graphNodeStatus(m.Graph, graphNodeID("rule", "MeasurePH")); got != baseStatus {
		t.Fatalf("base model graph mutated: before %q after %q", baseStatus, got)
	}
	foundMock := false
	for _, step := range sim.Steps {
		for _, write := range step.Writes {
			if write.Target == "Zone[zone].ph" && write.Value == "7.4" {
				foundMock = true
			}
		}
	}
	if !foundMock {
		t.Fatalf("simulation did not apply mock result to Zone[zone].ph: %#v", sim.Steps)
	}
}

func TestIndexRendersGraphAndMobileFallback(t *testing.T) {
	state.mu.Lock()
	oldSource, oldPath, oldMsg := state.source, state.path, state.msg
	state.source = sampleSource
	state.path = "sample.axm"
	state.msg = ""
	invalidateProjectLocked()
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.source, state.path, state.msg = oldSource, oldPath, oldMsg
		invalidateProjectLocked()
		state.mu.Unlock()
	}()

	req := httptest.NewRequest(http.MethodGet, "/?id=rule:MeasurePH&graph=selected", nil)
	rr := httptest.NewRecorder()
	handleIndex(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	for _, want := range []string{"crfg-svg", "graph-mobile", "State inspector", `href="/?action=MeasurePH#workspace"`, "mockOutputs"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index response missing %q", want)
		}
	}
}

func graphHasNode(g ProjectGraph, id string) bool {
	for _, node := range g.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func graphHasEdge(g ProjectGraph, from, to, kind string) bool {
	for _, edge := range g.Edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return true
		}
	}
	return false
}

func graphNodeStatus(g ProjectGraph, id string) string {
	for _, node := range g.Nodes {
		if node.ID == id {
			return node.Status
		}
	}
	return ""
}

func BenchmarkParseProjectCompilerBackedTRIZ(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m := parseProject(studioTRIZSource, "switcher.axm")
		if !m.CompileOK {
			b.Fatalf("compile failed: %#v", m.CompilerDiagnostics)
		}
	}
}
