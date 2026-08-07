package adgo

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type DiagnosticSeverity string

const (
	DiagnosticInfo    DiagnosticSeverity = "info"
	DiagnosticWarning DiagnosticSeverity = "warning"
	DiagnosticError   DiagnosticSeverity = "error"
)

type Diagnostic struct {
	Severity DiagnosticSeverity `json:"severity"`
	Code     string             `json:"code"`
	NodeID   string             `json:"nodeId,omitempty"`
	TaskID   string             `json:"taskId,omitempty"`
	Message  string             `json:"message"`
}

type TaskDiagnostic struct {
	ID         string     `json:"id"`
	NodeID     string     `json:"nodeId"`
	Activity   string     `json:"activity"`
	WorkerID   string     `json:"workerId,omitempty"`
	Attempt    int        `json:"attempt"`
	Status     TaskStatus `json:"status"`
	LeaseUntil time.Time  `json:"leaseUntil,omitempty"`
	LeaseLeft  time.Duration `json:"leaseLeft,omitempty"`
}

type ExecutionDiagnostics struct {
	Summary      ExecutionSummary     `json:"summary"`
	Ready        []string             `json:"ready,omitempty"`
	Waiting      map[string]string    `json:"waiting,omitempty"`
	ActiveTasks  []TaskDiagnostic     `json:"activeTasks,omitempty"`
	Budget       BudgetUsage          `json:"budget"`
	BudgetLimit  BudgetLimit          `json:"budgetLimit"`
	Quality      QualityVector        `json:"quality,omitempty"`
	Diagnostics  []Diagnostic         `json:"diagnostics,omitempty"`
	ProviderHealth []ProviderHealth   `json:"providerHealth,omitempty"`
}

func (e *Engine) Diagnostics(ctx context.Context, executionID string) (ExecutionDiagnostics, error) {
	execution, err := e.store.Load(ctx, executionID)
	if err != nil {
		return ExecutionDiagnostics{}, err
	}
	report := ExecutionDiagnostics{
		Summary: ExecutionSummary{ID: execution.ID, PlanID: execution.PlanID, PlanDigest: execution.PlanDigest, Version: execution.Version, Status: execution.Status, Failure: execution.Failure},
		Waiting: cloneStringMap(execution.WaitingFor), Budget: execution.BudgetUsage, BudgetLimit: execution.BudgetLimit, Quality: cloneQuality(execution.Quality),
	}
	now := time.Now().UTC()
	for _, task := range execution.ActiveTasks {
		left := time.Duration(0)
		if !task.LeaseUntil.IsZero() {
			left = task.LeaseUntil.Sub(now)
		}
		report.ActiveTasks = append(report.ActiveTasks, TaskDiagnostic{ID: task.ID, NodeID: task.NodeID, Activity: task.Activity, WorkerID: task.WorkerID, Attempt: task.Attempt, Status: task.Status, LeaseUntil: task.LeaseUntil, LeaseLeft: left})
	}
	sort.Slice(report.ActiveTasks, func(i, j int) bool { return report.ActiveTasks[i].ID < report.ActiveTasks[j].ID })

	for id, node := range e.plan.Nodes {
		runtime := execution.Nodes[id]
		if isReady(e.plan, execution, node, runtime, now) {
			report.Ready = append(report.Ready, id)
		}
	}
	sort.Strings(report.Ready)
	report.Diagnostics = AuditExecution(e.plan, execution, now)
	if e.router != nil {
		health, healthErr := e.router.SnapshotContext(ctx)
		if healthErr != nil {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Severity: DiagnosticWarning, Code: "ADG-DIAG-ROUTER", Message: healthErr.Error()})
		} else {
			report.ProviderHealth = health
		}
	}
	return report, nil
}

// AuditExecution checks internal invariants without mutating state. It is safe to
// run in a monitor process against any committed snapshot.
func AuditExecution(plan *Plan, execution *Execution, now time.Time) []Diagnostic {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := []Diagnostic{}
	if plan == nil || execution == nil {
		return []Diagnostic{{Severity: DiagnosticError, Code: "ADG-DIAG-NIL", Message: "plan and execution are required"}}
	}
	if execution.PlanID != plan.ID || execution.PlanDigest != plan.Digest {
		out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-PIN", Message: "execution plan pin does not match loaded plan"})
	}

	activeByNode := map[string]int{}
	for taskID, task := range execution.ActiveTasks {
		node, exists := plan.Nodes[task.NodeID]
		if !exists {
			out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-ORPHAN-TASK", TaskID: taskID, NodeID: task.NodeID, Message: "active task references unknown node"})
			continue
		}
		_ = node
		activeByNode[task.NodeID]++
		runtime := execution.Nodes[task.NodeID]
		if runtime == nil || runtime.Status != NodeRunning {
			out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-TASK-STATE", TaskID: taskID, NodeID: task.NodeID, Message: "active task exists while node is not running"})
		}
		if task.Status == TaskRunning {
			if task.WorkerID == "" {
				out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-WORKER", TaskID: taskID, NodeID: task.NodeID, Message: "running task has no worker id"})
			}
			if task.LeaseUntil.IsZero() {
				out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-LEASE", TaskID: taskID, NodeID: task.NodeID, Message: "running task has no lease"})
			} else if !task.LeaseUntil.After(now) {
				out = append(out, Diagnostic{Severity: DiagnosticWarning, Code: "ADG-DIAG-LEASE-EXPIRED", TaskID: taskID, NodeID: task.NodeID, Message: "worker lease expired and awaits coordinator recovery"})
			}
		}
	}

	for nodeID, runtime := range execution.Nodes {
		if runtime == nil {
			out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-NODE-NIL", NodeID: nodeID, Message: "node runtime is nil"})
			continue
		}
		if _, exists := plan.Nodes[nodeID]; !exists {
			out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-ORPHAN-NODE", NodeID: nodeID, Message: "execution contains node absent from plan"})
		}
		if runtime.Status == NodeRunning && activeByNode[nodeID] == 0 {
			out = append(out, Diagnostic{Severity: DiagnosticWarning, Code: "ADG-DIAG-RUNNING-NO-TASK", NodeID: nodeID, Message: "node is running without active task"})
		}
		if runtime.Status == NodeWaiting {
			if _, ok := execution.WaitingFor[nodeID]; !ok && runtime.NotBefore.IsZero() {
				out = append(out, Diagnostic{Severity: DiagnosticWarning, Code: "ADG-DIAG-WAIT-REASON", NodeID: nodeID, Message: "node is waiting without event or timer reason"})
			}
		}
		if runtime.Status == NodeCompleted && activeByNode[nodeID] > 0 {
			out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-COMPLETED-TASK", NodeID: nodeID, Message: "completed node still owns active task"})
		}
	}

	for nodeID := range execution.WaitingFor {
		if nodeID == executionControlNode {
			continue
		}
		runtime := execution.Nodes[nodeID]
		if runtime == nil || runtime.Status != NodeWaiting {
			out = append(out, Diagnostic{Severity: DiagnosticWarning, Code: "ADG-DIAG-WAIT-ORPHAN", NodeID: nodeID, Message: "waiting reason exists for non-waiting node"})
		}
	}

	for index, entry := range execution.History {
		expected := uint64(index + 1)
		if entry.Seq != expected {
			out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-HISTORY-SEQ", Message: fmt.Sprintf("history sequence %d at index %d; expected %d", entry.Seq, index, expected)})
			break
		}
	}
	if execution.BudgetUsage.Cost < 0 || execution.BudgetUsage.Tokens < 0 || execution.BudgetUsage.LLMCalls < 0 || execution.BudgetUsage.SearchQueries < 0 || execution.BudgetUsage.BrowserFetches < 0 {
		out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-BUDGET", Message: "budget usage contains negative values"})
	}
	if terminal(execution.Status) && len(execution.ActiveTasks) > 0 {
		out = append(out, Diagnostic{Severity: DiagnosticError, Code: "ADG-DIAG-TERMINAL-ACTIVE", Message: "terminal execution still contains active tasks"})
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

type FleetDiagnostics struct {
	Total       int                           `json:"total"`
	ByStatus    map[ExecutionStatus]int       `json:"byStatus"`
	Diagnostics map[string][]Diagnostic       `json:"diagnostics,omitempty"`
}

func AuditFleet(ctx context.Context, store Store, plans map[string]*Plan) (FleetDiagnostics, error) {
	catalog, ok := store.(ExecutionCatalog)
	if !ok {
		return FleetDiagnostics{}, fmt.Errorf("adgo: fleet audit requires ExecutionCatalog")
	}
	ids, err := catalog.ListExecutionIDs(ctx)
	if err != nil {
		return FleetDiagnostics{}, err
	}
	out := FleetDiagnostics{ByStatus: map[ExecutionStatus]int{}, Diagnostics: map[string][]Diagnostic{}}
	for _, id := range ids {
		execution, err := store.Load(ctx, id)
		if err != nil {
			return out, err
		}
		out.Total++
		out.ByStatus[execution.Status]++
		plan := plans[execution.PlanDigest]
		if plan == nil {
			out.Diagnostics[id] = []Diagnostic{{Severity: DiagnosticError, Code: "ADG-DIAG-PLAN-MISSING", Message: "plan digest is not loaded by audit process"}}
			continue
		}
		if diagnostics := AuditExecution(plan, execution, time.Now().UTC()); len(diagnostics) > 0 {
			out.Diagnostics[id] = diagnostics
		}
	}
	return out, nil
}
