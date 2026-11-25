package jsdate

import (
	"testing"
	"time"
)

// Date#toISOString (Node): a year outside 0..9999 is written with a sign and
// six digits; a time outside +-8.64e15 ms is a RangeError.
func TestR94carryISOString(t *testing.T) {
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "+010000-01-01T00:00:00.000Z"},
		{time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC), "-000001-01-01T00:00:00.000Z"},
		{time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), "0000-01-01T00:00:00.000Z"},
		{time.Date(275760, 1, 1, 0, 0, 0, 0, time.UTC), "+275760-01-01T00:00:00.000Z"},
		{time.Date(2026, 11, 12, 1, 2, 3, 456_789_000, time.FixedZone("x", 3600)), "2026-11-12T00:02:03.456Z"},
		{time.UnixMilli(-8.64e15), "-271821-04-20T00:00:00.000Z"},
	} {
		if got, ok := ISOString(c.t); !ok || got != c.want {
			t.Errorf("ISOString(%v) = %q, %v; want %q", c.t, got, ok, c.want)
		}
	}
	for _, out := range []time.Time{time.UnixMilli(8.64e15 + 1), time.UnixMilli(-8.64e15 - 1)} {
		if got, ok := ISOString(out); ok {
			t.Errorf("ISOString(%v) = %q, want a RangeError", out, got)
		}
	}
}
