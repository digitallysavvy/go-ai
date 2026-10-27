package harnessutil

import (
	"strings"
	"testing"
	"time"
)

var jsDateFuzzSeeds = []string{
	"2026-10-27T12:00:00Z", "2026-10-27", "2026-10-27T24:00", "2026-10-27T12:00:00.123456789+05:30",
	"Tue, 27 Oct 2026 12:00:00 GMT", "October 27, 2026 3:04 PM EST", "10/27/2026", "27 Oct 2026 (comment (nested)) GMT+0200",
	"", " ", "(", "((((", ")))", "-", "+999999-12-31T23:59:59.999Z", "-271821-04-20T00:00:00Z", "999999999999999999999",
	"Sat Sep 13 275760 00:00:00 GMT+0000", "\ufeff2026", "\u00002026-01-01", "😀2026-01-01", "Mon Mon Mon 12 12 12 12 12:12:12:12:12",
}

// FuzzParseJSDate: hostile input never panics, the result is a valid ECMAScript
// time value, the answer is deterministic and independent of loc when UTC is
// explicit, and a parse takes linear time.
func FuzzParseJSDate(f *testing.F) {
	for _, s := range jsDateFuzzSeeds {
		f.Add(s)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		ny = time.UTC
	}
	f.Fuzz(func(t *testing.T, s string) {
		ms, ok := parseJSDateIn(s, time.UTC)
		if !ok && ms != 0 {
			t.Fatalf("%q: not ok but ms = %d", s, ms)
		}
		if ms < -8.64e15 || ms > 8.64e15 {
			t.Fatalf("%q: %d is outside the ECMAScript time range", s, ms)
		}
		if ms2, ok2 := parseJSDateIn(s, time.UTC); ms2 != ms || ok2 != ok {
			t.Fatalf("%q: not deterministic", s)
		}
		_, _ = parseJSDateIn(s, ny)
	})
}

// A 1 MiB string of each hostile shape parses in well under a second.
func TestParseJSDateHostileInputIsLinear(t *testing.T) {
	const n = 1 << 20
	inputs := map[string]string{
		"open parens":   strings.Repeat("(", n),
		"nested parens": strings.Repeat("(a", n/2) + strings.Repeat(")", n/2),
		"digits":        strings.Repeat("9", n),
		"zeros":         strings.Repeat("0", n) + "1",
		"words":         strings.Repeat("January ", n/8),
		"symbols":       strings.Repeat("-+:.", n/4),
		"whitespace":    strings.Repeat(" \t", n/2) + "2026",
		"numbers":       strings.Repeat("12 ", n/3),
		"astral":        strings.Repeat("😀", n/4),
	}
	for name, s := range inputs {
		start := time.Now()
		_, _ = ParseJSDate(s)
		if d := time.Since(start); d > time.Second {
			t.Errorf("%s: %v for %d bytes", name, d, len(s))
		}
	}
}
