package pebble

import (
	"context"

	"github.com/Homiakus/axiom/internal/runtime"
	pebbledb "github.com/cockroachdb/pebble"
)

// ListExecutionIDs implements runtime.ExecutionCatalog by scanning only the
// execution namespace. The persisted Execution.ID is authoritative, so catalog
// enumeration does not need to reverse the store key escaping scheme.
func (s *Store) ListExecutionIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	const prefix = "exec/"
	iter, err := s.db.NewIter(&pebbledb.IterOptions{LowerBound: []byte(prefix), UpperBound: []byte(prefixEnd(prefix))})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	ids := make([]string, 0)
	for iter.First(); iter.Valid(); iter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var execution runtime.Execution
		if err := decodeValue(s.codec, iter.Value(), &execution); err != nil {
			return nil, err
		}
		if execution.ID != "" {
			ids = append(ids, execution.ID)
		}
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return ids, nil
}
