package adgo

import (
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)

// Clock is the semantic time source used by durable orchestration decisions.
// Implementations must be safe for concurrent use. Runtime defaults to the
// process wall clock; tests and simulations can inject a deterministic clock.
type Clock interface {
	Now() time.Time
}

// Timer is the minimal timer surface used by durable orchestration code.
type Timer = durabletime.Timer

// TimerClock extends Clock with timer creation capability.
type TimerClock interface {
	Clock
	NewTimer(time.Duration) Timer
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }
func (wallClock) NewTimer(d time.Duration) Timer { return durabletime.SystemClock{}.NewTimer(d) }

// WithClock overrides the semantic time source used by Runtime. Passing nil is
// ignored so option composition remains backwards compatible.
func WithClock(clock Clock) RuntimeOption {
	return func(r *Runtime) {
		if clock != nil {
			r.clock = clock
		}
	}
}

// WithEngineClock overrides the semantic time source used by Engine. Passing nil is
// ignored so option composition remains backwards compatible.
func WithEngineClock(clock Clock) EngineOption {
	return func(e *Engine) {
		if clock != nil && e.runtime != nil {
			e.runtime.clock = clock
		}
	}
}

func (r *Runtime) now() time.Time {
	if r == nil || r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock.Now().UTC()
}

func (r *Runtime) newTimer(d time.Duration) Timer {
	if r == nil || r.clock == nil {
		return durabletime.SystemClock{}.NewTimer(d)
	}
	if tc, ok := r.clock.(durabletime.Clock); ok {
		return tc.NewTimer(d)
	}
	if tc, ok := r.clock.(interface{ NewTimer(time.Duration) Timer }); ok {
		return tc.NewTimer(d)
	}
	return durabletime.SystemClock{}.NewTimer(d)
}

func (e *Engine) now() time.Time {
	if e == nil || e.runtime == nil {
		return time.Now().UTC()
	}
	return e.runtime.now()
}

func (e *Engine) newTimer(d time.Duration) Timer {
	if e == nil || e.runtime == nil {
		return durabletime.SystemClock{}.NewTimer(d)
	}
	return e.runtime.newTimer(d)
}

