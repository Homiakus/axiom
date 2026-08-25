package adgo

import "time"

// Clock is the semantic time source used by durable orchestration decisions.
// Implementations must be safe for concurrent use. Runtime defaults to the
// process wall clock; tests and simulations can inject a deterministic clock.
type Clock interface {
	Now() time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// WithClock overrides the semantic time source used by Runtime. Passing nil is
// ignored so option composition remains backwards compatible.
func WithClock(clock Clock) RuntimeOption {
	return func(r *Runtime) {
		if clock != nil {
			r.clock = clock
		}
	}
}

func (r *Runtime) now() time.Time {
	if r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock.Now().UTC()
}
