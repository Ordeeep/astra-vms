package app

import (
	"testing"
	"time"
)

func TestScheduleUsesBothRepeatedLocalHoursAndOvernightStartDay(t *testing.T) {
	w := []ScheduleWindow{{Day: 0, Start: 60, End: 120}}
	for _, utc := range []string{"2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"} {
		at, _ := time.Parse(time.RFC3339, utc)
		if !scheduled(at, "America/New_York", false, w) {
			t.Fatalf("repeated hour excluded: %s", utc)
		}
	}
	overnight := []ScheduleWindow{{Day: 1, Start: 1380, End: 60}}
	at, _ := time.Parse(time.RFC3339, "2026-10-20T00:30:00Z")
	if !scheduled(at, "UTC", false, overnight) {
		t.Fatal("overnight range lost")
	}
	at = at.Add(30 * time.Minute)
	if scheduled(at, "UTC", false, overnight) {
		t.Fatal("exclusive end included")
	}
	// The nonexistent 02:00–03:00 spring hour never becomes an instant.
	for minute := 0; minute < 180; minute++ {
		start, _ := time.Parse(time.RFC3339, "2026-03-08T06:00:00Z")
		if scheduled(start.Add(time.Duration(minute)*time.Minute), "America/New_York", false, []ScheduleWindow{{Day: 0, Start: 120, End: 180}}) {
			t.Fatal("nonexistent hour was fabricated")
		}
	}
}
