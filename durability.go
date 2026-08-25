package axiom

import runtimepkg "github.com/Homiakus/axiom/internal/runtime"

// StoreDurability describes how strongly a Store persists committed writes.
type StoreDurability = runtimepkg.StoreDurability

// DurabilityProvider is implemented by stores that declare their persistence
// semantics. Custom production stores should implement this in addition to
// TransactionalStore.
type DurabilityProvider = runtimepkg.DurabilityProvider

// StoreTransaction is the transaction surface used by TransactionalStore.
type StoreTransaction = runtimepkg.StoreTransaction

// TransactionalStore exposes atomic store transactions.
type TransactionalStore = runtimepkg.TransactionalStore

const (
	StoreDurabilityEphemeral   StoreDurability = runtimepkg.StoreDurabilityEphemeral
	StoreDurabilityBestEffort  StoreDurability = runtimepkg.StoreDurabilityBestEffort
	StoreDurabilityBuffered    StoreDurability = runtimepkg.StoreDurabilityBuffered
	StoreDurabilitySynchronous StoreDurability = runtimepkg.StoreDurabilitySynchronous
)
