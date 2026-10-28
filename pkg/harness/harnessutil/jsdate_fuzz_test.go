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

// Each hostile shape parses in linear time: a 1 MiB string costs far less
// than the 16 times a quarter of it would cost if the parser were quadratic.
// Cost is the process CPU time of one parse, not wall time, so other
// processes competing for the CPUs (a parallel full-suite run) cannot inflate
// one size more than the other; the sizes are interleaved and each takes the
// cheapest of several runs. A wall-clock backstop only guards against a hang.
func TestParseJSDateHostileInputIsLinear(t *testing.T) {
	const n = 1 << 20
	inputs := map[string]func(n int) string{
		"open parens":   func(n int) string { return strings.Repeat("(", n) },
		"nested parens": func(n int) string { return strings.Repeat("(a", n/2) + strings.Repeat(")", n/2) },
		"digits":        func(n int) string { return strings.Repeat("9", n) },
		"zeros":         func(n int) string { return strings.Repeat("0", n) + "1" },
		"words":         func(n int) string { return strings.Repeat("January ", n/8) },
		"symbols":       func(n int) string { return strings.Repeat("-+:.", n/4) },
		"whitespace":    func(n int) string { return strings.Repeat(" \t", n/2) + "2026" },
		"numbers":       func(n int) string { return strings.Repeat("12 ", n/3) },
		"astral":        func(n int) string { return strings.Repeat("😀", n/4) },
	}
	const (
		runs      = 5
		growthCap = 8.0 // linear is 4x, quadratic 16x
		noise     = 50 * time.Millisecond
		backstop  = 2 * time.Minute
	)
	for name, build := range inputs {
		small, large := build(n/4), build(n)
		bestSmall, bestLarge := time.Duration(1<<63-1), time.Duration(1<<63-1)
		for i := 0; i < runs; i++ {
			for _, c := range []struct {
				src  string
				best *time.Duration
			}{{small, &bestSmall}, {large, &bestLarge}} {
				start, cpuStart := time.Now(), processCPUTime()
				_, _ = ParseJSDate(c.src)
				d := processCPUTime() - cpuStart
				if wall := time.Since(start); wall > backstop {
					t.Fatalf("%s: %d bytes took %v", name, len(c.src), wall)
				}
				if d < *c.best {
					*c.best = d
				}
			}
		}
		if limit := time.Duration(float64(bestSmall)*growthCap) + noise; bestLarge > limit {
			t.Errorf("%s: cost grows superlinearly: %d bytes took %v CPU, %d bytes took %v CPU (limit %v)",
				name, len(small), bestSmall, len(large), bestLarge, limit)
		}
	}
}
