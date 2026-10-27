package codemode

import (
	"strings"
	"testing"
	"time"
)

// fillTo repeats unit until the source reaches size bytes.
func fillTo(size int, unit string) string {
	return strings.Repeat(unit, size/len(unit))
}

// The stripper runs on model-written code before the sandbox and outside its
// timeout. Deeply nested brackets, long comparison lists and unterminated
// regex candidates cost quadratic time: a source of the default size limit
// could pin a CPU for tens of seconds.
//
// A wall-clock cap alone is flaky on loaded CI runners, so the test checks how
// the cost grows instead: quadrupling the input must cost well under the 16x of
// a quadratic scan. Each size takes the fastest of several interleaved runs so
// load hits both alike. The absolute cap is only a backstop against a hang.
func TestStripTypeScriptBoundedOnAdversarialInput(t *testing.T) {
	cases := map[string]func(size int) string{
		"nested parens": func(size int) string {
			return strings.Repeat("(", size/2) + "1" + strings.Repeat(")", size/2-1)
		},
		"nested objects": func(size int) string {
			return strings.Repeat("({a: ", size/7) + "1" + strings.Repeat("})", size/7)
		},
		"nested templates": func(size int) string {
			return strings.Repeat("`${", size/5) + "1" + strings.Repeat("}`", size/5)
		},
		"comparison list":     func(size int) string { return fillTo(size, "a<b,") },
		"optional calls":      func(size int) string { return fillTo(size, "a?.b<T>?.(1),") },
		"arrow list":          func(size int) string { return fillTo(size, "(a) => (b) => (c) => 1,") },
		"slash-class":         func(size int) string { return fillTo(size, "/[") },
		"assign-slash-class":  func(size int) string { return fillTo(size, "x=/[") },
		"paren-slash-class":   func(size int) string { return fillTo(size, "(/[") },
		"paren-slash":         func(size int) string { return fillTo(size, "(/") },
		"regex class newline": func(size int) string { return fillTo(size, "(/[\n") },
	}
	const (
		runs      = 3
		growthCap = 8.0 // linear is 4x, quadratic 16x
		noise     = 50 * time.Millisecond
		backstop  = 2 * time.Minute
	)
	for name, gen := range cases {
		large := gen(DefaultMaxSourceBytes)
		small := gen(DefaultMaxSourceBytes / 4)
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
}

// The regex memo must not change what is a regex literal.
func TestRegexMemoKeepsRegexLiterals(t *testing.T) {
	src := "const a: RegExp = /[/]x/g; const b = [/[/[/]/, /y/]; const c = (/[/ / 2);\nconst d: number = 1;"
	got, err := stripTypeScriptAnnotations(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "const a = /[/]x/g; const b = [/[/[/]/, /y/]; const c = (/[/ / 2);\nconst d = 1;"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// A comparison list is not consumed as type arguments.
func TestStripKeepsComparisonList(t *testing.T) {
	src := "const r = [" + strings.Repeat("a<b,", 100) + "a>b];"
	got, err := stripTypeScriptAnnotations(src)
	if err != nil || got != src {
		t.Fatalf("err=%v got %q", err, got)
	}
}
