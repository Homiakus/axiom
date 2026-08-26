package durabletime

// TimeUsageCategory classifies every use of time/clock primitives in Axiom.
type TimeUsageCategory string

const (
	// CategoryDurableDecision represents time used for deterministic, durable decisions
	// (e.g. execution transitions, retry eligibility, state machine decisions).
	// These MUST be injected via Clock and not call wall-clock directly.
	CategoryDurableDecision TimeUsageCategory = "durable_decision"

	// CategoryLeaseFencing represents lease, heartbeat, and fencing expiration checks.
	CategoryLeaseFencing TimeUsageCategory = "lease_fencing"

	// CategoryRetryScheduleDeadline represents backoff calculation and schedule delay deadlines.
	CategoryRetryScheduleDeadline TimeUsageCategory = "retry_schedule_deadline"

	// CategoryPersistedEventTimestamp represents informational audit / history log timestamps
	// (e.g. CreatedAt, UpdatedAt, CompletedAt) that do not alter state machine transition paths.
	CategoryPersistedEventTimestamp TimeUsageCategory = "persisted_event_timestamp"

	// CategoryObservabilityElapsed represents wall-clock duration measurement for metrics,
	// telemetry, and benchmarking (e.g. time.Since for latency recording).
	CategoryObservabilityElapsed TimeUsageCategory = "observability_elapsed"

	// CategoryOSFreshnessBoundary represents filesystem or OS-level freshness boundaries
	// (e.g. file lock mtime staleness checks against the local filesystem clock).
	CategoryOSFreshnessBoundary TimeUsageCategory = "os_freshness_boundary"

	// CategoryTestOnlyWait represents test simulation, sleeps, or polling in test harnesses.
	CategoryTestOnlyWait TimeUsageCategory = "test_only_wait"
)

// ClockUsageEntry records a classified clock/time call in the repository.
type ClockUsageEntry struct {
	Package                string
	File                   string
	Function               string
	CallType               string
	Category               TimeUsageCategory
	RequiresClockInjection bool
	Rationale              string
}

// Registry is the canonical checked inventory of time usages in production code.
var Registry = []ClockUsageEntry{
	// --- adgo/admission.go & admission_file.go ---
	{
		Package:                "adgo",
		File:                   "admission.go",
		Function:               "MemoryAdmissionController.Acquire",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: true,
		Rationale:              "Memory admission lease expiry check; target for TIME-002 injection",
	},
	{
		Package:                "adgo",
		File:                   "admission.go",
		Function:               "MemoryAdmissionController.refill",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: true,
		Rationale:              "Rate limiter token bucket refill elapsed time; target for TIME-002",
	},
	{
		Package:                "adgo",
		File:                   "admission_file.go",
		Function:               "FileAdmissionController.Acquire",
		CallType:               "time.Now",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "File lock timeout deadline tracking against filesystem mtime",
	},
	{
		Package:                "adgo",
		File:                   "file_lock.go",
		Function:               "withOwnedFileLock",
		CallType:               "time.Now",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "File lock initial acquisition timestamp recorded in lockfile",
	},
	{
		Package:                "adgo",
		File:                   "file_lock.go",
		Function:               "removeStaleFileLock",
		CallType:               "time.Since",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "Filesystem mtime staleness calculation for dead owner reclamation",
	},
	{
		Package:                "adgo",
		File:                   "file_lock_heartbeat.go",
		Function:               "startFileLockHeartbeat",
		CallType:               "time.NewTicker",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: false,
		Rationale:              "Background OS file mtime touch loop for active lock owner",
	},

	// --- adgo/runtime.go & execution ---
	{
		Package:                "adgo",
		File:                   "runtime.go",
		Function:               "NewRuntime",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Default worker ID generation using timestamp seed",
	},
	{
		Package:                "adgo",
		File:                   "runtime.go",
		Function:               "Runtime.ExecuteNode",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Duration elapsed timer for activity execution telemetry",
	},
	{
		Package:                "adgo",
		File:                   "runtime.go",
		Function:               "Runtime.ExecuteNode",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Calculates execution latency for node metrics",
	},
	{
		Package:                "adgo",
		File:                   "runtime.go",
		Function:               "Runtime.appendHistory",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Informational history entry timestamp",
	},

	// --- adgo/policy.go & retry/hedging ---
	{
		Package:                "adgo",
		File:                   "policy.go",
		Function:               "PolicyDecider.Evaluate",
		CallType:               "time.Now",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: true,
		Rationale:              "Retry backoff deadline evaluation; target for TIME-003",
	},
	{
		Package:                "adgo",
		File:                   "speculation.go",
		Function:               "HedgeExecutor.Execute",
		CallType:               "time.NewTimer",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: true,
		Rationale:              "Speculative execution hedging delay timer",
	},

	// --- adgo/retention.go & repair.go ---
	{
		Package:                "adgo",
		File:                   "retention.go",
		Function:               "RetentionManager.Purge",
		CallType:               "time.Now",
		Category:               CategoryDurableDecision,
		RequiresClockInjection: true,
		Rationale:              "Retention window expiry threshold comparison",
	},
	{
		Package:                "adgo",
		File:                   "repair.go",
		Function:               "DependencyRepairPlanner.CheckStale",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: true,
		Rationale:              "Stale execution lease detection threshold",
	},

	// --- internal/runtime (Core Engine) ---
	{
		Package:                "runtime",
		File:                   "types.go",
		Function:               "systemClock.Now",
		CallType:               "time.Now",
		Category:               CategoryDurableDecision,
		RequiresClockInjection: true,
		Rationale:              "Default systemClock adapter implementing Clock interface",
	},
	{
		Package:                "runtime",
		File:                   "retry_store.go",
		Function:               "DrainDueTasks",
		CallType:               "time.NewTimer",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: true,
		Rationale:              "Retry scheduler sleep timer; target for TIME-003 Clock unification",
	},
	{
		Package:                "runtime",
		File:                   "worker.go",
		Function:               "Worker.Run",
		CallType:               "time.NewTicker",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: false,
		Rationale:              "Background task queue poll interval ticker",
	},

	// --- internal/store/pebble & memory ---
	{
		Package:                "pebble",
		File:                   "store.go",
		Function:               "Store.flushLoop",
		CallType:               "time.NewTicker",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Background batch flush interval ticker for WithSyncEvery",
	},
	{
		Package:                "pebble",
		File:                   "transaction.go",
		Function:               "Transaction.Commit",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "CreatedAt and UpdatedAt informational timestamps on persisted tasks",
	},
	{
		Package:                "pebble",
		File:                   "transaction.go",
		Function:               "Transaction.AcquireLease",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: false,
		Rationale:              "Lease expiry timestamp calculation",
	},
	{
		Package:                "memory",
		File:                   "store.go",
		Function:               "Store.EnqueueTask",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "CreatedAt and UpdatedAt timestamps on in-memory tasks",
	},
	{
		Package:                "memory",
		File:                   "store.go",
		Function:               "Store.AcquireLease",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: false,
		Rationale:              "Lease expiry timestamp calculation in memory store",
	},

	// --- flow outbox ---
	{
		Package:                "axiom",
		File:                   "flow_outbox.go",
		Function:               "Outbox.Enqueue",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Outbox entry creation timestamp",
	},
}
