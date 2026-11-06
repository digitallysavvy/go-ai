package harnessutil

import (
	"testing"
	"time"
)

// Values from node v24 Date.parse with TZ=UTC: the input ends at the first
// NUL, U+2028/U+2029 are word characters rather than white space, and a
// legacy UTC offset may reach Smi::kMaxValue (2^31-1 seconds), not 2^30-1.
func TestR48ParseJSDateNULLineSeparatorsAndOffsetLimit(t *testing.T) {
	const nan = int64(-1)
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"Jan 1 2000 10:00 +300000:00", -133279200000},
		{"Jan 1 2000 10:00 +596523:00", -1200762000000},
		{"Jan 1 2000 10:00 +596524:00", nan},
		{"2000\x00junk", 946684800000},
		{"Jan 1 2000\x00 10:00", 946684800000},
		{"(\x00) Oct 27 2026", nan},
		{"1 Jan 2000", nan},
		{"Jan Sept 1 2000", 946684800000},
		{"Jan 1 2000\n10:00\r", 946720800000},
	} {
		got, ok := parseJSDateIn(tc.in, time.UTC)
		if tc.want == nan {
			if ok {
				t.Errorf("%q = %d; want NaN", tc.in, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("%q = %d, %v; want %d", tc.in, got, ok, tc.want)
		}
	}
}
