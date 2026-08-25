package runtime

// StoreDurability describes how strongly a Store persists committed writes.
// The level is deliberately independent from transaction support: atomicity
// and durability are separate capabilities.
type StoreDurability string

const (
	// StoreDurabilityEphemeral means state is process-local and is lost when the
	// process exits or crashes.
	StoreDurabilityEphemeral StoreDurability = "ephemeral"
	// StoreDurabilityBestEffort means writes reach a persistent backend but are
	// not synchronously forced to stable storage. Recent commits may be lost on
	// host or power failure with no configured upper bound.
	StoreDurabilityBestEffort StoreDurability = "best-effort"
	// StoreDurabilityBuffered means persistence is asynchronous with a bounded
	// application-configured flush interval. A crash may lose the most recent
	// buffered window.
	StoreDurabilityBuffered StoreDurability = "buffered"
	// StoreDurabilitySynchronous means a successful commit is synchronously
	// persisted by the backend before it is reported as complete.
	StoreDurabilitySynchronous StoreDurability = "synchronous"
)

// DurabilityProvider lets a Store declare its persistence semantics without
// conflating them with transaction support. Production validation uses this
// capability in addition to TransactionalStore.
type DurabilityProvider interface {
	Durability() StoreDurability
}
