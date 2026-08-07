package axiom

import runtimepkg "github.com/Homiakus/axiom/internal/runtime"

type (
	Run         = runtimepkg.Run
	Status      = runtimepkg.Status
	TaskStatus  = runtimepkg.TaskStatus
	FactValue   = runtimepkg.FactValue
	Explanation = runtimepkg.Explanation
	EventNamer  = runtimepkg.EventNamer
)

const (
	StatusStarted   = runtimepkg.StatusStarted
	StatusRunning   = runtimepkg.StatusRunning
	StatusWaiting   = runtimepkg.StatusWaiting
	StatusCompleted = runtimepkg.StatusCompleted
	StatusFailed    = runtimepkg.StatusFailed
	StatusCanceled  = runtimepkg.StatusCanceled

	TaskPending    = runtimepkg.TaskPending
	TaskRunning    = runtimepkg.TaskRunning
	TaskCompleted  = runtimepkg.TaskCompleted
	TaskFailed     = runtimepkg.TaskFailed
	TaskSuperseded = runtimepkg.TaskSuperseded
)
