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
	ExecutionID     string                    `json:"executionId"`
	Domain          string                    `json:"domain"`
	Status          Status                    `json:"status"`
	Version         int                       `json:"version"`
	Context         map[string]map[string]any `json:"context,omitempty"`
	Facts           map[string]FactValue      `json:"facts,omitempty"`
	PendingActivities []ActivityTask          `json:"pendingActivities,omitempty"`
	History         []HistoryEntry            `json:"history"`
	ModuleHash      string                    `json:"moduleHash,omitempty"`
	CompilerVersion string                    `json:"compilerVersion,omitempty"`
	PlanVersion     string                    `json:"planVersion,omitempty"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
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

	pending := make([]ActivityTask, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if task.Status == TaskCompleted || task.Status == TaskFailed || task.Status == TaskSuperseded {
			continue
		}
		pending = append(pending, *task)
	}

	if execution.ID == "" {
		return nil, fmt.Errorf("axiom: execution snapshot requires an execution id")
	}

	return &ExecutionSnapshot{
		ExecutionID:       execution.ID,
		Domain:            execution.Domain,
		Status:            execution.Status,
		Version:           execution.Version,
		Context:           cloneContext(execution.Context),
		Facts:             cloneFacts(execution.Facts),
		PendingActivities: pending,
		History:           cloneHistoryEntries(history),
		ModuleHash:        execution.ModuleHash,
		CompilerVersion:   execution.CompilerVersion,
		PlanVersion:       execution.PlanVersion,
		CreatedAt:         execution.CreatedAt,
		UpdatedAt:         execution.UpdatedAt,
	}, nil
}

func cloneContext(input map[string]map[string]any) map[string]map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]map[string]any, len(input))
	for contextName, fields := range input {
		clonedFields := make(map[string]any, len(fields))
		for key, value := range fields {
			clonedFields[key] = cloneSnapshotValue(value)
		}
		result[contextName] = clonedFields
	}
	return result
}

func cloneHistoryEntries(input []HistoryEntry) []HistoryEntry {
	if input == nil {
		return nil
	}
	result := make([]HistoryEntry, len(input))
	for index, entry := range input {
		result[index] = entry
		if entry.Payload != nil {
			result[index].Payload = make(map[string]any, len(entry.Payload))
			for key, value := range entry.Payload {
				result[index].Payload[key] = cloneSnapshotValue(value)
			}
		}
	}
	return result
}

func cloneSnapshotValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = cloneSnapshotValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneSnapshotValue(item)
		}
		return result
	default:
		return value
	}
}
