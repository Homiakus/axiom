package runtime

import (
	"context"
	"time"
)

// NextTimer returns the earliest not-yet-fired wall-clock timer for this
// execution using the same per-execution lock as the rest of the high-level
// Run API.
func (r *Run) NextTimer(ctx context.Context) (*TimerSchedule, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return r.engine.NextTimer(ctx, r.id)
}

// RunDueTimers executes every timer due at or before now for this execution.
// Pass a zero time to use the Engine clock.
func (r *Run) RunDueTimers(ctx context.Context, now time.Time) (int, error) {
	unlock, err := r.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	return r.engine.RunDueTimers(ctx, r.id, now)
}
