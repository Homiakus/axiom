package memory

import "github.com/Homiakus/axiom/internal/runtime"

// Durability reports that the in-memory store is process-local.
func (*Store) Durability() runtime.StoreDurability {
	return runtime.StoreDurabilityEphemeral
}
