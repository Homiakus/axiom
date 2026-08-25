package axiom

// Durability reports that the built-in Flow store is process-local. It is
// intentionally not a DurableFlowStore because it cannot survive a crash.
func (*MemoryFlowStore) Durability() StoreDurability {
	return StoreDurabilityEphemeral
}
