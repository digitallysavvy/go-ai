package jsdate

import (
	"testing"
	"time"
)

// V8 MakeDay is NaN outside years -1,000,000..1,000,000, so Date.parse of a
// far year is NaN even where the int64 millisecond arithmetic would wrap
// back into the valid time range (year 584282229 does).
func TestR93carryFarYearsAreNaN(t *testing.T) {
	for _, s := range []string{"1/1/584282229", "Jan 1 584282230", "1/1/1000001"} {
		if ms, ok := parseIn(s, time.UTC); ok {
			t.Errorf("Parse(%q) = %d, want NaN", s, ms)
		}
	}
	if _, ok := parseIn("1/1/275761", time.UTC); ok {
		t.Error("1/1/275761 is beyond the time range, want NaN")
	}
	if ms, ok := parseIn("1/1/2000", time.UTC); !ok || ms != 946684800000 {
		t.Errorf("1/1/2000 = %d %v", ms, ok)
	}
}
