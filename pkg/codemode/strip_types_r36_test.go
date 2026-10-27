package codemode

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The stripper rescanned the enclosing span at every bracket, so deeply nested
// brackets and long flat runs cost quadratic time: a 256 KiB snippet could pin
// a CPU for minutes before the sandbox's own limits applied.
//
// A wall-clock cap alone is flaky under load (a loaded -race run can be many
// times slower), so the test checks how the cost grows instead: quadrupling
// the input must cost well under the 16x of a quadratic scan. Each size takes
// the fastest of several runs, the sizes are interleaved so load hits both
// alike, and the large size is long enough (40-140 KB) that the
// reverted quadratic code takes seconds at the small size and minutes at the
// large one. The absolute cap is only a backstop against a hang.
func TestStripTypeScriptBoundedOnAdversarialInput(t *testing.T) {
	cases := map[string]func(n int) string{
		"nested parens":    func(n int) string { return strings.Repeat("(", n) + "1" + strings.Repeat(")", n) },
		"nested objects":   func(n int) string { return strings.Repeat("({a: ", n) + "1" + strings.Repeat("})", n) },
		"nested templates": func(n int) string { return strings.Repeat("`${", n) + "1" + strings.Repeat("}`", n) },
		"comparison list":  func(n int) string { return strings.Repeat("a<b,", n) },
		"optional calls":   func(n int) string { return strings.Repeat("a?.b<T>?.(1),", n) },
		"arrow list":       func(n int) string { return strings.Repeat("(a) => (b) => (c) => 1,", n) },
	}
	sizes := map[string]int{
		"nested parens": 20000, "nested objects": 20000, "nested templates": 20000,
		"comparison list": 30000, "optional calls": 6000, "arrow list": 3000,
	}
	const (
		runs      = 3
		growthCap = 8.0 // linear is 4x, quadratic 16x
		noise     = 50 * time.Millisecond
		backstop  = 2 * time.Minute
	)
	for name, gen := range cases {
		large := gen(sizes[name])
		small := gen(sizes[name] / 4)
		bestSmall, bestLarge := time.Duration(1<<62), time.Duration(1<<62)
		for i := 0; i < runs; i++ {
			for _, c := range []struct {
				src  string
				best *time.Duration
			}{{small, &bestSmall}, {large, &bestLarge}} {
				start := time.Now()
				_, _ = stripTypeScriptAnnotations(c.src)
				d := time.Since(start)
				if d > backstop {
					t.Fatalf("%s: %d bytes took %v", name, len(c.src), d)
				}
				if d < *c.best {
					*c.best = d
				}
			}
		}
		t.Logf("%s: %d bytes %v, %d bytes %v", name, len(small), bestSmall, len(large), bestLarge)
		if limit := time.Duration(float64(bestSmall)*growthCap) + noise; bestLarge > limit {
			t.Errorf("%s: cost grows superlinearly: %d bytes took %v, %d bytes took %v (limit %v)",
				name, len(small), bestSmall, len(large), bestLarge, limit)
		}
	}
}

func TestStripTypeScriptLeavesTooDeeplyNestedSourceUnmodified(t *testing.T) {
	src := "const x: number = " + strings.Repeat("(", maxStripNesting+1) + "1" + strings.Repeat(")", maxStripNesting+1) + ";"
	got, err := stripTypeScriptAnnotations(src)
	if err == nil || got != src {
		t.Fatalf("too deep: err=%v changed=%v", err, got != src)
	}
	ok := "const x: number = " + strings.Repeat("(", 50) + "1" + strings.Repeat(")", 50) + ";"
	if got, err := stripTypeScriptAnnotations(ok); err != nil || got == ok {
		t.Fatalf("moderate nesting must still strip: err=%v", err)
	}
	tmpl := "const x: number = " + strings.Repeat("`${", maxStripNesting+1) + "1" + strings.Repeat("}`", maxStripNesting+1) + ";"
	if got, err := stripTypeScriptAnnotations(tmpl); err == nil || got != tmpl {
		t.Fatalf("deep templates: err=%v", err)
	}
}

// Source nested deeper than the stripper cap reaches the engine unmodified: a
// type annotation left in it is a syntax error there, and plain deep JS still
// runs; either way RunCodeMode returns promptly instead of hanging.
func TestRunCodeModeTooDeeplyNestedSourceFailsCleanly(t *testing.T) {
	deep := strings.Repeat("(", maxStripNesting+1) + "1" + strings.Repeat(")", maxStripNesting+1)
	for name, src := range map[string]string{
		"annotated": "const x: number = " + deep + "; return x;",
		"plain":     "const x = " + deep + "; return x;",
	} {
		start := time.Now()
		got, err := RunCodeMode(context.Background(), RunInput{JS: src, Tools: ToolSet{}})
		if d := time.Since(start); d > 10*time.Second {
			t.Fatalf("%s: took %v", name, d)
		}
		switch name {
		case "annotated":
			if err == nil {
				t.Fatalf("annotated: want an engine syntax error, got %v", got)
			}
		case "plain":
			if err != nil {
				t.Logf("plain: engine error %v", err)
			}
		}
	}
}
