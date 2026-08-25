package durabletime

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestManualClockTimerBeforeDeadlineDoesNotFire(t *testing.T) {
	start := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	timer := clock.NewTimer(10 * time.Second)

	if err := clock.Advance(9 * time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}
	select {
	case <-timer.C():
		t.Fatal("timer fired before deadline")
	default:
	}
}

func TestManualClockTimerFiresAtExactDeadline(t *testing.T) {
	start := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	timer := clock.NewTimer(10 * time.Second)

	if err := clock.Advance(10 * time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}
	select {
	case got := <-timer.C():
		want := start.Add(10 * time.Second)
		if !got.Equal(want) {
			t.Fatalf("fire time = %v, want %v", got, want)
		}
	default:
		t.Fatal("timer did not fire at deadline")
	}
}

func TestManualClockEqualDeadlineTimersFireInCreationOrder(t *testing.T) {
	start := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	clock := NewManualClock(start)
	first := clock.NewTimer(time.Second)
	second := clock.NewTimer(time.Second)
	third := clock.NewTimer(time.Second)

	if err := clock.Advance(time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}

	for i, timer := range []Timer{first, second, third} {
		select {
		case got := <-timer.C():
			if !got.Equal(start.Add(time.Second)) {
				t.Fatalf("timer %d fire time = %v", i, got)
			}
		default:
			t.Fatalf("timer %d did not fire", i)
		}
	}
}

func TestManualClockStoppedTimerNeverFires(t *testing.T) {
	clock := NewManualClock(time.Unix(0, 0).UTC())
	timer := clock.NewTimer(time.Second)
	if !timer.Stop() {
		t.Fatal("first Stop returned false")
	}
	if timer.Stop() {
		t.Fatal("second Stop returned true")
	}
	if err := clock.Advance(2 * time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}
	select {
	case <-timer.C():
		t.Fatal("stopped timer fired")
	default:
	}
}

func TestManualClockImmediateTimer(t *testing.T) {
	start := time.Unix(42, 0).UTC()
	clock := NewManualClock(start)
	for _, d := range []time.Duration{0, -time.Second} {
		timer := clock.NewTimer(d)
		select {
		case got := <-timer.C():
			if !got.Equal(start) {
				t.Fatalf("duration %v fired at %v, want %v", d, got, start)
			}
		default:
			t.Fatalf("duration %v did not fire immediately", d)
		}
	}
}

func TestManualClockRejectsNegativeAdvance(t *testing.T) {
	clock := NewManualClock(time.Unix(0, 0).UTC())
	before := clock.Now()
	if err := clock.Advance(-time.Nanosecond); !errors.Is(err, ErrNegativeAdvance) {
		t.Fatalf("error = %v, want %v", err, ErrNegativeAdvance)
	}
	if got := clock.Now(); !got.Equal(before) {
		t.Fatalf("clock moved backwards: got %v want %v", got, before)
	}
}

func TestManualClockConcurrentNow(t *testing.T) {
	clock := NewManualClock(time.Unix(0, 0).UTC())
	const readers = 32
	const reads = 100
	var wg sync.WaitGroup
	wg.Add(readers)
	for range readers {
		go func() {
			defer wg.Done()
			for range reads {
				_ = clock.Now()
			}
		}()
	}
	for range reads {
		if err := clock.Advance(time.Nanosecond); err != nil {
			t.Fatalf("advance: %v", err)
		}
	}
	wg.Wait()
}
