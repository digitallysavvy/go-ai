package codemode

import (
	"strings"
	"testing"
	"time"
)

// fillTo repeats unit until the source reaches the default size limit.
func fillTo(prefix, unit, suffix string) string {
	n := (DefaultMaxSourceBytes - len(prefix) - len(suffix)) / len(unit)
	return prefix + strings.Repeat(unit, n) + suffix
}

// The stripper runs on model-written code before the sandbox and outside its
// timeout. Deeply nested brackets, long comparison lists and unterminated
// regex candidates cost quadratic time: a source of the default size limit
// could pin a CPU for tens of seconds.
func TestStripTypeScriptBoundedOnAdversarialInput(t *testing.T) {
	half := DefaultMaxSourceBytes / 2
	cases := map[string]string{
		"nested parens":       strings.Repeat("(", half) + "1" + strings.Repeat(")", half-1),
		"nested objects":      strings.Repeat("({a: ", DefaultMaxSourceBytes/7) + "1" + strings.Repeat("})", DefaultMaxSourceBytes/7),
		"nested templates":    strings.Repeat("`${", DefaultMaxSourceBytes/5) + "1" + strings.Repeat("}`", DefaultMaxSourceBytes/5),
		"comparison list":     fillTo("", "a<b,", ""),
		"optional calls":      fillTo("", "a?.b<T>?.(1),", ""),
		"arrow list":          fillTo("", "(a) => (b) => (c) => 1,", ""),
		"slash-class":         fillTo("", "/[", ""),
		"assign-slash-class":  fillTo("", "x=/[", ""),
		"paren-slash-class":   fillTo("", "(/[", ""),
		"paren-slash":         fillTo("", "(/", ""),
		"regex class newline": fillTo("", "(/[\n", ""),
	}
	for name, src := range cases {
		start := time.Now()
		_, _ = stripTypeScriptAnnotations(src)
		d := time.Since(start)
		t.Logf("%s: %v", name, d)
		if d > 4*time.Second {
			t.Errorf("%s: %d bytes took %v", name, len(src), d)
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
