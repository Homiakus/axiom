package diagram

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/internal/lang"
)

// ToMermaidFlowchart converts an Axiom compiled Module or Plan into a visual Mermaid flowchart graph.
func ToMermaidFlowchart(module *axiom.Module) string {
	if module == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("flowchart TD\n")

	// Render Signals Subgraph
	if len(module.Signals) > 0 {
		sb.WriteString("  subgraph Signals [Triggers & Events]\n")
		for _, name := range sortedKeys(module.Signals) {
			sb.WriteString(fmt.Sprintf("    sig_%s([\"⚡ %s\"])\n", sanitizeID(name), name))
		}
		sb.WriteString("  end\n\n")
	}

	// Render Rules Subgraph
	if len(module.Rules) > 0 {
		sb.WriteString("  subgraph Rules [Transition Rules]\n")
		for _, name := range sortedKeys(module.Rules) {
			sb.WriteString(fmt.Sprintf("    rule_%s[\"⚙️ Rule: %s\"]\n", sanitizeID(name), name))
		}
		sb.WriteString("  end\n\n")
	}

	// Render Activities Subgraph
	if len(module.Activities) > 0 {
		sb.WriteString("  subgraph Activities [External Operations]\n")
		for _, name := range sortedKeys(module.Activities) {
			sb.WriteString(fmt.Sprintf("    act_%s[[\"🛠️ Activity: %s\"]]\n", sanitizeID(name), name))
		}
		sb.WriteString("  end\n\n")
	}

	// Render Claims Subgraph
	if len(module.Claims) > 0 {
		sb.WriteString("  subgraph Invariants [Claims & Constraints]\n")
		for _, name := range sortedKeys(module.Claims) {
			sb.WriteString(fmt.Sprintf("    claim_%s{{\"🛡️ Claim: %s\"}}\n", sanitizeID(name), name))
		}
		sb.WriteString("  end\n\n")
	}

	// Connect Signals -> Rules -> Activities & State Writes
	for _, ruleName := range sortedKeys(module.Rules) {
		rule := module.Rules[ruleName]
		rID := sanitizeID(ruleName)

		for _, trigger := range rule.Triggers {
			if trigger.Kind == lang.TriggerSignal {
				sID := sanitizeID(trigger.Name)
				condLabel := ""
				if len(rule.When) > 0 {
					condLabel = fmt.Sprintf(" -- \"when: %d guard(s)\" -->", len(rule.When))
				} else {
					condLabel = " -->"
				}
				sb.WriteString(fmt.Sprintf("  sig_%s%s rule_%s\n", sID, condLabel, rID))
			}
		}

		// Rule -> Activity
		if rule.Run != "" {
			aID := sanitizeID(rule.Run)
			sb.WriteString(fmt.Sprintf("  rule_%s ==>|\"runs\"| act_%s\n", rID, aID))
		}

		// Rule -> State Writes
		if len(rule.Writes) > 0 {
			for _, write := range rule.Writes {
				targetID := sanitizeID(write.Name)
				sb.WriteString(fmt.Sprintf("  rule_%s -. \"updates\" .-> state_%s[(\"%s\")]\n", rID, targetID, write.Name))
			}
		}
	}

	return sb.String()
}

// HistoryToMermaidSequence converts runtime execution history into a Mermaid sequence diagram.
func HistoryToMermaidSequence(history []axiom.HistoryEntry) string {
	var sb strings.Builder
	sb.WriteString("sequenceDiagram\n")
	sb.WriteString("  autonumber\n")
	sb.WriteString("  actor Client\n")
	sb.WriteString("  participant Engine\n")
	sb.WriteString("  participant State\n")
	sb.WriteString("  participant Activity\n\n")

	for _, entry := range history {
		name := ""
		if entry.Payload != nil {
			if s, ok := entry.Payload["signal"].(string); ok {
				name = s
			} else if r, ok := entry.Payload["rule"].(string); ok {
				name = r
			} else if a, ok := entry.Payload["activity"].(string); ok {
				name = a
			} else if t, ok := entry.Payload["target"].(string); ok {
				name = t
			}
		}

		switch entry.Type {
		case "SignalReceived", "SignalDispatched", "EventHandled", "signal":
			if name != "" {
				sb.WriteString(fmt.Sprintf("  Client->>Engine: Signal %s\n", name))
			} else {
				sb.WriteString("  Client->>Engine: Signal Received\n")
			}
		case "ActivityScheduled", "activity_scheduled":
			if name != "" {
				sb.WriteString(fmt.Sprintf("  Engine->>Activity: Schedule %s\n", name))
			}
		case "ActivityCompleted", "EffectCompleted", "activity_completed":
			if name != "" {
				sb.WriteString(fmt.Sprintf("  Activity-->>Engine: Output from %s\n", name))
			}
		case "WriteApplied", "StateUpdated", "rule_fired":
			if name != "" {
				sb.WriteString(fmt.Sprintf("  Engine->>State: Rule %s applied writes\n", name))
			}
		}
	}

	return sb.String()
}

// ToPlantUML converts an Axiom compiled Module into PlantUML state diagram syntax.
func ToPlantUML(module *axiom.Module) string {
	if module == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("@startuml\n")
	sb.WriteString("skinparam state {\n")
	sb.WriteString("  StartColor #2C3E50\n")
	sb.WriteString("  EndColor #2C3E50\n")
	sb.WriteString("  BackgroundColor #ECF0F1\n")
	sb.WriteString("  BorderColor #2980B9\n")
	sb.WriteString("}\n\n")

	for _, ruleName := range sortedKeys(module.Rules) {
		rule := module.Rules[ruleName]
		for _, trigger := range rule.Triggers {
			if trigger.Kind == lang.TriggerSignal {
				actLabel := ""
				if rule.Run != "" {
					actLabel = "\\n[runs " + rule.Run + "]"
				}
				sb.WriteString(fmt.Sprintf("[%s] --> [%s] : %s%s\n", trigger.Name, ruleName, trigger.Name, actLabel))
			}
		}
	}

	sb.WriteString("\n@enduml\n")
	return sb.String()
}

func sanitizeID(name string) string {
	r := strings.NewReplacer(".", "_", "-", "_", " ", "_", ":", "_")
	return r.Replace(name)
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
