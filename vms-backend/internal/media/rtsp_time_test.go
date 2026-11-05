package media

import (
	"testing"
	"time"
)

func TestRTPClockDoesNotWrapAfterDayLongCapture(t *testing.T) {
	for _, duration := range []time.Duration{24 * time.Hour, 35 * time.Hour, 30 * 24 * time.Hour} {
		ticks := int64(duration/time.Second)*90000 + 45000
		if got := rtpDuration(ticks); got != duration+500*time.Millisecond {
			t.Fatalf("duration %v became %v", duration, got)
		}
	}
}
