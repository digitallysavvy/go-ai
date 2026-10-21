package codemode

import (
	"strings"
	"testing"
	"time"
)

// The stripper rescans the enclosing span at every bracket, so deeply nested
// brackets and long flat runs cost quadratic time: a 256 KiB snippet could pin
// a CPU for minutes before the sandbox's own limits applied.
func TestStripTypeScriptBoundedOnAdversarialInput(t *testing.T) {
	cases := map[string]string{
		"nested parens":    strings.Repeat("(", 20000) + "1" + strings.Repeat(")", 20000),
		"nested objects":   strings.Repeat("({a: ", 20000) + "1" + strings.Repeat("})", 20000),
		"nested templates": strings.Repeat("`${", 20000) + "1" + strings.Repeat("}`", 20000),
		"comparison list":  strings.Repeat("a<b,", 30000),
		"optional calls":   strings.Repeat("a?.b<T>?.(1),", 6000),
		"arrow list":       strings.Repeat("(a) => (b) => (c) => 1,", 3000),
	}
	for name, src := range cases {
		start := time.Now()
		_, _ = stripTypeScriptAnnotations(src)
		if d := time.Since(start); d > 8*time.Second {
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
	tmpl := "const x: number = " + strings.Repeat("`${", maxStripNesting+1) + "1" + strings.Repeat("}`", maxStripNesting+1) + ";"
	if got, err := stripTypeScriptAnnotations(tmpl); err == nil || got != tmpl {
		t.Fatalf("deep templates: err=%v", err)
	}
}
