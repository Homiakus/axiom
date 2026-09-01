package runtime

import (
	"testing"
	"time"

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
