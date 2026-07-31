package runtime

import (
	"context"
	"sort"
	"time"

	"github.com/Homiakus/axiom/internal/compiler"
	"github.com/Homiakus/axiom/internal/diag"
)

// ReplayFromHistoryContext reconstructs an execution while honoring caller
// cancellation. It is equivalent to ReplayFromHistory for successful runs.
func ReplayFromHistoryContext(ctx context.Context, module *compiler.Module, history []HistoryEntry) (*Execution, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if module == nil {
		return nil, diag.Error{Code: "AX900", Kind: "replay", Message: "module is required"}
	}

	entries := append([]HistoryEntry{}, history...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
	engine := NewEngine(module, replayStore{}, nil)
	var execution *Execution

	for index, entry := range entries {
		if index%128 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		switch entry.Type {
		case "ExecutionStarted":
			execution = &Execution{
				ID:              stringPayload(entry.Payload, "executionID"),
				Domain:          stringPayload(entry.Payload, "domain"),
				Status:          StatusStarted,
				Context:         map[string]map[string]any{},
				Computed:        map[string]any{},
				Facts:           map[string]FactValue{},
				ModuleHash:      stringPayload(entry.Payload, "moduleHash"),
				CompilerVersion: stringPayload(entry.Payload, "compilerVersion"),
				PlanVersion:     stringPayload(entry.Payload, "planVersion"),
				CreatedAt:       entry.CreatedAt,
				UpdatedAt:       entry.CreatedAt,
			}
			if execution.Domain == "" {
				execution.Domain = module.Domain
			}
			if execution.ModuleHash == "" {
				execution.ModuleHash = module.CompiledHash
			}
			if execution.ModuleHash != "" && module.CompiledHash != "" && execution.ModuleHash != module.CompiledHash {
				return nil, diag.Error{Code: "AX901", Kind: "replay", Message: "module hash mismatch during replay", Hint: "Replay with the same compiled module version that produced the history."}
			}
			if execution.CompilerVersion == "" {
				execution.CompilerVersion = module.CompilerVersion
			}
			if execution.PlanVersion == "" {
				execution.PlanVersion = module.PlanVersion
			}
			if execution.ID == "" {
				execution.ID = "replay"
			}
			if createdAt := stringPayload(entry.Payload, "createdAt"); createdAt != "" {
				if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
					execution.CreatedAt = parsed
					execution.UpdatedAt = parsed
				}
			}
			if err := engine.applyDefaults(execution); err != nil {
				return nil, err
			}
			engine.prepareExecution(execution)

		case "ContextPatched":
			if execution == nil {
				return nil, replayOrderError(entry)
			}
			values, ok := mapPayload(entry.Payload, "values")
			if !ok && changedPayloadLen(entry.Payload) > 0 {
				return nil, diag.Error{Code: "AX902", Kind: "replay", Message: "ContextPatched event does not contain values", Hint: "Old histories without patch values cannot be used as an event-sourcing source of truth."}
			}
			if len(values) > 0 {
				if _, err := engine.applyPatch(execution, values); err != nil {
					return nil, err
				}
			}

		case "WriteApplied":
			if execution == nil {
				return nil, replayOrderError(entry)
			}
			values, ok := mapPayload(entry.Payload, "values")
			if !ok {
				values, ok = mapPayload(entry.Payload, "writes")
			}
			if !ok && changedPayloadLen(entry.Payload) > 0 {
				return nil, diag.Error{Code: "AX903", Kind: "replay", Message: "WriteApplied event does not contain values", Hint: "Old histories without write values cannot be used as an event-sourcing source of truth."}
			}
			if len(values) > 0 {
				if _, err := engine.applyPatch(execution, values); err != nil {
					return nil, err
				}
			}

		case "SignalReceived":
			if execution == nil {
				return nil, replayOrderError(entry)
			}
			execution.Status = StatusRunning
		case "ActivityScheduled":
			if execution == nil {
				return nil, replayOrderError(entry)
			}
			execution.Status = StatusWaiting
		case "ActivityCompleted":
			if execution == nil {
				return nil, replayOrderError(entry)
			}
			execution.Status = StatusRunning
		case "ActivityFailed":
			if execution == nil {
				return nil, replayOrderError(entry)
			}
			execution.Status = StatusFailed
		case "ExecutionReachedFixpoint":
			if execution == nil {
				return nil, replayOrderError(entry)
			}
			if _, err := engine.recomputeFast(execution, nil); err != nil {
				return nil, err
			}
			if execution.Status != StatusFailed {
				execution.Status = StatusWaiting
			}
		case "ExecutionCompleted":
			if execution != nil {
				execution.Status = StatusCompleted
			}
		case "ExecutionFailed":
			if execution != nil {
				execution.Status = StatusFailed
			}
		case "ExecutionCanceled":
			if execution != nil {
				execution.Status = StatusCanceled
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if execution == nil {
		return nil, diag.Error{Code: "AX904", Kind: "replay", Message: "history does not contain ExecutionStarted"}
	}
	if _, err := engine.recomputeFast(execution, nil); err != nil {
		return nil, err
	}
	if err := engine.checkClaimsFast(execution, nil); err != nil {
		return nil, err
	}
	return execution, nil
}
