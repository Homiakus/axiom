package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Task struct {
	Heading string `json:"heading"`
	Status  string `json:"status"`
	Line    int    `json:"line"`
}

type EdgeSpace struct {
	SchemaVersion int               `json:"schema_version"`
	Policy        InteractionPolicy `json:"interaction_policy"`
	Dimensions    []Dimension       `json:"dimensions"`
	Sentinels     []Sentinel        `json:"sentinels"`
	Campaigns     []Campaign        `json:"campaigns"`
}

type InteractionPolicy struct {
	PRStrength      int  `json:"pr_strength"`
	NightlyStrength int  `json:"nightly_strength"`
	Exhaustive      bool `json:"exhaustive_sentinels"`
}

type Dimension struct {
	Name     string   `json:"name"`
	Nominal  string   `json:"nominal"`
	Values   []string `json:"values"`
	Critical []string `json:"critical_values"`
}

type Sentinel struct {
	ID         string   `json:"id"`
	Dimensions []string `json:"dimensions"`
	Invariant  string   `json:"invariant"`
	Command    string   `json:"command"`
}

type Campaign struct {
	ID      string `json:"id"`
	Tier    string `json:"tier"`
	Command string `json:"command"`
}

func main() {
	mode := flag.String("mode", "validate", "validate, next, or matrix")
	planPath := flag.String("plan", "docs/PRODUCTION_STABILIZATION_PLAN.md", "canonical execution plan")
	edgePath := flag.String("edge-space", "quality/edge-space.json", "edge-space contract")
	jsonOut := flag.Bool("json", false, "emit JSON when supported")
	flag.Parse()

	tasks, err := parsePlan(*planPath)
	if err != nil {
		fatal(err)
	}

	switch *mode {
	case "validate":
		es, err := loadAndValidateEdgeSpace(*edgePath)
		if err != nil {
			fatal(err)
		}
		next, ok := nextActionable(tasks)
		if !ok {
			fmt.Printf("quality-loop contract valid: %d plan tasks, %d edge dimensions; no actionable task remains\n", len(tasks), len(es.Dimensions))
			return
		}
		fmt.Printf("quality-loop contract valid: %d plan tasks, %d edge dimensions; next=%s [%s]\n", len(tasks), len(es.Dimensions), next.Heading, next.Status)
	case "next":
		next, ok := nextActionable(tasks)
		if !ok {
			if *jsonOut {
				mustJSON(map[string]any{"done": true})
			} else {
				fmt.Println("no actionable task remains")
			}
			return
		}
		if *jsonOut {
			mustJSON(next)
			return
		}
		fmt.Printf("%s\t%s\tline=%d\n", next.Status, next.Heading, next.Line)
	case "matrix":
		es, err := loadAndValidateEdgeSpace(*edgePath)
		if err != nil {
			fatal(err)
		}
		if *jsonOut {
			mustJSON(es)
			return
		}
		printMatrix(es)
	default:
		fatal(fmt.Errorf("unknown mode %q", *mode))
	}
}

func parsePlan(path string) ([]Task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open plan: %w", err)
	}
	defer f.Close()

	var tasks []Task
	var heading string
	var headingLine int
	s := bufio.NewScanner(f)
	line := 0
	for s.Scan() {
		line++
		text := strings.TrimSpace(s.Text())
		if strings.HasPrefix(text, "## ") {
			heading = strings.TrimSpace(strings.TrimPrefix(text, "## "))
			headingLine = line
			continue
		}
		if heading == "" || !strings.HasPrefix(text, "Status:") {
			continue
		}
		status := normalizeStatus(strings.TrimSpace(strings.TrimPrefix(text, "Status:")))
		if status == "" {
			continue
		}
		tasks = append(tasks, Task{Heading: heading, Status: status, Line: headingLine})
		heading = ""
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("scan plan: %w", err)
	}
	if len(tasks) == 0 {
		return nil, errors.New("plan contains no executable task sections with a Status line")
	}
	return tasks, nil
}

func normalizeStatus(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.TrimSpace(strings.TrimSuffix(s, "."))
	return s
}

func nextActionable(tasks []Task) (Task, bool) {
	for _, task := range tasks {
		u := strings.ToUpper(task.Status)
		if strings.Contains(u, "EXTERNAL") {
			continue
		}
		if strings.Contains(u, "P0 BLOCKER") || strings.Contains(u, "TODO") || strings.Contains(u, "PARTIAL") {
			if strings.Contains(u, "DONE") && !strings.Contains(u, "PARTIAL") {
				continue
			}
			return task, true
		}
	}
	return Task{}, false
}

func loadAndValidateEdgeSpace(path string) (EdgeSpace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EdgeSpace{}, fmt.Errorf("read edge space: %w", err)
	}
	var es EdgeSpace
	if err := json.Unmarshal(data, &es); err != nil {
		return EdgeSpace{}, fmt.Errorf("decode edge space: %w", err)
	}
	if es.SchemaVersion != 1 {
		return EdgeSpace{}, fmt.Errorf("unsupported edge-space schema_version %d", es.SchemaVersion)
	}
	if es.Policy.PRStrength < 2 || es.Policy.NightlyStrength < es.Policy.PRStrength {
		return EdgeSpace{}, errors.New("interaction policy must use PR strength >=2 and nightly strength >= PR strength")
	}
	if len(es.Dimensions) < 4 {
		return EdgeSpace{}, errors.New("edge space must define at least four independent dimensions")
	}

	dims := make(map[string]Dimension, len(es.Dimensions))
	for _, d := range es.Dimensions {
		if d.Name == "" || len(d.Values) < 2 {
			return EdgeSpace{}, fmt.Errorf("dimension %q must have a name and at least two values", d.Name)
		}
		if _, exists := dims[d.Name]; exists {
			return EdgeSpace{}, fmt.Errorf("duplicate dimension %q", d.Name)
		}
		valueSet := map[string]bool{}
		for _, v := range d.Values {
			if valueSet[v] {
				return EdgeSpace{}, fmt.Errorf("dimension %q contains duplicate value %q", d.Name, v)
			}
			valueSet[v] = true
		}
		if !valueSet[d.Nominal] {
			return EdgeSpace{}, fmt.Errorf("dimension %q nominal value %q is not listed", d.Name, d.Nominal)
		}
		for _, v := range d.Critical {
			if !valueSet[v] {
				return EdgeSpace{}, fmt.Errorf("dimension %q critical value %q is not listed", d.Name, v)
			}
		}
		dims[d.Name] = d
	}

	seenSentinel := map[string]bool{}
	covered := map[string]bool{}
	for _, s := range es.Sentinels {
		if s.ID == "" || s.Invariant == "" || s.Command == "" || len(s.Dimensions) < 2 {
			return EdgeSpace{}, fmt.Errorf("sentinel %q is incomplete", s.ID)
		}
		if seenSentinel[s.ID] {
			return EdgeSpace{}, fmt.Errorf("duplicate sentinel %q", s.ID)
		}
		seenSentinel[s.ID] = true
		for _, name := range s.Dimensions {
			if _, ok := dims[name]; !ok {
				return EdgeSpace{}, fmt.Errorf("sentinel %q references unknown dimension %q", s.ID, name)
			}
			covered[name] = true
		}
	}
	if es.Policy.Exhaustive {
		for name, d := range dims {
			if len(d.Critical) > 0 && !covered[name] {
				return EdgeSpace{}, fmt.Errorf("critical dimension %q has no executable sentinel", name)
			}
		}
	}
	for _, c := range es.Campaigns {
		if c.ID == "" || c.Command == "" || (c.Tier != "pr" && c.Tier != "nightly" && c.Tier != "weekly") {
			return EdgeSpace{}, fmt.Errorf("invalid campaign %+v", c)
		}
	}
	if len(es.Campaigns) == 0 {
		return EdgeSpace{}, errors.New("edge space must define executable campaigns")
	}
	return es, nil
}

func printMatrix(es EdgeSpace) {
	fmt.Printf("schema=%d pr-strength=%d nightly-strength=%d exhaustive-sentinels=%t\n", es.SchemaVersion, es.Policy.PRStrength, es.Policy.NightlyStrength, es.Policy.Exhaustive)
	fmt.Println("dimension\tvalues\tcritical")
	sorted := append([]Dimension(nil), es.Dimensions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, d := range sorted {
		fmt.Printf("%s\t%d\t%d\n", d.Name, len(d.Values), len(d.Critical))
	}
	fmt.Printf("sentinels=%d campaigns=%d\n", len(es.Sentinels), len(es.Campaigns))
}

func mustJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "qualityloop:", err)
	os.Exit(2)
}
