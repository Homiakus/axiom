package axiom

import (
	"context"

	runtimepkg "github.com/Homiakus/axiom/internal/runtime"
)

// ReplayFromHistoryContext reconstructs execution state from history and
// stops promptly when ctx is canceled.
func ReplayFromHistoryContext(ctx context.Context, module *Module, history []HistoryEntry) (*Execution, error) {
	return runtimepkg.ReplayFromHistoryContext(ctx, module, history)
}
