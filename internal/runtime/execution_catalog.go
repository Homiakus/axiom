package runtime

import (
	"context"
	"errors"
	"sort"
)

// ErrExecutionCatalogUnsupported indicates that the configured Store does not
// provide durable execution enumeration. Callers must not interpret this as an
// empty catalog: doing so would hide pending durable work after process restart.
var ErrExecutionCatalogUnsupported = errors.New("axiom: execution catalog is unsupported by store")

// ExecutionCatalog is an optional Store capability for enumerating durable
// execution identities. It intentionally remains outside Store so existing
// custom stores keep source compatibility.
type ExecutionCatalog interface {
	ListExecutionIDs(context.Context) ([]string, error)
}

// ListExecutionIDs returns a stable sorted snapshot of execution ids visible to
// this Engine's durable store.
func (e *Engine) ListExecutionIDs(ctx context.Context) ([]string, error) {
	if e == nil || e.store == nil {
		return nil, ErrExecutionCatalogUnsupported
	}
	catalog, ok := e.store.(ExecutionCatalog)
	if !ok {
		return nil, ErrExecutionCatalogUnsupported
	}
	ids, err := catalog.ListExecutionIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out, nil
}

// ListExecutionIDs preserves the optional catalog capability through the retry
// decorator used by every Engine.
func (s *retryStore) ListExecutionIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.Store == nil {
		return nil, ErrExecutionCatalogUnsupported
	}
	catalog, ok := s.Store.(ExecutionCatalog)
	if !ok {
		return nil, ErrExecutionCatalogUnsupported
	}
	return catalog.ListExecutionIDs(ctx)
}
