package memory

import (
	"context"
	"sort"
)

// ListExecutionIDs implements runtime.ExecutionCatalog.
func (s *Store) ListExecutionIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.executions))
	for id := range s.executions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
