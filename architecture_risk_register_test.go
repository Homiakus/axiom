package axiom

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

type architectureRiskRegister struct {
	Version string             `json:"version"`
	Updated string             `json:"updated"`
	Risks   []architectureRisk `json:"risks"`
}

type architectureRisk struct {
	ID                  string   `json:"id"`
	Status              string   `json:"status"`
	Component           string   `json:"component"`
	FailureMode         string   `json:"failure_mode"`
	Effect              string   `json:"effect"`
	Causes              []string `json:"causes"`
	CurrentControls     []string `json:"current_controls"`
	Severity            int      `json:"severity"`
	Occurrence          int      `json:"occurrence"`
	Detectability       int      `json:"detectability"`
	RPN                 int      `json:"rpn"`
	Level               string   `json:"level"`
	OwnerRole           string   `json:"owner_role"`
	Findings            []string `json:"findings"`
	Tasks               []string `json:"tasks"`
	Mitigation          string   `json:"mitigation"`
	TargetOccurrence    int      `json:"target_occurrence"`
	TargetDetectability int      `json:"target_detectability"`
	TargetRPN           int      `json:"target_rpn"`
	ExitEvidence        []string `json:"exit_evidence"`
	ReviewTrigger       string   `json:"review_trigger"`
}

var (
	riskIDPattern    = regexp.MustCompile(`^R-[0-9]{3}$`)
	findingIDPattern = regexp.MustCompile(`^F-[0-9]{3}$`)
	taskIDPattern    = regexp.MustCompile(`^T-[0-9]{3}$`)
)

func TestArchitectureRiskRegister(t *testing.T) {
	raw, err := os.ReadFile("docs/architecture-risk-register.json")
	if err != nil {
		t.Fatalf("read architecture risk register: %v", err)
	}

	var register architectureRiskRegister
	if err := json.Unmarshal(raw, &register); err != nil {
		t.Fatalf("parse architecture risk register: %v", err)
	}
	if strings.TrimSpace(register.Version) == "" || strings.TrimSpace(register.Updated) == "" {
		t.Fatal("architecture risk register must declare version and updated date")
	}
	if len(register.Risks) == 0 {
		t.Fatal("architecture risk register must contain at least one risk")
	}

	masterPlan, err := os.ReadFile("MASTER_PLAN.md")
	if err != nil {
		t.Fatalf("read MASTER_PLAN.md: %v", err)
	}
	plan := string(masterPlan)

	seen := make(map[string]struct{}, len(register.Risks))
	for _, risk := range register.Risks {
		risk := risk
		t.Run(risk.ID, func(t *testing.T) {
			validateArchitectureRisk(t, risk, plan, seen)
		})
	}
}

func validateArchitectureRisk(t *testing.T, risk architectureRisk, masterPlan string, seen map[string]struct{}) {
	t.Helper()

	if !riskIDPattern.MatchString(risk.ID) {
		t.Fatalf("risk id %q must match R-XXX", risk.ID)
	}
	if _, ok := seen[risk.ID]; ok {
		t.Fatalf("duplicate risk id %s", risk.ID)
	}
	seen[risk.ID] = struct{}{}

	validStatus := map[string]bool{
		"OPEN": true, "MITIGATING": true, "VERIFYING": true, "ACCEPTED": true, "CLOSED": true,
	}
	if !validStatus[risk.Status] {
		t.Fatalf("unsupported risk status %q", risk.Status)
	}

	requireText := map[string]string{
		"component":      risk.Component,
		"failure_mode":   risk.FailureMode,
		"effect":         risk.Effect,
		"owner_role":     risk.OwnerRole,
		"mitigation":     risk.Mitigation,
		"review_trigger": risk.ReviewTrigger,
	}
	for field, value := range requireText {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s must not be empty", field)
		}
	}
	if len(risk.Causes) == 0 || len(risk.CurrentControls) == 0 || len(risk.ExitEvidence) == 0 {
		t.Fatal("causes, current_controls and exit_evidence must all be non-empty")
	}

	validateFMEAScore(t, "severity", risk.Severity)
	validateFMEAScore(t, "occurrence", risk.Occurrence)
	validateFMEAScore(t, "detectability", risk.Detectability)
	validateFMEAScore(t, "target_occurrence", risk.TargetOccurrence)
	validateFMEAScore(t, "target_detectability", risk.TargetDetectability)

	wantRPN := risk.Severity * risk.Occurrence * risk.Detectability
	if risk.RPN != wantRPN {
		t.Fatalf("rpn=%d, want severity*occurrence*detectability=%d", risk.RPN, wantRPN)
	}
	wantTargetRPN := risk.Severity * risk.TargetOccurrence * risk.TargetDetectability
	if risk.TargetRPN != wantTargetRPN {
		t.Fatalf("target_rpn=%d, want severity*target_occurrence*target_detectability=%d", risk.TargetRPN, wantTargetRPN)
	}
	if risk.TargetRPN >= risk.RPN && risk.Status != "CLOSED" && risk.Status != "ACCEPTED" {
		t.Fatalf("active risk target_rpn=%d must be lower than current rpn=%d", risk.TargetRPN, risk.RPN)
	}

	wantLevel := fmeaPriority(risk.Severity, risk.RPN)
	if risk.Level != wantLevel {
		t.Fatalf("level=%q, want %q for severity=%d rpn=%d", risk.Level, wantLevel, risk.Severity, risk.RPN)
	}

	if len(risk.Findings) == 0 && len(risk.Tasks) == 0 {
		t.Fatal("risk must link to evidence (F-XXX) or executable plan work (T-XXX)")
	}
	for _, finding := range risk.Findings {
		if !findingIDPattern.MatchString(finding) {
			t.Fatalf("finding reference %q must match F-XXX", finding)
		}
		if !containsPlanID(masterPlan, finding) {
			t.Fatalf("finding %s is not present in MASTER_PLAN.md", finding)
		}
	}
	for _, task := range risk.Tasks {
		if !taskIDPattern.MatchString(task) {
			t.Fatalf("task reference %q must match T-XXX", task)
		}
		if !containsPlanID(masterPlan, task) {
			t.Fatalf("task %s is not present in MASTER_PLAN.md", task)
		}
	}

	if (risk.Level == "critical" || risk.Level == "high") && risk.Status != "CLOSED" && len(risk.Tasks) == 0 {
		t.Fatalf("active %s risk must have at least one mitigation task in MASTER_PLAN.md", risk.Level)
	}
}

func validateFMEAScore(t *testing.T, name string, score int) {
	t.Helper()
	if score < 1 || score > 10 {
		t.Fatalf("%s=%d must be in [1,10]", name, score)
	}
}

func fmeaPriority(severity, rpn int) string {
	switch {
	case rpn >= 300:
		return "critical"
	case rpn >= 180 || severity >= 9:
		return "high"
	case rpn >= 80:
		return "medium"
	default:
		return "low"
	}
}

func containsPlanID(masterPlan, id string) bool {
	// MASTER_PLAN uses full headings for most findings/tasks and bold list
	// entries for compact milestone tasks such as T-010. Requiring one of
	// these canonical declaration forms prevents a risk from being satisfied
	// only by a coincidental prose reference.
	for _, prefix := range []string{
		"### " + id + " ",
		"#### " + id + " ",
		"- **" + id + " ",
	} {
		if strings.Contains(masterPlan, prefix) {
			return true
		}
	}
	return false
}

func TestFMEAPrioritySeverityFloor(t *testing.T) {
	cases := []struct {
		severity int
		rpn      int
		want     string
	}{
		{severity: 10, rpn: 40, want: "high"},
		{severity: 9, rpn: 81, want: "high"},
		{severity: 8, rpn: 300, want: "critical"},
		{severity: 8, rpn: 180, want: "high"},
		{severity: 8, rpn: 80, want: "medium"},
		{severity: 5, rpn: 20, want: "low"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("S%d_RPN%d", tc.severity, tc.rpn), func(t *testing.T) {
			if got := fmeaPriority(tc.severity, tc.rpn); got != tc.want {
				t.Fatalf("fmeaPriority(%d,%d)=%q, want %q", tc.severity, tc.rpn, got, tc.want)
			}
		})
	}
}
