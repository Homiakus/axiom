package durabletime

import (
	"testing"
	"time"
)

type nowOnlySource struct {
	at time.Time
}

func (s nowOnlySource) Now() time.Time { return s.at }

var _ NowSource = SystemClock{}
var _ Clock = SystemClock{}
var _ NowSource = (*ManualClock)(nil)
var _ Clock = (*ManualClock)(nil)
var _ NowSource = nowOnlySource{}

func TestNowSourceDoesNotRequireTimerCapability(t *testing.T) {
	want := time.Date(2026, 9, 1, 18, 30, 0, 0, time.UTC)
	var source NowSource = nowOnlySource{at: want}
	if got := source.Now(); !got.Equal(want) {
		t.Fatalf("Now()=%v want=%v", got, want)
	}
}
