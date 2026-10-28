package fx

import (
	"reflect"
	"strings"
	"testing"
)

// Expected values are from Node 24 (minimatch braceExpand and
// path.matchesGlob, macOS).
func TestR50BraceExpandLikeMinimatch(t *testing.T) {
	cases := []struct {
		pattern string
		want    []string
	}{
		{"a{1..3}", []string{"a1", "a2", "a3"}},
		{"a{01..10..3}", []string{"a01", "a04", "a07", "a10"}},
		{"{a..e..2}", []string{"a", "c", "e"}},
		{"{5..1}", []string{"5", "4", "3", "2", "1"}},
		{"{-2..2}", []string{"-2", "-1", "0", "1", "2"}},
		{"{-05..5..5}", []string{"-05", "000", "005"}},
		{"x{Z..b}", []string{"xZ", "x[", "x", "x]", "x^", "x_", "x`", "xa", "xb"}},
		{"${a,b}c", []string{"${a,b}c"}},
		{"{a},b}", []string{"a}", "b"}},
		{"{{a,b}}", []string{"{a}", "{b}"}},
		{"a{,b}c", []string{"ac", "abc"}},
		{"{,a}", []string{"a"}},
		{"{}x{a,b}", []string{"{}xa", "{}xb"}},
		{"{a,{b,c}}d", []string{"ad", "bd", "cd"}},
		{"{1..3}{a,b}", []string{"1a", "1b", "2a", "2b", "3a", "3b"}},
		{"{a,b", []string{"{a,b"}},
	}
	for _, c := range cases {
		if got := minimatchBraceExpand(c.pattern); !reflect.DeepEqual(got, c.want) {
			t.Errorf("braceExpand(%q) = %q, want %q", c.pattern, got, c.want)
		}
	}
	// brace-expansion stops at EXPANSION_MAX instead of expanding 2^20.
	if n := len(minimatchBraceExpand(strings.Repeat("{a,b}", 20))); n != 100000 {
		t.Errorf("expansions = %d, want 100000", n)
	}
}

func TestR50GlobMatchesLikeNode(t *testing.T) {
	cases := []struct {
		path, pattern string
		want          bool
	}{
		{"b", "a/../b", true},
		{"a/b", "a/./b", true},
		{"a/b", "a//b", true},
		{"x/y", "{**/x,x/y}", false}, // minimatch's second-phase merge
		{"x", "{**/x,x/y}", true},
		{"a/q/b", "a/**/../q/b", true},
		{"q/b", "**/../q/b", true},
		{"a2", "a{1..3}", true},
		{"a05", "a{01..10}", true},
		{"b.ts", "*.{js..ts}", false},
	}
	for _, c := range cases {
		g, err := compileGlob(c.pattern, true, false)
		if err != nil {
			t.Fatalf("compileGlob(%q): %v", c.pattern, err)
		}
		if got := g.MatchString(c.path); got != c.want {
			t.Errorf("matchesGlob(%q, %q) = %v, want %v", c.path, c.pattern, got, c.want)
		}
	}
	if _, err := compileGlob(strings.Repeat("a", 65537), true, false); err == nil || err.Error() != "pattern is too long" {
		t.Errorf("long pattern error = %v, want pattern is too long", err)
	}
}
