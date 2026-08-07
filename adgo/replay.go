package adgo

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type VersionedStore interface {
	ListVersions(context.Context, string) ([]*Execution, error)
}

type ReplayReport struct {
	ExecutionID string   `json:"executionId"`
	Versions    int      `json:"versions"`
	Valid       bool     `json:"valid"`
	Problems    []string `json:"problems,omitempty"`
}

// ListVersions returns immutable committed snapshots in version order. This is
// both an audit trail and a recovery substrate: the latest valid snapshot is the
// execution state, while earlier snapshots make every committed transition
// inspectable without re-running probabilistic activities.
func (s *FileStore) ListVersions(_ context.Context, id string) ([]*Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.commitsDir(id))
	if errorsIsNotExist(err) {
		return nil, ErrExecutionNotFound
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, ent := range entries {
		if !ent.IsDir() && strings.HasSuffix(ent.Name(), ".json") {
			names = append(names, ent.Name())
		}
	}
	sort.Strings(names)
	out := make([]*Execution, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(s.commitsDir(id), name))
		if err != nil {
			return nil, err
		}
		var e Execution
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("decode commit %s: %w", name, err)
		}
		ensureExecution(&e)
		out = append(out, &e)
	}
	return out, nil
}

func errorsIsNotExist(err error) bool {
	return err != nil && (os.IsNotExist(err) || err == fs.ErrNotExist)
}

// VerifyReplay validates the deterministic control-plane properties across a
// sequence of committed snapshots. It deliberately does not re-run activities:
// external/LLM work is at-least-once and may be probabilistic; committed facts
// and artifact digests are the replay boundary.
func VerifyReplay(plan *Plan, versions []*Execution) ReplayReport {
	report := ReplayReport{Valid: true}
	if len(versions) == 0 {
		report.Valid = false
		report.Problems = append(report.Problems, "no committed versions")
		return report
	}
	report.ExecutionID = versions[0].ID
	report.Versions = len(versions)
	for i, e := range versions {
		if e.ID != report.ExecutionID {
			report.Problems = append(report.Problems, fmt.Sprintf("version %d execution id changed", i+1))
		}
		if e.PlanID != plan.ID || e.PlanDigest != plan.Digest {
			report.Problems = append(report.Problems, fmt.Sprintf("version %d plan pin mismatch", e.Version))
		}
		if i > 0 {
			prev := versions[i-1]
			if e.Version != prev.Version+1 {
				report.Problems = append(report.Problems, fmt.Sprintf("version sequence %d -> %d is not contiguous", prev.Version, e.Version))
			}
			if len(e.History) < len(prev.History) {
				report.Problems = append(report.Problems, fmt.Sprintf("history shrank at version %d", e.Version))
			}
			if e.BudgetUsage.Cost+1e-12 < prev.BudgetUsage.Cost {
				report.Problems = append(report.Problems, fmt.Sprintf("cost decreased at version %d", e.Version))
			}
			if e.BudgetUsage.Tokens < prev.BudgetUsage.Tokens {
				report.Problems = append(report.Problems, fmt.Sprintf("token usage decreased at version %d", e.Version))
			}
		}
		for j, h := range e.History {
			if h.Seq != uint64(j+1) {
				report.Problems = append(report.Problems, fmt.Sprintf("history sequence invalid at version %d index %d", e.Version, j))
				break
			}
		}
		for id, rt := range e.Nodes {
			if _, ok := plan.Nodes[id]; !ok {
				report.Problems = append(report.Problems, fmt.Sprintf("unknown node %s at version %d", id, e.Version))
			}
			if rt == nil {
				report.Problems = append(report.Problems, fmt.Sprintf("nil runtime for node %s at version %d", id, e.Version))
			}
		}
	}
	report.Valid = len(report.Problems) == 0
	return report
}
