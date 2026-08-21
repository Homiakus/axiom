package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type ContinueOptions struct {
	Reason         string
	Actor          string
	CarryData      []string
	CarryArtifacts []string
	Budget         *BudgetLimit
}

// ContinueAsNew closes a quiescent execution and starts a fresh control-plane
// execution under the same immutable plan. This bounds per-execution history
// growth while preserving selected durable facts/artifacts.
func (e *Engine) ContinueAsNew(ctx context.Context, executionID, newID string, options ContinueOptions) (*Execution, error) {
	if executionID == newID || newID == "" {
		return nil, fmt.Errorf("adgo: continue-as-new requires a distinct non-empty execution id")
	}
	current, err := e.store.Load(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if err := e.verifyPinned(current); err != nil {
		return nil, err
	}
	if len(current.ActiveTasks) > 0 {
		return nil, fmt.Errorf("adgo: continue-as-new requires a quiescent execution")
	}
	if current.Status == StatusCompensating {
		return nil, fmt.Errorf("adgo: cannot continue-as-new during compensation")
	}

	dataKeys := options.CarryData
	if dataKeys == nil {
		dataKeys = make([]string, 0, len(current.Data))
		for key := range current.Data {
			dataKeys = append(dataKeys, key)
		}
		sort.Strings(dataKeys)
	}
	initial := map[string]any{}
	for _, key := range dataKeys {
		raw, ok := current.Data[key]
		if !ok {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode carried data %s: %w", key, err)
		}
		initial[key] = value
	}
	budget := current.BudgetLimit
	if options.Budget != nil {
		budget = *options.Budget
	}

	if _, err := e.StartOrLoad(ctx, newID, initial, budget); err != nil {
		return nil, err
	}
	artifactKeys := options.CarryArtifacts
	if artifactKeys == nil {
		artifactKeys = make([]string, 0, len(current.Artifacts))
		for key := range current.Artifacts {
			artifactKeys = append(artifactKeys, key)
		}
		sort.Strings(artifactKeys)
	}
	created, err := e.mutate(ctx, newID, func(x *Execution) error {
		for _, key := range artifactKeys {
			if artifact, ok := current.Artifacts[key]; ok {
				x.Artifacts[key] = artifact
			}
		}
		if err := SetData(x, "__adgo:continuedFrom", map[string]any{"execution": executionID, "version": current.Version}); err != nil {
			return err
		}
		appendHistory(x, "continued_from", "", options.Reason, map[string]any{"execution": executionID, "version": current.Version, "actor": options.Actor})
		return nil
	})
	if err != nil {
		return nil, err
	}

	_, err = e.mutate(ctx, executionID, func(x *Execution) error {
		if len(x.ActiveTasks) > 0 {
			return fmt.Errorf("adgo: source execution became active during continue-as-new")
		}
		if err := SetData(x, "__adgo:continuedAs", newID); err != nil {
			return err
		}
		x.Status = StatusCompleted
		x.Metrics.WallTime = time.Since(x.CreatedAt)
		appendHistory(x, "continued_as_new", "", options.Reason, map[string]any{"newExecution": newID, "actor": options.Actor})
		return nil
	})
	if err != nil {
		// The new execution already exists durably. Returning the error is safer
		// than deleting it; a retry is idempotent because StartOrLoad uses newID.
		if errors.Is(err, ErrConflict) {
			return created, err
		}
		return created, err
	}
	return created, nil
}
