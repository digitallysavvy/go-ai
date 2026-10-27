package harnessutil

import (
	"testing"
	"time"
)

// A local time skipped by a spring-forward transition is read with the
// offset before the transition, and a repeated one takes the earlier
// instant, like V8 (ICU UCAL_TZ_LOCAL_FORMER). Values from node v24
// Date.parse with TZ set to each zone.
func TestR47ParseJSDateLocalTimeAtDSTTransitions(t *testing.T) {
	for _, tc := range []struct {
		zone, in string
		want     int64
	}{
		{"America/New_York", "2026-03-08T02:30:00", 1772955000000},
		{"America/New_York", "Mar 8 2026 02:30", 1772955000000},
		{"America/New_York", "2026-11-01T01:30:00", 1793511000000},
		{"Europe/London", "2026-03-29T01:30:00", 1774747800000},
		{"Europe/London", "2026-10-25T01:30:00", 1792888200000},
		{"Australia/Lord_Howe", "2026-10-04T02:15:00", 1791042300000},
		{"Australia/Lord_Howe", "2026-04-05T01:45:00", 1775313900000},
	} {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Skipf("no tzdata for %s: %v", tc.zone, err)
		}
		if got, ok := parseJSDateIn(tc.in, loc); !ok || got != tc.want {
			t.Errorf("%s %q = %d, %v; want %d", tc.zone, tc.in, got, ok, tc.want)
		}
	}
}
