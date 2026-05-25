package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var graphIDCleanRe = regexp.MustCompile(`[^A-Za-z0-9_.:-]+`)

func buildProjectGraph(m ProjectModel, sim SimulationReport) ProjectGraph {
	return buildFilteredProjectGraph(m, sim, "", "", "")
}

func buildFilteredProjectGraph(m ProjectModel, sim SimulationReport, filter, focus, selected string) ProjectGraph {
	if filter == "" {
		filter = "all"
	}
	g := graphBuilder{
		model:         m,
		graph:         ProjectGraph{Filter: filter, Focus: focus, Selected: selected},
		nodeIndex:     map[string]int{},
		edgeSeen:      map[string]bool{},
		activeRules:   map[string]string{},
		activeActions: map[string]bool{},
		activeWrites:  map[string]bool{},
	}
	g.ingestSimulation(sim)
	g.addProjectNodes()
	g.addRuleEdges()
	g.addSafetyEdges()
	g.applySelection()
	g.applyFilter()
	g.layout()
	g.buildTimeline()
	g.graph.StateFields = buildStateFieldInfo(m)
	return g.graph
}

type graphBuilder struct {
	model         ProjectModel
	graph         ProjectGraph
	nodeIndex     map[string]int
	edgeSeen      map[string]bool
	activeRules   map[string]string
	activeActions map[string]bool
	activeWrites  map[string]bool
}

func (g *graphBuilder) ingestSimulation(sim SimulationReport) {
	for _, step := range sim.Steps {
		if step.Verdict != "" {
			g.activeRules[step.Rule] = step.Verdict
		}
		for _, action := range step.Actions {
			if name := actionNameFromCall(action); name != "" {
				g.activeActions[name] = true
			}
		}
		for _, write := range step.Writes {
			g.activeWrites[write.Target] = true
		}
	}
}

func (g *graphBuilder) addProjectNodes() {
	eventNames := sortedBlockNames(g.model.Events)
	for _, name := range eventNames {
		g.addNode(GraphNode{ID: graphNodeID("event", name), Kind: "event", Label: name, Layer: 0, URL: blockURL("event", name)})
	}
	condNames := sortedBlockNames(g.model.Conditions)
	for _, name := range condNames {
		g.addNode(GraphNode{ID: graphNodeID("condition", name), Kind: "condition", Label: name, Layer: 1, URL: blockURL("condition", name)})
	}
	for _, name := range sortedActionNames(g.model.Actions) {
		action := g.model.Actions[name]
		status := ""
		if g.activeActions[name] {
			status = "SCHEDULED"
		}
		detail := "inferred"
		if action.Declared {
			detail = "declared"
		}
		g.addNode(GraphNode{ID: graphNodeID("action", name), Kind: "action", Label: name + "()", Detail: detail, Layer: 3, URL: "/?action=" + name + "#workspace", Status: status})
	}
	ruleNames := sortedRuleNames(g.model.Rules)
	for _, name := range ruleNames {
		status := g.activeRules[name]
		g.addNode(GraphNode{ID: graphNodeID("rule", name), Kind: "rule", Label: name, Detail: "rule", Layer: 2, URL: blockURL("rule", name), Status: status})
	}
	for _, field := range sortedStateFields(g.model) {
		status := ""
		if g.activeWrites[field] {
			status = "WRITTEN"
		}
		g.addNode(GraphNode{ID: graphNodeID("state", field), Kind: "state", Label: field, Detail: "context", Layer: 4, URL: "#state-inspector", Status: status})
	}
	for _, name := range sortedBlockNames(g.model.Always) {
		g.addNode(GraphNode{ID: graphNodeID("safety", name), Kind: "safety", Label: name, Detail: "always", Layer: 5, URL: blockURL("always", name)})
	}
}

func (g *graphBuilder) addRuleEdges() {
	for _, name := range sortedRuleNames(g.model.Rules) {
		r := g.model.Rules[name]
		ruleID := graphNodeID("rule", name)
		if r.OnEvent != "" {
			for _, ev := range splitRefs(r.OnEvent) {
				g.addEdge(graphNodeID("event", ev), ruleID, "trigger", "on")
			}
		}
		if r.Every != "" {
			timerID := graphNodeID("event", "timer:"+r.Every)
			g.addNode(GraphNode{ID: timerID, Kind: "event", Label: "timer " + r.Every, Layer: 0})
			g.addEdge(timerID, ruleID, "trigger", "every")
		}
		for _, cond := range r.WhenLines {
			if _, ok := g.model.Events[strings.TrimSpace(cond)]; ok {
				g.addEdge(graphNodeID("event", strings.TrimSpace(cond)), ruleID, "trigger", "when")
				continue
			}
			condName := conditionRefName(g.model, cond)
			if condName != "" {
				g.addEdge(graphNodeID("condition", condName), ruleID, "condition", "when")
				for _, read := range stateRefsFromConditionBlock(g.model.Conditions[condName]) {
					g.addEdge(graphNodeID("state", read), graphNodeID("condition", condName), "read", "reads")
				}
				continue
			}
			for _, read := range uniqueMatches(stateRefRe, cond) {
				g.addEdge(graphNodeID("state", read), ruleID, "read", "reads")
			}
		}
		for _, read := range r.Reads {
			g.addEdge(graphNodeID("state", read), ruleID, "read", "reads")
		}
		for _, call := range extractActionCalls(r.DoLines) {
			g.addEdge(ruleID, graphNodeID("action", call.Name), "activity", "run")
			for _, input := range call.Args {
				for _, read := range uniqueMatches(stateRefRe, input) {
					g.addEdge(graphNodeID("state", read), graphNodeID("action", call.Name), "input", "input")
				}
			}
		}
		for _, write := range r.Writes {
			targetID := graphNodeID("state", write)
			g.addEdge(ruleID, targetID, "write", "write")
			for _, call := range extractActionCalls(r.DoLines) {
				g.addEdge(graphNodeID("action", call.Name), targetID, "output", "output")
			}
		}
	}
}

func (g *graphBuilder) addSafetyEdges() {
	for _, name := range sortedBlockNames(g.model.Always) {
		b := g.model.Always[name]
		safetyID := graphNodeID("safety", name)
		for _, ref := range stateRefsFromConditionBlock(b) {
			g.addEdge(safetyID, graphNodeID("state", ref), "protects", "protects")
		}
	}
}

func (g *graphBuilder) addNode(n GraphNode) {
	if n.ID == "" {
		return
	}
	if idx, ok := g.nodeIndex[n.ID]; ok {
		old := &g.graph.Nodes[idx]
		if old.Status == "" {
			old.Status = n.Status
		}
		if old.URL == "" {
			old.URL = n.URL
		}
		return
	}
	g.nodeIndex[n.ID] = len(g.graph.Nodes)
	g.graph.Nodes = append(g.graph.Nodes, n)
}

func (g *graphBuilder) addEdge(from, to, kind, label string) {
	if from == "" || to == "" || from == to {
		return
	}
	id := graphSafeID("edge:" + from + ":" + to + ":" + kind)
	if g.edgeSeen[id] {
		return
	}
	g.edgeSeen[id] = true
	g.graph.Edges = append(g.graph.Edges, GraphEdge{ID: id, From: from, To: to, Kind: kind, Label: label})
}

func (g *graphBuilder) applySelection() {
	selected := g.graph.Selected
	if selected == "" {
		selected = selectedNodeID(g.model, g.graph.Focus)
	}
	focus := g.graph.Focus
	if focus == "" {
		focus = selected
	}
	g.graph.Selected = selected
	g.graph.Focus = focus
	for i := range g.graph.Nodes {
		if g.graph.Nodes[i].ID == selected {
			g.graph.Nodes[i].Selected = true
		}
		if g.graph.Nodes[i].ID == focus {
			g.graph.Nodes[i].Focused = true
		}
	}
}

func (g *graphBuilder) applyFilter() {
	keep := map[string]bool{}
	for _, node := range g.graph.Nodes {
		switch g.graph.Filter {
		case "selected":
			if node.Selected || node.Focused {
				keep[node.ID] = true
			}
		case "safety":
			if node.Kind == "safety" || node.Kind == "state" {
				keep[node.ID] = true
			}
		case "runnable":
			if node.Status != "" || node.Kind == "event" {
				keep[node.ID] = true
			}
		case "writes":
			if node.Kind == "state" || node.Status == "WRITTEN" {
				keep[node.ID] = true
			}
		default:
			keep[node.ID] = true
		}
	}
	if g.graph.Filter == "selected" && len(keep) > 0 {
		for changed := true; changed; {
			changed = false
			for _, edge := range g.graph.Edges {
				if keep[edge.From] || keep[edge.To] {
					if !keep[edge.From] {
						keep[edge.From] = true
						changed = true
					}
					if !keep[edge.To] {
						keep[edge.To] = true
						changed = true
					}
				}
			}
		}
	}
	for _, edge := range g.graph.Edges {
		if edge.Kind == "protects" && g.graph.Filter == "safety" {
			keep[edge.From] = true
			keep[edge.To] = true
		}
		if edge.Kind == "write" && g.graph.Filter == "writes" {
			keep[edge.From] = true
			keep[edge.To] = true
		}
	}
	if g.graph.Selected != "" {
		keep[g.graph.Selected] = true
	}
	nodes := g.graph.Nodes[:0]
	for _, node := range g.graph.Nodes {
		if keep[node.ID] {
			nodes = append(nodes, node)
		}
	}
	g.graph.Nodes = nodes
	edges := g.graph.Edges[:0]
	for _, edge := range g.graph.Edges {
		if keep[edge.From] && keep[edge.To] {
			if g.graph.Filter == "runnable" {
				edge.Active = true
			}
			edges = append(edges, edge)
		}
	}
	g.graph.Edges = edges
}

func (g *graphBuilder) layout() {
	byLayer := map[int][]int{}
	maxLayer := 0
	for i, node := range g.graph.Nodes {
		byLayer[node.Layer] = append(byLayer[node.Layer], i)
		if node.Layer > maxLayer {
			maxLayer = node.Layer
		}
	}
	for layer, indexes := range byLayer {
		sort.Slice(indexes, func(i, j int) bool {
			return g.graph.Nodes[indexes[i]].Label < g.graph.Nodes[indexes[j]].Label
		})
		for pos, idx := range indexes {
			g.graph.Nodes[idx].X = 100 + layer*220
			g.graph.Nodes[idx].Y = 70 + pos*92
		}
	}
	maxY := 420
	for _, node := range g.graph.Nodes {
		if node.Y+80 > maxY {
			maxY = node.Y + 80
		}
	}
	g.graph.Width = 220 + maxLayer*220
	if g.graph.Width < 980 {
		g.graph.Width = 980
	}
	g.graph.Height = maxY
}

func (g *graphBuilder) buildTimeline() {
	nodes := append([]GraphNode(nil), g.graph.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Layer == nodes[j].Layer {
			return nodes[i].Label < nodes[j].Label
		}
		return nodes[i].Layer < nodes[j].Layer
	})
	for _, node := range nodes {
		g.graph.Timeline = append(g.graph.Timeline, GraphTimelineItem{
			NodeID: node.ID,
			Kind:   node.Kind,
			Label:  node.Label,
			Detail: node.Detail,
			Status: node.Status,
			URL:    node.URL,
		})
	}
}

func buildStateFieldInfo(m ProjectModel) []StateFieldInfo {
	fields := map[string]*StateFieldInfo{}
	ensure := func(name string) *StateFieldInfo {
		if fields[name] == nil {
			fields[name] = &StateFieldInfo{Name: name}
		}
		return fields[name]
	}
	for _, rn := range sortedRuleNames(m.Rules) {
		r := m.Rules[rn]
		for _, read := range r.Reads {
			ensure(read).ReadBy = appendUnique(ensure(read).ReadBy, rn)
		}
		for _, line := range r.WhenLines {
			for _, read := range uniqueMatches(stateRefRe, line) {
				ensure(read).ReadBy = appendUnique(ensure(read).ReadBy, rn)
			}
			if condName := conditionRefName(m, line); condName != "" {
				for _, read := range stateRefsFromConditionBlock(m.Conditions[condName]) {
					ensure(read).ReadBy = appendUnique(ensure(read).ReadBy, rn)
				}
			}
		}
		for _, write := range r.Writes {
			ensure(write).WrittenBy = appendUnique(ensure(write).WrittenBy, rn)
		}
	}
	for _, name := range sortedBlockNames(m.Always) {
		for _, ref := range stateRefsFromConditionBlock(m.Always[name]) {
			ensure(ref).ProtectedBy = appendUnique(ensure(ref).ProtectedBy, name)
		}
	}
	out := make([]StateFieldInfo, 0, len(fields))
	for _, f := range fields {
		sort.Strings(f.ReadBy)
		sort.Strings(f.WrittenBy)
		sort.Strings(f.ProtectedBy)
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func selectedNodeID(m ProjectModel, selected string) string {
	if selected == "" {
		return ""
	}
	for _, b := range m.Blocks {
		if b.ID == selected {
			switch b.Kind {
			case "rule":
				return graphNodeID("rule", b.Name)
			case "event", "signal":
				return graphNodeID("event", b.Name)
			case "condition", "fact":
				return graphNodeID("condition", b.Name)
			case "function", "action":
				return graphNodeID("action", b.Name)
			case "always", "claim":
				return graphNodeID("safety", b.Name)
			}
		}
	}
	if strings.HasPrefix(selected, "graph:") {
		return strings.TrimPrefix(selected, "graph:")
	}
	return selected
}

func graphNodeID(kind, name string) string {
	return graphSafeID(kind + ":" + name)
}

func graphSafeID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "_")
	return graphIDCleanRe.ReplaceAllString(s, "_")
}

func blockURL(kind, name string) string {
	switch kind {
	case "event", "signal":
		return "/?id=event:" + name + "#workspace"
	case "condition", "fact":
		return "/?id=condition:" + name + "#workspace"
	case "rule":
		return "/?id=rule:" + name + "#workspace"
	case "always", "claim":
		return "/?id=always:" + name + "#workspace"
	case "function", "action":
		return "/?action=" + name + "#workspace"
	default:
		return "#workspace"
	}
}

func conditionRefName(m ProjectModel, cond string) string {
	c := strings.TrimSpace(cond)
	c = strings.TrimPrefix(c, "not ")
	c = strings.TrimSpace(c)
	c = strings.TrimSuffix(c, "()")
	if _, ok := m.Conditions[c]; ok {
		return c
	}
	return ""
}

func stateRefsFromConditionBlock(b Block) []string {
	return uniqueMatches(stateRefRe, strings.Join(b.Body, "\n"))
}

func sortedStateFields(m ProjectModel) []string {
	seen := map[string]bool{}
	for _, r := range m.Rules {
		for _, ref := range r.Reads {
			seen[ref] = true
		}
		for _, ref := range r.Writes {
			seen[ref] = true
		}
		for _, line := range r.WhenLines {
			for _, ref := range uniqueMatches(stateRefRe, line) {
				seen[ref] = true
			}
			if condName := conditionRefName(m, line); condName != "" {
				for _, ref := range stateRefsFromConditionBlock(m.Conditions[condName]) {
					seen[ref] = true
				}
			}
		}
	}
	for _, b := range m.Always {
		for _, ref := range stateRefsFromConditionBlock(b) {
			seen[ref] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedRuleNames(rules map[string]RuleInfo) []string {
	out := make([]string, 0, len(rules))
	for name := range rules {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedActionNames(actions map[string]ActionInfo) []string {
	out := make([]string, 0, len(actions))
	for name := range actions {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedBlockNames(blocks map[string]Block) []string {
	out := make([]string, 0, len(blocks))
	for name := range blocks {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func splitRefs(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '|' || r == '&' || r == '(' || r == ')' || r == ' '
	})
	out := []string{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" && !strings.Contains(f, ":") {
			out = appendUnique(out, f)
		}
	}
	return out
}

func actionNameFromCall(call string) string {
	if mm := actionCallRe.FindStringSubmatch(call); len(mm) > 1 {
		return mm[1]
	}
	call = strings.TrimSpace(call)
	if idx := strings.Index(call, "("); idx > 0 {
		return strings.TrimSpace(call[:idx])
	}
	return ""
}

func graphEdgePath(from, to GraphNode) string {
	return fmt.Sprintf("M%d,%d C%d,%d %d,%d %d,%d", from.X+76, from.Y, from.X+150, from.Y, to.X-150, to.Y, to.X-76, to.Y)
}
