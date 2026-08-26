package durableserial

// SurfaceCategory classifies the durable serialized state surfaces across Axiom.
type SurfaceCategory string

const (
	CategoryCoreExecution     SurfaceCategory = "core_execution"
	CategoryCoreTaskHistory   SurfaceCategory = "core_task_history"
	CategoryADGOExecution     SurfaceCategory = "adgo_execution"
	CategoryADGOInbox         SurfaceCategory = "adgo_inbox"
	CategoryADGOLocking       SurfaceCategory = "adgo_locking"
	CategoryFlowStateOutbox   SurfaceCategory = "flow_state_outbox"
	CategoryScheduleRouter    SurfaceCategory = "schedule_router"
	CategoryAdmissionControl  SurfaceCategory = "admission_control"
	CategoryRetentionRepair   SurfaceCategory = "retention_repair"
	CategoryArtifactManifest  SurfaceCategory = "artifact_manifest"
)

// EncodingType specifies the serialization codec used by the durable surface.
type EncodingType string

const (
	EncodingJSON      EncodingType = "JSON"
	EncodingGob       EncodingType = "Gob"
	EncodingRawBinary EncodingType = "RawBinary"
	EncodingRawString EncodingType = "RawString"
	EncodingJSONOrGob EncodingType = "JSON (default) / Gob (opt-in)"
)

// CompatibilityPromise defines the backward/forward compatibility guarantee.
type CompatibilityPromise string

const (
	PromiseImmutableAppendOnly  CompatibilityPromise = "Immutable append-only; historical entries never rewritten"
	PromiseVersionedCAS         CompatibilityPromise = "Optimistic concurrency with monotonic version increment and CAS validation"
	PromiseFormatPinnedFailFast CompatibilityPromise = "Format/codec pinned in metadata; fail-fast fail-closed on mismatch"
	PromiseAtomicFileRename     CompatibilityPromise = "Crash-durable tempfile + fsync + atomic rename; stale lease reclamation"
	PromiseDeterministicOutbox  CompatibilityPromise = "Synchronous state+outbox batch commit with exactly-once idempotency"
	PromiseDeterministicDigest  CompatibilityPromise = "Strict semantic digest checking; rejects mismatched AST or module hash"
)

// SerializedSurfaceEntry records a machine-reviewable specification of a durable state surface.
type SerializedSurfaceEntry struct {
	SurfaceID            string
	Name                 string
	OwnerPackage         string
	Category             SurfaceCategory
	StorageMedium        string
	KeyPattern           string
	Encoding             EncodingType
	SchemaVersionField   string
	CompatibilityPromise CompatibilityPromise
	MigrationPath        string
	GoldenFixturePath    string
}

// Registry is the canonical machine-reviewable inventory of durable serialized surfaces in Axiom.
var Registry = []SerializedSurfaceEntry{
	// 1. Core Pebble Execution
	{
		SurfaceID:            "CORE-PEBBLE-EXECUTION",
		Name:                 "Core Pebble Execution State",
		OwnerPackage:         "internal/store/pebble",
		Category:             CategoryCoreExecution,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "exec/<execution_id>",
		Encoding:             EncodingJSONOrGob,
		SchemaVersionField:   "Execution.Version (uint64), meta/axiom-store-schema (\"1\")",
		CompatibilityPromise: PromiseFormatPinnedFailFast,
		MigrationPath:        "Schema version checked at Open; legacy unmarked stores auto-adopted after codec detection; incompatible formats fail fast.",
		GoldenFixturePath:    "testdata/compat/core_pebble_execution.json",
	},
	// 2. Core Pebble History
	{
		SurfaceID:            "CORE-PEBBLE-HISTORY",
		Name:                 "Core Pebble History Log",
		OwnerPackage:         "internal/store/pebble",
		Category:             CategoryCoreTaskHistory,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "hist/<execution_id>/<seq>",
		Encoding:             EncodingJSONOrGob,
		SchemaVersionField:   "HistoryEntry.Seq (int), meta/axiom-store-schema (\"1\")",
		CompatibilityPromise: PromiseImmutableAppendOnly,
		MigrationPath:        "Append-only sequential entries; replayed in sequence to reconstruct execution state.",
		GoldenFixturePath:    "testdata/compat/core_pebble_history.json",
	},
	// 3. Core Pebble Activity Task
	{
		SurfaceID:            "CORE-PEBBLE-TASK",
		Name:                 "Core Pebble Activity Task Record",
		OwnerPackage:         "internal/store/pebble",
		Category:             CategoryCoreTaskHistory,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "task/<execution_id>/<task_id>",
		Encoding:             EncodingJSONOrGob,
		SchemaVersionField:   "ActivityTask.Attempt (int), meta/axiom-store-schema (\"1\")",
		CompatibilityPromise: PromiseFormatPinnedFailFast,
		MigrationPath:        "State machine transitions with lease timeout and durable retry deadlines.",
		GoldenFixturePath:    "testdata/compat/core_pebble_task.json",
	},
	// 4. Core Pebble Task Dedup Index
	{
		SurfaceID:            "CORE-PEBBLE-TASK-DEDUP",
		Name:                 "Core Pebble Task Deduplication Index",
		OwnerPackage:         "internal/store/pebble",
		Category:             CategoryCoreTaskHistory,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "tdedup/<execution_id>/<rule>/<activity>/<idempotency_key>",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "N/A (TaskID string reference)",
		CompatibilityPromise: PromiseFormatPinnedFailFast,
		MigrationPath:        "Maps composite idempotency key to task ID to prevent duplicate activity enqueue.",
		GoldenFixturePath:    "testdata/compat/core_pebble_task_dedup.json",
	},
	// 5. ADGO FileStore Execution Commit
	{
		SurfaceID:            "ADGO-FILESTORE-COMMIT",
		Name:                 "ADGO FileStore Execution Commit",
		OwnerPackage:         "adgo",
		Category:             CategoryADGOExecution,
		StorageMedium:        "Filesystem Directory",
		KeyPattern:           "executions/<encoded_id>/commits/<version_20d>.json",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "Execution.Version (uint64), PlanDigest (sha256)",
		CompatibilityPromise: PromiseAtomicFileRename,
		MigrationPath:        "Each version is a complete immutable JSON file written via temp+fsync+rename; latest read via directory scan.",
		GoldenFixturePath:    "testdata/compat/adgo_filestore_commit.json",
	},
	// 6. ADGO FileStore Inbox Event
	{
		SurfaceID:            "ADGO-FILESTORE-INBOX",
		Name:                 "ADGO FileStore Inbox Event",
		OwnerPackage:         "adgo",
		Category:             CategoryADGOInbox,
		StorageMedium:        "Filesystem Directory",
		KeyPattern:           "executions/<encoded_id>/inbox/<encoded_event_id>.json",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "Event.ID (string), Event.At (time.Time)",
		CompatibilityPromise: PromiseAtomicFileRename,
		MigrationPath:        "Atomic write per event; deterministic listing and removal on AckInbox.",
		GoldenFixturePath:    "testdata/compat/adgo_filestore_inbox.json",
	},
	// 7. ADGO File Lock Record
	{
		SurfaceID:            "ADGO-FILE-LOCK",
		Name:                 "ADGO FileStore / Admission Ownership Lock",
		OwnerPackage:         "adgo",
		Category:             CategoryADGOLocking,
		StorageMedium:        "Filesystem File",
		KeyPattern:           "locks/<encoded_id>.lock",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "fileLockRecord (Owner, AcquiredAt, HeartbeatAt); fallback to legacy integer timestamp",
		CompatibilityPromise: PromiseAtomicFileRename,
		MigrationPath:        "Accepts structured JSON lock records with heartbeat; backwards-compatible reader supports legacy timestamp strings.",
		GoldenFixturePath:    "testdata/compat/adgo_file_lock.json",
	},
	// 8. ADGO Pebble Execution Latest Pointer
	{
		SurfaceID:            "ADGO-PEBBLE-LATEST",
		Name:                 "ADGO Pebble Execution Latest State",
		OwnerPackage:         "adgo",
		Category:             CategoryADGOExecution,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "adgo/e/<hash>/latest",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "Execution.Version (uint64), meta/adgo-store-schema (\"1\")",
		CompatibilityPromise: PromiseVersionedCAS,
		MigrationPath:        "Point lookup for latest committed execution; format marker verified at Open.",
		GoldenFixturePath:    "testdata/compat/adgo_pebble_latest.json",
	},
	// 9. ADGO Pebble Version Snapshot
	{
		SurfaceID:            "ADGO-PEBBLE-VERSION",
		Name:                 "ADGO Pebble Immutable Version Snapshot",
		OwnerPackage:         "adgo",
		Category:             CategoryADGOExecution,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "adgo/e/<hash>/v/<version_20d>",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "Execution.Version (uint64), meta/adgo-store-schema (\"1\")",
		CompatibilityPromise: PromiseImmutableAppendOnly,
		MigrationPath:        "Atomic version snapshots written concurrently with latest pointer.",
		GoldenFixturePath:    "testdata/compat/adgo_pebble_version.json",
	},
	// 10. ADGO Pebble Inbox Event
	{
		SurfaceID:            "ADGO-PEBBLE-INBOX",
		Name:                 "ADGO Pebble Inbox Event",
		OwnerPackage:         "adgo",
		Category:             CategoryADGOInbox,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "adgo/e/<hash>/inbox/<event_hash>",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "Event.ID (string), Event.At (time.Time)",
		CompatibilityPromise: PromiseFormatPinnedFailFast,
		MigrationPath:        "Deduplicated by event hash; sorted chronologically and deleted on AckInbox.",
		GoldenFixturePath:    "testdata/compat/adgo_pebble_inbox.json",
	},
	// 11. ADGO Pebble Execution Catalog Index
	{
		SurfaceID:            "ADGO-PEBBLE-CATALOG",
		Name:                 "ADGO Pebble Execution Catalog Index",
		OwnerPackage:         "adgo",
		Category:             CategoryADGOExecution,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "adgo/c/<hash>",
		Encoding:             EncodingRawString,
		SchemaVersionField:   "Execution ID raw string",
		CompatibilityPromise: PromiseFormatPinnedFailFast,
		MigrationPath:        "Enables ordered discovery and iteration across all execution IDs.",
		GoldenFixturePath:    "testdata/compat/adgo_pebble_catalog.txt",
	},
	// 12. Flow Pebble State Record
	{
		SurfaceID:            "FLOW-PEBBLE-STATE",
		Name:                 "Flow Pebble State Record",
		OwnerPackage:         "axiom",
		Category:             CategoryFlowStateOutbox,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "flow/state/<flow_name>/<id>",
		Encoding:             EncodingRawBinary,
		SchemaVersionField:   "User-defined typed state byte payload",
		CompatibilityPromise: PromiseDeterministicOutbox,
		MigrationPath:        "Updated synchronously in one Pebble batch along with history and outbox intents.",
		GoldenFixturePath:    "testdata/compat/flow_pebble_state.bin",
	},
	// 13. Flow Pebble History Record
	{
		SurfaceID:            "FLOW-PEBBLE-HISTORY",
		Name:                 "Flow Pebble History Record",
		OwnerPackage:         "axiom",
		Category:             CategoryFlowStateOutbox,
		StorageMedium:        "Pebble DB",
		KeyPattern:           "flow/hist/<flow_name>/<id>/<seq>",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "FlowHistoryEntry.Sequence (int)",
		CompatibilityPromise: PromiseImmutableAppendOnly,
		MigrationPath:        "Incremental append-only event sequence preserving effect execution records.",
		GoldenFixturePath:    "testdata/compat/flow_pebble_history.json",
	},
	// 14. Flow Outbox Intent Record
	{
		SurfaceID:            "FLOW-OUTBOX-INTENT",
		Name:                 "Flow Outbox Durable Effect Intent",
		OwnerPackage:         "axiom",
		Category:             CategoryFlowStateOutbox,
		StorageMedium:        "Pebble DB / FlowHistoryEntry",
		KeyPattern:           "flow/hist/<flow_name>/<id>/<seq> (type: durable_effect_intent)",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "durableEffectIntentRecord (EffectName, DeduplicationID)",
		CompatibilityPromise: PromiseDeterministicOutbox,
		MigrationPath:        "Persisted before side-effect execution; drained during recovery for exactly-once delivery.",
		GoldenFixturePath:    "testdata/compat/flow_outbox_intent.json",
	},
	// 15. ADGO Schedule Store Record
	{
		SurfaceID:            "ADGO-SCHEDULE-STORE",
		Name:                 "ADGO Schedule Store Record",
		OwnerPackage:         "adgo",
		Category:             CategoryScheduleRouter,
		StorageMedium:        "Memory / Durable Store",
		KeyPattern:           "adgo.Schedule struct",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "Schedule.Version (uint64)",
		CompatibilityPromise: PromiseVersionedCAS,
		MigrationPath:        "Monotonic versioning with CAS update protection for scheduled workflow triggers.",
		GoldenFixturePath:    "testdata/compat/adgo_schedule.json",
	},
	// 16. ADGO Router Health State Record
	{
		SurfaceID:            "ADGO-ROUTER-HEALTH",
		Name:                 "ADGO Router Worker Health State",
		OwnerPackage:         "adgo",
		Category:             CategoryScheduleRouter,
		StorageMedium:        "Memory / Health Store",
		KeyPattern:           "adgo.WorkerHealth struct",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "WorkerHealth.WorkerID (string)",
		CompatibilityPromise: PromiseVersionedCAS,
		MigrationPath:        "Tracks latency, failure rates, and circuit breaker cooldowns for remote worker pools.",
		GoldenFixturePath:    "testdata/compat/adgo_router_health.json",
	},
	// 17. ADGO Admission Controller State Record
	{
		SurfaceID:            "ADGO-ADMISSION-STATE",
		Name:                 "ADGO Admission Control State",
		OwnerPackage:         "adgo",
		Category:             CategoryAdmissionControl,
		StorageMedium:        "Memory / File Admission Store",
		KeyPattern:           "admissionSnapshot, permitRecord",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "PermitsInUse (int), TokenBalance (float64)",
		CompatibilityPromise: PromiseVersionedCAS,
		MigrationPath:        "Guards workflow concurrency limits and rate-limiting token refills.",
		GoldenFixturePath:    "testdata/compat/adgo_admission_state.json",
	},
	// 18. ADGO Retention & Repair Metadata
	{
		SurfaceID:            "ADGO-RETENTION-REPAIR",
		Name:                 "ADGO Retention Policy and Repair Plan",
		OwnerPackage:         "adgo",
		Category:             CategoryRetentionRepair,
		StorageMedium:        "Memory / Configuration",
		KeyPattern:           "RetentionPolicy, RepairPlan",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "RetentionPolicy.RetainVersions (int)",
		CompatibilityPromise: PromiseVersionedCAS,
		MigrationPath:        "Governs snapshot pruning policies and automatic consistency repair actions.",
		GoldenFixturePath:    "testdata/compat/adgo_retention_repair.json",
	},
	// 19. AXM Compiled Plan / Artifact Manifest
	{
		SurfaceID:            "AXM-PLAN-MANIFEST",
		Name:                 "AXM Compiled Plan & Artifact Manifest",
		OwnerPackage:         "model",
		Category:             CategoryArtifactManifest,
		StorageMedium:        "JSON / Byte Stream",
		KeyPattern:           "model.Plan, runtime.PlanDigest",
		Encoding:             EncodingJSON,
		SchemaVersionField:   "CompilerVersion (string), ModuleHash (sha256)",
		CompatibilityPromise: PromiseDeterministicDigest,
		MigrationPath:        "Replay rejects mismatched compiler version or plan digest.",
		GoldenFixturePath:    "testdata/compat/axm_plan_manifest.json",
	},
}
