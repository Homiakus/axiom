package runtime

import (
	"context"
	"fmt"
	"time"
)

// ExecutionSnapshot is a JSON-friendly, read-only representation of one durable
// execution. It is intended for diagnostics, audit/export pipelines and
// presentation adapters. Snapshot never mutates or advances the execution.
type ExecutionSnapshot struct {
	ExecutionID       string                    `json:"executionId"`
	Domain            string                    `json:"domain"`
	Status            Status                    `json:"status"`
	Version           int                       `json:"version"`
	Context           map[string]map[string]any `json:"context,omitempty"`
	Facts             map[string]FactValue      `json:"facts,omitempty"`
	PendingActivities []ActivityTask            `json:"pendingActivities,omitempty"`
	History           []HistoryEntry            `json:"history"`
	ModuleHash        string                    `json:"moduleHash,omitempty"`
	CompilerVersion   string                    `json:"compilerVersion,omitempty"`
	PlanVersion       string                    `json:"planVersion,omitempty"`
	CreatedAt         time.Time                 `json:"createdAt"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
}

// Snapshot returns a deterministic read-only snapshot of the current execution.
// It uses persisted execution/update timestamps and does not inject a wall-clock
// capture time, so equal durable state produces equal exported data.
func (r *Run) Snapshot(ctx context.Context) (*ExecutionSnapshot, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	execution, err := r.engine.store.GetExecution(ctx, r.id)
	if err != nil {
		return nil, err
	}
	history, err := r.engine.store.ListHistory(ctx, r.id)
	if err != nil {
		return nil, err
	}
	tasks, err := r.engine.store.ListTasks(ctx, r.id)
	if err != nil {
		return nil, err
	}

	if execution.ID == "" {
		return nil, fmt.Errorf("axiom: execution snapshot requires an execution id")
	}

	return &ExecutionSnapshot{
		ExecutionID:       execution.ID,
		Domain:            execution.Domain,
		Status:            execution.Status,
		Version:           execution.Version,
		Context:           CloneContext(execution.Context),
		Facts:             CloneFacts(execution.Facts),
		PendingActivities: snapshotPendingActivities(tasks),
		History:           snapshotHistory(history),
		ModuleHash:        execution.ModuleHash,
		CompilerVersion:   execution.CompilerVersion,
		PlanVersion:       execution.PlanVersion,
		CreatedAt:         execution.CreatedAt,
		UpdatedAt:         execution.UpdatedAt,
	}, nil
}

func snapshotPendingActivities(tasks []*ActivityTask) []ActivityTask {
	pending := make([]ActivityTask, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if task.Status == TaskCompleted || task.Status == TaskFailed || task.Status == TaskSuperseded {
			continue
		}
		copyTask := *task
		copyTask.Input = CloneAnyMap(task.Input)
		copyTask.Result = CloneAnyMap(task.Result)
		pending = append(pending, copyTask)
	}
	return pending
}

func snapshotHistory(input []HistoryEntry) []HistoryEntry {
	if input == nil {
		return nil
	}
	result := make([]HistoryEntry, len(input))
	for index, entry := range input {
		result[index] = entry
		result[index].Payload = CloneAnyMap(entry.Payload)
	}
	return result
}
