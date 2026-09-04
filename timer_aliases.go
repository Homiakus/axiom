package axiom

import runtimepkg "github.com/Homiakus/axiom/internal/runtime"

type (
	// TimerSchedule is one executable timer trigger resolved for an execution.
	TimerSchedule = runtimepkg.TimerSchedule
	// TimerExecutionSource returns execution IDs currently owned by a timer worker.
	TimerExecutionSource = runtimepkg.TimerExecutionSource
	// TimerWorkerOptions configures timer worker polling and error buffering.
	TimerWorkerOptions = runtimepkg.TimerWorkerOptions
)
