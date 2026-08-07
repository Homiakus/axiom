package adgo

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExecutionCatalog is the optional store capability required by the production
// coordinator/worker engine. It deliberately stays outside Store so existing
// custom stores remain source-compatible; production stores can opt in without
// changing the embedded Runtime contract.
type ExecutionCatalog interface {
	ListExecutionIDs(context.Context) ([]string, error)
}

// ExecutionSummary is a compact control-plane projection suitable for admin
// APIs, schedulers and dashboards. Large domain data and artifacts are not
// copied into the listing path.
type ExecutionSummary struct {
	ID         string          `json:"id"`
	PlanID     string          `json:"planId"`
	PlanDigest string          `json:"planDigest"`
	Version    uint64          `json:"version"`
	Status     ExecutionStatus `json:"status"`
	Failure    string          `json:"failure,omitempty"`
}

func (s *MemoryStore) ListExecutionIDs(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.exec))
	for id := range s.exec {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *FileStore) ListExecutionIDs(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	root := filepath.Join(s.root, "executions")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		commits := filepath.Join(root, entry.Name(), "commits")
		files, err := os.ReadDir(commits)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		names := make([]string, 0, len(files))
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
				names = append(names, f.Name())
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		data, err := os.ReadFile(filepath.Join(commits, names[len(names)-1]))
		if err != nil {
			return nil, err
		}
		var execution Execution
		if err := json.Unmarshal(data, &execution); err != nil {
			return nil, err
		}
		if execution.ID != "" {
			ids = append(ids, execution.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ListExecutionSummaries returns compact snapshots for stores that implement
// ExecutionCatalog. The sort order is deterministic by execution id.
func ListExecutionSummaries(ctx context.Context, store Store) ([]ExecutionSummary, error) {
	catalog, ok := store.(ExecutionCatalog)
	if !ok {
		return nil, errors.New("adgo: store does not implement ExecutionCatalog")
	}
	ids, err := catalog.ListExecutionIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ExecutionSummary, 0, len(ids))
	for _, id := range ids {
		execution, err := store.Load(ctx, id)
		if err != nil {
			if errors.Is(err, ErrExecutionNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, ExecutionSummary{
			ID:         execution.ID,
			PlanID:     execution.PlanID,
			PlanDigest: execution.PlanDigest,
			Version:    execution.Version,
			Status:     execution.Status,
			Failure:    execution.Failure,
		})
	}
	return out, nil
}
