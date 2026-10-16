package codemode

import (
	"strings"
	"testing"
)

// Node rejects `x as T < y` (type arguments that never close); the `}` of a
// surrounding block must not be mistaken for the closer of the `<`.
func TestR30StripUnterminatedTypeArgsInsideBlockIsRejected(t *testing.T) {
	for _, src := range []string{
		"{ let a = b as T < c; var b, c; return a; }",
		"if (1) { let a = b as T < c; }",
		"function g() { return b as T < c; }",
	} {
		got, _ := stripTypeScriptAnnotations(src)
		if got != src {
			t.Errorf("%q stripped to %q, want it returned unmodified", src, got)
		}
	}
	got, _ := stripTypeScriptAnnotations("{ let a = b as Array<number>; return a; }")
	if got != "{ let a = b"+strings.Repeat(" ", 17)+"; return a; }" {
		t.Errorf("valid type args stripped to %q", got)
	}
}
