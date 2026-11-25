package fx

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Expected values are from Node 24.20 path.matchesGlob (its bundled
// brace-expansion stops at EXPANSION_MAX alternatives and at
// EXPANSION_MAX_LENGTH characters in all).
func TestR50IntegratorBraceExpansionLengthLimits(t *testing.T) {
	x := strings.Repeat("x", 60000)
	alt := func(i int) string {
		return strings.NewReplacer("0", "a", "1", "b").Replace(fmt.Sprintf("%07b", i)) + x
	}
	el := func(n int) string { return fmt.Sprintf("%049d", n) }
	cases := []struct {
		name, pattern, path string
		want                bool
	}{
		// 128 alternatives of 60007 characters: only the first 66 fit in
		// 4,000,000.
		{"combine last kept", strings.Repeat("{a,b}", 7) + x, alt(65), true},
		{"combine first cut", strings.Repeat("{a,b}", 7) + x, alt(66), false},
		{"combine last", strings.Repeat("{a,b}", 7) + x, alt(127), false},
		// 99999 padded 49-character numbers: only the first 81632 fit.
		{"sequence last kept", "{" + strings.Repeat("0", 48) + "1..99999}", el(81632), true},
		{"sequence first cut", "{" + strings.Repeat("0", 48) + "1..99999}", el(81633), false},
		// A comma part's expansions are cut by length; the next part is
		// still expanded.
		{"values last kept", "{" + strings.Repeat("{a,b}", 7) + x + ",c}", alt(65), true},
		{"values first cut", "{" + strings.Repeat("{a,b}", 7) + x + ",c}", alt(66), false},
		{"values next part", "{" + strings.Repeat("{a,b}", 7) + x + ",c}", "c", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parts := minimatchBraceExpand(c.pattern)
			got := false
			for _, p := range parts {
				if p == c.path {
					got = true
					break
				}
			}
			if got != c.want {
				t.Fatalf("%d expansions; contains %.20q... = %v, want %v", len(parts), c.path, got, c.want)
			}
		})
	}
}

// A sequence stops at EXPANSION_MAX elements instead of counting to its end.
func TestR50IntegratorBraceSequenceStopsAtMax(t *testing.T) {
	done := make(chan int, 1)
	go func() { done <- len(minimatchBraceExpand("{1..99999999999}")) }()
	select {
	case n := <-done:
		if n != braceExpansionMax {
			t.Fatalf("expansions = %d, want %d", n, braceExpansionMax)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("{1..99999999999} did not stop at EXPANSION_MAX")
	}
}
