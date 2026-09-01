package durabletime

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNegativeAdvance is returned when a ManualClock is asked to move backwards.
var ErrNegativeAdvance = errors.New("durabletime: negative clock advance")

// NowSource is the minimal semantic time source shared by durable orchestration
// boundaries. Implementations must be safe for concurrent use.
//
// NowSource intentionally does not require timer creation. Runtime packages may
// accept lightweight semantic clocks while richer code can opt into Clock.
type NowSource interface {
	Now() time.Time
}

// Clock extends NowSource with timer creation capability for orchestration code
// that owns waiting behavior.
type Clock interface {
	NowSource
	NewTimer(time.Duration) Timer
}

// Timer is the minimal timer surface used by durable orchestration code.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// SystemClock delegates to the process wall clock.
type SystemClock struct{}

// Now returns the current UTC wall-clock time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// NewTimer creates a wall-clock timer.
func (SystemClock) NewTimer(d time.Duration) Timer {
	return &systemTimer{timer: time.NewTimer(d)}
}

type systemTimer struct {
	timer *time.Timer
}

func (t *systemTimer) C() <-chan time.Time { return t.timer.C }
func (t *systemTimer) Stop() bool          { return t.timer.Stop() }

// ManualClock is a deterministic, manually advanced clock intended for tests,
// simulation and replay. Advancing the clock never sleeps.
type ManualClock struct {
	mu     sync.Mutex
	now    time.Time
	nextID uint64
	timers map[uint64]*manualTimer
}

// NewManualClock creates a manual clock at start. Zero values are allowed so
// callers can deliberately test zero-time behavior.
func NewManualClock(start time.Time) *ManualClock {
	return &ManualClock{now: start, timers: make(map[uint64]*manualTimer)}
}

// Now returns the current logical time.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer creates a deterministic timer. Non-positive durations fire
// immediately at the current logical time.
func (c *ManualClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	t := &manualTimer{
		clock:    c,
		id:       c.nextID,
		deadline: c.now.Add(d),
		ch:       make(chan time.Time, 1),
	}
	if d <= 0 {
		t.fired = true
		t.ch <- c.now
		return t
	}
	c.timers[t.id] = t
	return t
}

// Advance moves logical time forward and synchronously delivers every timer
// whose deadline has been reached. Timers with equal deadlines fire in
// creation order.
func (c *ManualClock) Advance(d time.Duration) error {
	if d < 0 {
		return ErrNegativeAdvance
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
	due := make([]*manualTimer, 0)
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.deadline.After(c.now) {
			due = append(due, t)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if due[i].deadline.Equal(due[j].deadline) {
			return due[i].id < due[j].id
		}
		return due[i].deadline.Before(due[j].deadline)
	})
	for _, t := range due {
		t.fired = true
		delete(c.timers, t.id)
		t.ch <- t.deadline
	}
	return nil
}

type manualTimer struct {
	clock    *ManualClock
	id       uint64
	deadline time.Time
	ch       chan time.Time
	stopped  bool
	fired    bool
}

func (t *manualTimer) C() <-chan time.Time { return t.ch }

func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	delete(t.clock.timers, t.id)
	return true
}
