package policy

import (
	"testing"
	"time"
)

// TestR94integISOTimestampExpandedYears: the decision timestamp is
// Date#toISOString (jsdate), so years outside 0..9999 use the expanded form.
func TestR94integISOTimestampExpandedYears(t *testing.T) {
	for in, want := range map[time.Time]string{
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC):                   "+010000-01-01T00:00:00.000Z",
		time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC):                      "-000001-01-01T00:00:00.000Z",
		time.Date(2026, 11, 12, 1, 2, 3, 456789000, time.UTC):          "2026-11-12T01:02:03.456Z",
		time.Date(2026, 11, 12, 1, 2, 3, 0, time.FixedZone("x", 3600)): "2026-11-12T00:02:03.000Z",
	} {
		if got := isoTimestampMillis(in); got != want {
			t.Errorf("isoTimestampMillis(%v) = %q, want %q", in, got, want)
		}
	}
}
