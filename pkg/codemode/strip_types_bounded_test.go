package codemode

import (
	"strings"
	"testing"
)

// A comparison list is not consumed as type arguments.
func TestStripKeepsComparisonList(t *testing.T) {
	src := "const r = [" + strings.Repeat("a<b,", 100) + "a>b];"
	got, err := stripTypeScriptAnnotations(src)
	if err != nil || got != src {
		t.Fatalf("err=%v got %q", err, got)
	}
}
