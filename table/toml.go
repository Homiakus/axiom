// Package table implements a TOML decision-table frontend.
package table

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Homiakus/axiom"
	"github.com/pelletier/go-toml/v2"
)

type Source struct {
	Data []byte
	Name string
}

func Bytes(data []byte) Source {
	return Source{Data: append([]byte(nil), data...), Name: "inline.toml"}
}
func Named(name string, data []byte) Source {
	return Source{Data: append([]byte(nil), data...), Name: name}
}
func Load(path string) (*axiom.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Named(path, data).CompilePlan()
}
func Parse(data []byte) (*axiom.Plan, error) { return Bytes(data).CompilePlan() }

type workflow struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}
type event struct {
	Name   string            `toml:"name"`
	Fields map[string]string `toml:"fields"`
}
type policy struct {
	Name        string `toml:"name"`
	Retry       int    `toml:"retry"`
	Timeout     string `toml:"timeout"`
	Concurrency string `toml:"concurrency"`
	Idempotency string `toml:"idempotency"`
}
type activity struct {
	Name           string            `toml:"name"`
	Require        []string          `toml:"require"`
	Input          map[string]string `toml:"input"`
	Output         map[string]string `toml:"output"`
	Effect         string            `toml:"effect"`
	Policy         string            `toml:"policy"`
	IdempotencyKey string            `toml:"idempotency_key"`
}
type transition struct {
	Name     string            `toml:"name"`
	On       string            `toml:"on"`
	Changed  string            `toml:"changed"`
	Timer    string            `toml:"timer"`
	When     []string          `toml:"when"`
	Require  []string          `toml:"require"`
	Activity string            `toml:"activity"`
	Set      map[string]string `toml:"set"`
}
type claim struct {
	Name   string   `toml:"name"`
	Always []string `toml:"always"`
}
type query struct {
	Name   string            `toml:"name"`
	Return map[string]string `toml:"return"`
}
type document struct {
	Workflow    workflow                  `toml:"workflow"`
	State       map[string]map[string]any `toml:"state"`
	Events      []event                   `toml:"event"`
	Policies    []policy                  `toml:"policy"`
	Activities  []activity                `toml:"activity"`
	Transitions []transition              `toml:"transition"`
	Claims      []claim                   `toml:"claim"`
	Queries     []query                   `toml:"query"`
}

func (s Source) CompilePlan() (*axiom.Plan, error) {
	var document document
	if err := toml.Unmarshal(s.Data, &document); err != nil {
		return nil, fmt.Errorf("table: parse TOML: %w", err)
	}
	source, err := render(document)
	if err != nil {
		return nil, err
	}
	name := s.Name
	if name == "" {
		name = "inline.toml"
	}
	plan, err := axiom.CompilePlan([]byte(source), axiom.WithSourceName(name))
	if err != nil {
		return nil, err
	}
	plan.Format = "toml"
	if document.Workflow.Version != "" {
		plan.Version = document.Workflow.Version
	}
	return plan, nil
}

func render(value document) (string, error) {
	if value.Workflow.Name == "" {
		return "", fmt.Errorf("table: workflow.name is required")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "domain %s\n\n", value.Workflow.Name)
	for _, event := range value.Events {
		fmt.Fprintf(&out, "signal %s:\n", event.Name)
		renderStringMap(&out, event.Fields, ":", 2)
		out.WriteByte('\n')
	}
	stateNames := keys(value.State)
	for _, stateName := range stateNames {
		fmt.Fprintf(&out, "context %s:\n", stateName)
		fieldNames := keys(value.State[stateName])
		for _, fieldName := range fieldNames {
			typ, defaultValue, hasDefault, err := field(value.State[stateName][fieldName])
			if err != nil {
				return "", fmt.Errorf("table: state.%s.%s: %w", stateName, fieldName, err)
			}
			fmt.Fprintf(&out, "  %s: %s", fieldName, typ)
			if hasDefault {
				fmt.Fprintf(&out, " = %s", defaultValue)
			}
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	for _, policy := range value.Policies {
		fmt.Fprintf(&out, "policy %s:\n", policy.Name)
		if policy.Retry > 0 {
			fmt.Fprintf(&out, "  retry = %d\n", policy.Retry)
		}
		if policy.Timeout != "" {
			fmt.Fprintf(&out, "  timeout = %s\n", policy.Timeout)
		}
		if policy.Concurrency != "" {
			fmt.Fprintf(&out, "  concurrency = %s\n", policy.Concurrency)
		}
		if policy.Idempotency != "" {
			fmt.Fprintf(&out, "  idempotency = %s\n", policy.Idempotency)
		}
		out.WriteByte('\n')
	}
	for _, activity := range value.Activities {
		fmt.Fprintf(&out, "activity %s:\n", activity.Name)
		renderList(&out, "require", activity.Require, 2)
		renderStringSection(&out, "input", activity.Input, " = ", 2)
		renderStringSection(&out, "output", activity.Output, ": ", 2)
		effect := activity.Effect
		if effect == "" {
			effect = "none"
		}
		fmt.Fprintf(&out, "  effect: %s\n", effect)
		if activity.IdempotencyKey != "" {
			fmt.Fprintf(&out, "  idempotencyKey: %s\n", activity.IdempotencyKey)
		}
		if activity.Policy != "" {
			fmt.Fprintf(&out, "  policy: %s\n", activity.Policy)
		}
		out.WriteByte('\n')
	}
	for _, transition := range value.Transitions {
		fmt.Fprintf(&out, "rule %s:\n", transition.Name)
		switch {
		case transition.On != "":
			fmt.Fprintf(&out, "  on %s\n", transition.On)
		case transition.Changed != "":
			fmt.Fprintf(&out, "  on changed(%s)\n", transition.Changed)
		case transition.Timer != "":
			fmt.Fprintf(&out, "  on timer(%s)\n", transition.Timer)
		}
		renderList(&out, "when", transition.When, 2)
		renderList(&out, "require", transition.Require, 2)
		if transition.Activity != "" {
			fmt.Fprintf(&out, "  run: %s\n", transition.Activity)
		}
		renderStringSection(&out, "write", transition.Set, " = ", 2)
		out.WriteByte('\n')
	}
	for _, claim := range value.Claims {
		fmt.Fprintf(&out, "claim %s:\n", claim.Name)
		renderList(&out, "always", claim.Always, 2)
		out.WriteByte('\n')
	}
	for _, query := range value.Queries {
		fmt.Fprintf(&out, "query %s:\n", query.Name)
		renderStringSection(&out, "return", query.Return, " = ", 2)
		out.WriteByte('\n')
	}
	return out.String(), nil
}

func field(value any) (string, string, bool, error) {
	if text, ok := value.(string); ok {
		if isType(text) {
			return text, "", false, nil
		}
		data, _ := json.Marshal(text)
		return "String", string(data), true, nil
	}
	if table, ok := value.(map[string]any); ok {
		typ, _ := table["type"].(string)
		if typ == "" {
			return "", "", false, fmt.Errorf("inline table requires type")
		}
		defaultValue, exists := table["default"]
		if !exists {
			return typ, "", false, nil
		}
		encoded, err := literal(defaultValue)
		return typ, encoded, true, err
	}
	encoded, err := literal(value)
	switch value.(type) {
	case bool:
		return "Bool", encoded, true, err
	case int64, int32, int:
		return "Int", encoded, true, err
	case float64, float32:
		return "Float", encoded, true, err
	}
	return "Any", encoded, true, err
}
func literal(value any) (string, error) { data, err := json.Marshal(value); return string(data), err }
func isType(value string) bool {
	base := strings.TrimSuffix(value, "?")
	return base == "String" || base == "Int" || base == "Float" || base == "Bool" || base == "Time" || base == "Duration" || base == "Object" || base == "Any" || strings.HasPrefix(base, "List<") || strings.HasPrefix(base, "Map<")
}
func keys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
func renderStringMap(out *strings.Builder, values map[string]string, separator string, indent int) {
	for _, key := range keys(values) {
		fmt.Fprintf(out, "%s%s%s %s\n", strings.Repeat(" ", indent), key, separator, values[key])
	}
}
func renderList(out *strings.Builder, name string, values []string, indent int) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "%s%s:\n", strings.Repeat(" ", indent), name)
	for _, value := range values {
		fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", indent+2), value)
	}
}
func renderStringSection(out *strings.Builder, name string, values map[string]string, separator string, indent int) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "%s%s:\n", strings.Repeat(" ", indent), name)
	for _, key := range keys(values) {
		fmt.Fprintf(out, "%s%s%s%s\n", strings.Repeat(" ", indent+2), key, separator, values[key])
	}
}
