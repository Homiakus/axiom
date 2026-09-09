package runtime

import (
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/diag"
	"github.com/Homiakus/axiom/internal/durabletime"
)

type coreNowOnlyClock struct {
	at time.Time
}

func (c coreNowOnlyClock) Now() time.Time { return c.at }

func acceptDurableNowSource(source durabletime.NowSource) time.Time {
	return source.Now()
}

func TestClockContractMatchesDurableNowSource(t *testing.T) {
	want := time.Date(2026, 9, 1, 18, 31, 0, 0, time.UTC)
	var clock Clock = coreNowOnlyClock{at: want}
	if got := acceptDurableNowSource(clock); !got.Equal(want) {
		t.Fatalf("Now()=%v want=%v", got, want)
	}
}

func TestExhaustiveStatusTransitions(t *testing.T) {
	statuses := []Status{
		StatusStarted,
		StatusRunning,
		StatusWaiting,
		StatusCompleted,
		StatusFailed,
		StatusCanceled,
	}

	for _, from := range statuses {
		for _, to := range statuses {
			err := ValidateTransition(from, to)
			if from == to {
				if err != nil {
					t.Errorf("expected reflexive transition %s -> %s to be legal, got %v", from, to, err)
				}
				continue
			}
			if from.IsTerminal() {
				// Terminal state cannot transition anywhere else
				if err == nil {
					t.Errorf("expected terminal transition %s -> %s to be illegal", from, to)
				}
				if diagErr, ok := err.(diag.Error); !ok || diagErr.Code != "AX407" {
					t.Errorf("expected AX407 error for terminal transition %s -> %s, got %#v", from, to, err)
				}
			} else {
				// Non-terminal states
				if !from.CanTransitionTo(to) && err == nil {
					t.Errorf("expected illegal transition %s -> %s to fail", from, to)
				}
			}
		}
	}
}

