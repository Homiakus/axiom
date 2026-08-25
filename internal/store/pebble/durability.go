package pebble

import "github.com/Homiakus/axiom/internal/runtime"

// Durability reports the persistence guarantee configured for this store.
// Transaction atomicity and durability are intentionally reported separately.
func (s *Store) Durability() runtime.StoreDurability {
	if s == nil {
		return runtime.StoreDurabilityEphemeral
	}
	if s.sync {
		return runtime.StoreDurabilitySynchronous
	}
	if s.flushEvery > 0 {
		return runtime.StoreDurabilityBuffered
	}
	return runtime.StoreDurabilityBestEffort
}
