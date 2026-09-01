package adgo

import (
	"testing"
	"time"

	"github.com/Homiakus/axiom/internal/durabletime"
)

type adgoNowOnlyClock struct {
	at time.Time
}

func (c adgoNowOnlyClock) Now() time.Time { return c.at }

func acceptDurableNowSource(source durabletime.NowSource) time.Time {
	return source.Now()
}

func TestClockContractMatchesDurableNowSource(t *testing.T) {
	want := time.Date(2026, 9, 1, 18, 32, 0, 0, time.UTC)
	var clock Clock = adgoNowOnlyClock{at: want}
	if got := acceptDurableNowSource(clock); !got.Equal(want) {
		t.Fatalf("Now()=%v want=%v", got, want)
	}
}
