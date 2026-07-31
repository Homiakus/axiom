package runtime

import (
	"context"
	"fmt"
)

// StateMap returns an isolated map representation without the JSON round-trip
// required by State's typed decoding path.
func (r *Run) StateMap(ctx context.Context) (map[string]map[string]any, Status, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, "", err
	}
	defer unlock()

	result, err := r.engine.Query(ctx, r.id, "state")
	if err != nil {
		return nil, "", err
	}
	contexts, ok := result["context"].(map[string]map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("axiom: unexpected state representation")
	}
	status, ok := result["status"].(Status)
	if !ok {
		return nil, "", fmt.Errorf("axiom: unexpected status representation")
	}
	return cloneContext(contexts), status, nil
}
