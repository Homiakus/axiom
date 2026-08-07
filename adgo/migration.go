package adgo

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type MigrationPolicy struct {
	// NodeMap maps old node ids to their replacement ids. Omitted ids keep their
	// name when that node still exists in the target plan.
	NodeMap             map[string]string
	ResetNodes          []string
	AllowSemanticChange bool
	AllowRemovedDormant bool
	DataPatch           map[string]any
	Reason              string
	Actor               string
}

type MigrationReport struct {
	Allowed       bool     `json:"allowed"`
	FromDigest    string   `json:"fromDigest"`
	ToDigest      string   `json:"toDigest"`
	MappedNodes   int      `json:"mappedNodes"`
	AddedNodes    []string `json:"addedNodes,omitempty"`
	RemovedNodes  []string `json:"removedNodes,omitempty"`
	ResetNodes    []string `json:"resetNodes,omitempty"`
	Problems      []string `json:"problems,omitempty"`
}

// ValidatePlanMigration is intentionally conservative for completed work. A
// caller may opt into semantic change, but the default protects against silently
// reinterpreting already-committed external effects under a different node.
func ValidatePlanMigration(from, to *Plan, execution *Execution, policy MigrationPolicy) MigrationReport {
	report := MigrationReport{}
	if from != nil {
		report.FromDigest = from.Digest
	}
	if to != nil {
		report.ToDigest = to.Digest
	}
	if from == nil || to == nil || execution == nil {
		report.Problems = append(report.Problems, "source plan, target plan and execution are required")
		return report
	}
	if execution.PlanID != from.ID || execution.PlanDigest != from.Digest {
		report.Problems = append(report.Problems, "execution is not pinned to the source plan")
	}
	if len(execution.ActiveTasks) > 0 {
		report.Problems = append(report.Problems, "migration requires a quiescent point with no active tasks")
	}
	seenTargets := map[string]string{}
	for oldID, runtime := range execution.Nodes {
		newID := oldID
		if mapped := policy.NodeMap[oldID]; mapped != "" {
			newID = mapped
		}
		newNode, exists := to.Nodes[newID]
		if !exists {
			if runtime != nil && (runtime.Status == NodeDormant || runtime.Status == NodeSkipped) && policy.AllowRemovedDormant {
				report.RemovedNodes = append(report.RemovedNodes, oldID)
				continue
			}
			report.Problems = append(report.Problems, fmt.Sprintf("old node %s has no target node %s", oldID, newID))
			continue
		}
		if previous, duplicate := seenTargets[newID]; duplicate && previous != oldID {
			report.Problems = append(report.Problems, fmt.Sprintf("old nodes %s and %s both map to %s", previous, oldID, newID))
			continue
		}
		seenTargets[newID] = oldID
		report.MappedNodes++
		oldNode, oldExists := from.Nodes[oldID]
		if !oldExists || runtime == nil || runtime.Status != NodeCompleted || policy.AllowSemanticChange {
			continue
		}
		if !completedNodeCompatible(oldNode, newNode) {
			report.Problems = append(report.Problems, fmt.Sprintf("completed node %s changes execution semantics in target node %s", oldID, newID))
		}
	}
	for newID := range to.Nodes {
		if _, mapped := seenTargets[newID]; !mapped {
			report.AddedNodes = append(report.AddedNodes, newID)
		}
	}
	for _, id := range policy.ResetNodes {
		if _, ok := to.Nodes[id]; !ok {
			report.Problems = append(report.Problems, fmt.Sprintf("reset node %s does not exist in target plan", id))
		}
	}
	report.ResetNodes = append([]string(nil), policy.ResetNodes...)
	sort.Strings(report.AddedNodes)
	sort.Strings(report.RemovedNodes)
	sort.Strings(report.ResetNodes)
	report.Allowed = len(report.Problems) == 0
	return report
}

func completedNodeCompatible(oldNode, newNode Node) bool {
	return oldNode.Kind == newNode.Kind &&
		oldNode.Activity == newNode.Activity &&
		oldNode.Capability == newNode.Capability &&
		oldNode.ExternalEffect == newNode.ExternalEffect &&
		oldNode.Irreversible == newNode.Irreversible &&
		oldNode.Compensation == newNode.Compensation
}

// MigrateExecution atomically repins a quiescent execution from one immutable
// Plan to another. New nodes are initialized, retained nodes keep their durable
// status, and reset roots invalidate their target-plan descendants and outputs.
func MigrateExecution(ctx context.Context, store Store, from, to *Plan, executionID string, policy MigrationPolicy) (*Execution, MigrationReport, error) {
	current, err := store.Load(ctx, executionID)
	if err != nil {
		return nil, MigrationReport{}, err
	}
	report := ValidatePlanMigration(from, to, current, policy)
	if !report.Allowed {
		return nil, report, fmt.Errorf("adgo: plan migration rejected: %v", report.Problems)
	}

	for attempt := 0; attempt < 8; attempt++ {
		current, err = store.Load(ctx, executionID)
		if err != nil {
			return nil, report, err
		}
		report = ValidatePlanMigration(from, to, current, policy)
		if !report.Allowed {
			return nil, report, fmt.Errorf("adgo: plan migration became invalid: %v", report.Problems)
		}
		next, commitErr := store.Commit(ctx, executionID, current.Version, func(x *Execution) error {
			newRuntime := make(map[string]*NodeRuntime, len(to.Nodes))
			newWaiting := map[string]string{}
			oldByNew := map[string]string{}
			for oldID := range x.Nodes {
				newID := oldID
				if mapped := policy.NodeMap[oldID]; mapped != "" {
					newID = mapped
				}
				if _, ok := to.Nodes[newID]; ok {
					oldByNew[newID] = oldID
				}
			}
			entries := map[string]struct{}{}
			for _, id := range to.Entry {
				entries[id] = struct{}{}
			}
			for newID := range to.Nodes {
				if oldID, ok := oldByNew[newID]; ok {
					if oldRuntime := x.Nodes[oldID]; oldRuntime != nil {
						copyRuntime := *oldRuntime
						newRuntime[newID] = &copyRuntime
						if expected := x.WaitingFor[oldID]; expected != "" {
							newWaiting[newID] = expected
						}
						continue
					}
				}
				_, entry := entries[newID]
				status := NodeDormant
				if entry {
					status = NodePending
				}
				newRuntime[newID] = &NodeRuntime{Status: status, Activated: entry}
			}

			x.Nodes = newRuntime
			x.WaitingFor = newWaiting
			x.ActiveTasks = map[string]TaskRuntime{}
			x.PlanID = to.ID
			x.PlanVersion = to.Version
			x.PlanDigest = to.Digest
			x.Status = StatusRunning
			x.Failure = ""

			// Reconstruct activation caused by already-completed nodes under the
			// target plan, then apply explicit reset roots.
			completedIDs := make([]string, 0, len(x.Nodes))
			for id, runtime := range x.Nodes {
				if runtime != nil && runtime.Status == NodeCompleted {
					completedIDs = append(completedIDs, id)
				}
			}
			sort.Strings(completedIDs)
			for _, id := range completedIDs {
				activateNext(to, x, id, x.Nodes[id].Outcome)
			}

			affected := map[string]struct{}{}
			for _, root := range policy.ResetNodes {
				affected[root] = struct{}{}
				for id := range to.descendants[root] {
					affected[id] = struct{}{}
				}
			}
			for id := range affected {
				runtime := x.Nodes[id]
				if runtime == nil {
					continue
				}
				runtime.Status = NodePending
				runtime.Activated = true
				runtime.Outcome = ""
				runtime.NotBefore = time.Time{}
				runtime.CompletedAt = time.Time{}
				runtime.LastError = ""
				runtime.LastFailure = ""
				runtime.Signature = ""
				delete(x.WaitingFor, id)
				node := to.Nodes[id]
				for _, key := range node.Produces {
					delete(x.Data, key)
					delete(x.Artifacts, key)
				}
			}
			for key, value := range policy.DataPatch {
				if err := SetData(x, key, value); err != nil {
					return err
				}
			}
			x.PlanDeltas = append(x.PlanDeltas, PlanDeltaRecord{Digest: to.Digest, AppliedAt: time.Now().UTC(), Reason: policy.Reason})
			appendHistory(x, "plan_migrated", "", policy.Reason, map[string]any{
				"fromDigest": from.Digest,
				"toDigest":   to.Digest,
				"actor":      policy.Actor,
				"resetNodes": policy.ResetNodes,
			})
			return nil
		})
		if commitErr == ErrConflict {
			continue
		}
		return next, report, commitErr
	}
	return nil, report, ErrConflict
}
