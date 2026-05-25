package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"axiom/pkg/axiom"
)

var nonGoNameRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

func generateGoStubs(m ProjectModel) string {
	var b strings.Builder
	b.WriteString("package actions\n\nimport (\n  \"context\"\n  \"fmt\"\n)\n\n")
	b.WriteString("type Actions struct{}\n\n")
	module, err := axiom.CompileAny([]byte(m.Source), axiom.WithSourceName(m.Path))
	if err != nil {
		b.WriteString("// compiler-backed stubs are unavailable: " + err.Error() + "\n")
		return b.String()
	}
	names := make([]string, 0, len(module.Activities))
	for name, activity := range module.Activities {
		if activity.Effect != "none" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, activityName := range names {
		activity := module.Activities[activityName]
		name := exportGoName(activityName)
		b.WriteString(fmt.Sprintf("type %sInput struct {\n", name))
		for _, input := range activity.Input {
			b.WriteString(fmt.Sprintf("  %s any `json:%q`\n", exportGoName(input.Name), input.Name))
		}
		b.WriteString("}\n\n")
		b.WriteString(fmt.Sprintf("type %sOutput struct {\n", name))
		for _, output := range activity.Output {
			b.WriteString(fmt.Sprintf("  %s %s `json:%q`\n", exportGoName(output.Name), goStubType(output.Type), output.Name))
		}
		b.WriteString("}\n\n")
		b.WriteString(fmt.Sprintf("func (a Actions) %s(ctx context.Context, input %sInput) (%sOutput, error) {\n", name, name, name))
		b.WriteString(fmt.Sprintf("  return %sOutput{}, fmt.Errorf(\"%s is not implemented\")\n", name, activityName))
		b.WriteString("}\n\n")
	}
	return b.String()
}

func exportGoName(name string) string {
	parts := nonGoNameRe.Split(name, -1)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func goStubType(typeName string) string {
	nullable := strings.HasSuffix(typeName, "?")
	base := strings.TrimSuffix(typeName, "?")
	typ := "any"
	switch base {
	case "String", "Time", "Duration":
		typ = "string"
	case "Bool":
		typ = "bool"
	case "Int":
		typ = "int"
	case "Float":
		typ = "float64"
	case "Object":
		typ = "map[string]any"
	}
	if nullable && typ != "any" {
		return "*" + typ
	}
	return typ
}

func generateReport(m ProjectModel) string {
	var b strings.Builder
	b.WriteString("# Axiom Rule Studio Report\n\n")
	b.WriteString("System: `" + m.SystemName + "`\n\n")
	b.WriteString(fmt.Sprintf("Rules: %d · States: %d · Events: %d · Conditions: %d · Always: %d\n\n", len(m.Rules), len(m.States), len(m.Events), len(m.Conditions), len(m.Always)))
	b.WriteString("## Diagnostics\n\n")
	if len(m.Diagnostics) == 0 {
		b.WriteString("No diagnostics.\n\n")
	} else {
		for _, d := range m.Diagnostics {
			b.WriteString("- " + d + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Rules\n\n")
	names := []string{}
	for n := range m.Rules {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r := m.Rules[n]
		b.WriteString("### " + n + "\n\n")
		if r.OnEvent != "" {
			b.WriteString("Starts: `" + r.OnEvent + "`\n\n")
		}
		if len(r.WhenLines) > 0 {
			b.WriteString("Allowed if:\n")
			for _, x := range r.WhenLines {
				b.WriteString("- " + x + "\n")
			}
			b.WriteString("\n")
		}
		if len(r.DoLines) > 0 {
			b.WriteString("Do:\n")
			for _, x := range r.DoLines {
				b.WriteString("- `" + x + "`\n")
			}
			b.WriteString("\n")
		}
		if len(r.ThenLines) > 0 {
			b.WriteString("Then:\n")
			for _, x := range r.ThenLines {
				b.WriteString("- " + x + "\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func makeProjectZip() (string, error) {
	out := "/mnt/data/axiom_rule_studio_go.zip"
	os.Remove(out)
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	base := "/mnt/data/axiom_rule_studio_go"
	return out, filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(base, path)
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
}
