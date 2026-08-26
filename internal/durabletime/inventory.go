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
		Rationale:              "Memory admission lease expiry check; injected via WithMemoryAdmissionClock",
	},
	{
		Package:                "adgo",
		File:                   "admission.go",
		Function:               "MemoryAdmissionController.refill",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: true,
		Rationale:              "Rate limiter token bucket refill elapsed time; injected via WithMemoryAdmissionClock",
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
		Function:               "withOwnedFileLock",
		CallType:               "time.After",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "File lock acquisition timeout channel",
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

	// --- adgo/awaitable.go ---
	{
		Package:                "adgo",
		File:                   "awaitable.go",
		Function:               "AwaitExecution",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Local helper polling deadline for synchronous test/script awaits",
	},

	// --- adgo/cache.go ---
	{
		Package:                "adgo",
		File:                   "cache.go",
		Function:               "Cache",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Cache entry timestamp and file lock freshness",
	},
	{
		Package:                "adgo",
		File:                   "cache.go",
		Function:               "Cache",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "File cache lock staleness duration check",
	},
	{
		Package:                "adgo",
		File:                   "cache.go",
		Function:               "Cache",
		CallType:               "time.After",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "File cache lock acquisition timeout channel",
	},

	// --- adgo/clock.go ---
	{
		Package:                "adgo",
		File:                   "clock.go",
		Function:               "wallClock.Now",
		CallType:               "time.Now",
		Category:               CategoryDurableDecision,
		RequiresClockInjection: true,
		Rationale:              "Default process wall-clock fallback adapter for Clock interface",
	},

	// --- adgo/compensation_recovery.go ---
	{
		Package:                "adgo",
		File:                   "compensation_recovery.go",
		Function:               "CompensationRecovery",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Compensation execution duration metric telemetry",
	},

	// --- adgo/control.go ---
	{
		Package:                "adgo",
		File:                   "control.go",
		Function:               "Control",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Execution pause/resume request informational timestamp",
	},

	// --- adgo/diagnostics.go ---
	{
		Package:                "adgo",
		File:                   "diagnostics.go",
		Function:               "Diagnostics",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "External monitoring audit snapshot timestamp",
	},

	// --- adgo/engine.go ---
	{
		Package:                "adgo",
		File:                   "engine.go",
		Function:               "Engine.Advance",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Execution wall-clock total duration metric calculation",
	},
	{
		Package:                "adgo",
		File:                   "engine.go",
		Function:               "Engine.normalizeWorker",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Worker ID seed timestamp generation",
	},
	{
		Package:                "adgo",
		File:                   "engine.go",
		Function:               "Engine.executeWorkItem",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Activity execution start time for duration telemetry",
	},
	{
		Package:                "adgo",
		File:                   "engine.go",
		Function:               "Engine.executeWorkItem",
		CallType:               "time.NewTicker",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: false,
		Rationale:              "Worker background heartbeat ticker",
	},
	{
		Package:                "adgo",
		File:                   "engine.go",
		Function:               "Engine.executeWorkItem",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Activity execution elapsed duration for telemetry",
	},
	{
		Package:                "adgo",
		File:                   "engine.go",
		Function:               "sleepContext",
		CallType:               "time.NewTimer",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: false,
		Rationale:              "Context-aware sleep helper timer for worker loops",
	},

	// --- adgo/explain.go ---
	{
		Package:                "adgo",
		File:                   "explain.go",
		Function:               "Explain",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Workflow explanation generation timestamp",
	},

	// --- adgo/host.go ---
	{
		Package:                "adgo",
		File:                   "host.go",
		Function:               "Host.Start",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Host execution created informational timestamp",
	},

	// --- adgo/http_worker.go ---
	{
		Package:                "adgo",
		File:                   "http_worker.go",
		Function:               "HTTPWorker",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Remote worker HTTP polling request timestamp",
	},
	{
		Package:                "adgo",
		File:                   "http_worker.go",
		Function:               "HTTPWorker",
		CallType:               "time.NewTicker",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: false,
		Rationale:              "Remote worker heartbeat ticker",
	},
	{
		Package:                "adgo",
		File:                   "http_worker.go",
		Function:               "HTTPWorker",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Remote worker HTTP handler latency telemetry",
	},

	// --- adgo/lifecycle.go ---
	{
		Package:                "adgo",
		File:                   "lifecycle.go",
		Function:               "Lifecycle",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Workflow lifecycle duration metric calculation",
	},

	// --- adgo/local.go ---
	{
		Package:                "adgo",
		File:                   "local.go",
		Function:               "LocalRunner",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Local single-process execution created timestamp",
	},

	// --- adgo/migration.go ---
	{
		Package:                "adgo",
		File:                   "migration.go",
		Function:               "Migration",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Schema migration audit timestamp",
	},

	// --- adgo/monitor.go ---
	{
		Package:                "adgo",
		File:                   "monitor.go",
		Function:               "Monitor",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Monitor observability loop timestamp",
	},

	// --- adgo/pebble_store.go ---
	{
		Package:                "adgo",
		File:                   "pebble_store.go",
		Function:               "PebbleStore.Commit",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Informational UpdatedAt timestamp on persisted ADGO execution records",
	},

	// --- adgo/policy.go ---
	{
		Package:                "adgo",
		File:                   "policy.go",
		Function:               "PolicyDecider.Evaluate",
		CallType:               "time.Now",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: true,
		Rationale:              "Retry backoff deadline evaluation; injected via Engine clock (TIME-004)",
	},

	// --- adgo/repair.go ---
	{
		Package:                "adgo",
		File:                   "repair.go",
		Function:               "DependencyRepairPlanner.CheckStale",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: true,
		Rationale:              "Stale execution lease detection threshold; injected via Clock (TIME-004)",
	},

	// --- adgo/retention.go ---
	{
		Package:                "adgo",
		File:                   "retention.go",
		Function:               "RetentionManager.Purge",
		CallType:               "time.Now",
		Category:               CategoryDurableDecision,
		RequiresClockInjection: true,
		Rationale:              "Retention window expiry threshold comparison; injected via RetentionPolicy.Clock (TIME-004)",
	},

	// --- adgo/router.go & router_store.go ---
	{
		Package:                "adgo",
		File:                   "router.go",
		Function:               "AdaptiveRouter",
		CallType:               "time.Now",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: false,
		Rationale:              "Provider failure cooldown timestamp",
	},
	{
		Package:                "adgo",
		File:                   "router_store.go",
		Function:               "FileProviderHealthStore",
		CallType:               "time.Now",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "Provider health file lock freshness timestamp",
	},
	{
		Package:                "adgo",
		File:                   "router_store.go",
		Function:               "FileProviderHealthStore",
		CallType:               "time.Since",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "Provider health file lock mtime staleness calculation",
	},
	{
		Package:                "adgo",
		File:                   "router_store.go",
		Function:               "FileProviderHealthStore",
		CallType:               "time.After",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "Provider health file lock wait timeout channel",
	},

	// --- adgo/runtime.go ---
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

	// --- adgo/schedule.go & scheduler.go ---
	{
		Package:                "adgo",
		File:                   "schedule.go",
		Function:               "Schedule",
		CallType:               "time.Now",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: false,
		Rationale:              "Cron schedule state update timestamp and file lock freshness",
	},
	{
		Package:                "adgo",
		File:                   "schedule.go",
		Function:               "Schedule",
		CallType:               "time.Since",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "Schedule file lock mtime staleness calculation",
	},
	{
		Package:                "adgo",
		File:                   "schedule.go",
		Function:               "Schedule",
		CallType:               "time.After",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "Schedule file lock wait timeout channel",
	},
	{
		Package:                "adgo",
		File:                   "scheduler.go",
		Function:               "DefaultScheduler",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Candidate prioritization score age calculation",
	},

	// --- adgo/service.go ---
	{
		Package:                "adgo",
		File:                   "service.go",
		Function:               "WorkerService.Drain",
		CallType:               "time.NewTimer",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Worker service drain timeout timer",
	},

	// --- adgo/signal_safe.go ---
	{
		Package:                "adgo",
		File:                   "signal_safe.go",
		Function:               "SignalSafe",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Signal safe event arrival timestamp",
	},

	// --- adgo/speculation.go ---
	{
		Package:                "adgo",
		File:                   "speculation.go",
		Function:               "HedgeExecutor.Execute",
		CallType:               "time.NewTimer",
		Category:               CategoryRetryScheduleDeadline,
		RequiresClockInjection: true,
		Rationale:              "Speculative execution hedging delay timer; injected via SpeculationPolicy.Clock (TIME-004)",
	},
	{
		Package:                "adgo",
		File:                   "speculation.go",
		Function:               "Speculation",
		CallType:               "time.Now",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Variant execution start timestamp for duration measurement",
	},
	{
		Package:                "adgo",
		File:                   "speculation.go",
		Function:               "Speculation",
		CallType:               "time.Since",
		Category:               CategoryObservabilityElapsed,
		RequiresClockInjection: false,
		Rationale:              "Variant execution duration measurement for metrics",
	},

	// --- adgo/store.go ---
	{
		Package:                "adgo",
		File:                   "store.go",
		Function:               "MemoryStore",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Informational UpdatedAt timestamp on in-memory execution records",
	},
	{
		Package:                "adgo",
		File:                   "store.go",
		Function:               "FileStore",
		CallType:               "time.After",
		Category:               CategoryOSFreshnessBoundary,
		RequiresClockInjection: false,
		Rationale:              "File store execution lock wait timeout channel",
	},

	// --- adgo/time_travel.go ---
	{
		Package:                "adgo",
		File:                   "time_travel.go",
		Function:               "TimeTravel",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Replay history checkpoint informational timestamp",
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
		Rationale:              "Retry scheduler sleep timer; unified with Engine Clock (TIME-003)",
	},
	{
		Package:                "runtime",
		File:                   "retry_store.go",
		Function:               "RetryStore",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Retry task audit history timestamp",
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
	{
		Package:                "runtime",
		File:                   "concurrency_store.go",
		Function:               "ConcurrencyStore",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: false,
		Rationale:              "Concurrency limiter lease expiration timestamp",
	},
	{
		Package:                "runtime",
		File:                   "execution_api.go",
		Function:               "ExecutionAPI",
		CallType:               "time.Now",
		Category:               CategoryPersistedEventTimestamp,
		RequiresClockInjection: false,
		Rationale:              "Execution API operation timestamp",
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
