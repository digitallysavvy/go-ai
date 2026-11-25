package fx

import "testing"

// Character classes parse like minimatch's parseClass (the matcher behind
// Node's path.matchesGlob): an out-of-order range is dropped rather than
// failing the tool, a class with nothing left or a range ending in a posix
// class matches nothing, [a-a] is the literal a (so it keeps its case under
// nocaseMagicOnly), and [:print:] is \p{C} without negation. Expected values
// from node v24 path.matchesGlob on macOS (nocase).
func TestR48GlobClassesParseLikeMinimatch(t *testing.T) {
	for _, tc := range []struct {
		path, pattern string
		want          bool
	}{
		{"b", "[z-a]", false},
		{"b", "[z-ab]", true},
		{"ab", "a[z-a]", false},
		{"A", "[a-a]", false},
		{"a", "[a-a]", true},
		{"b", "[a-[b]", true},
		{"a", "[a-[:digit:]]", false},
		{"1", "[a-[:digit:]]", false},
		{"é", "[[:alpha:](-[:graph:]", false},
		{"\u0001", "[[:print:]]", true},
		{"a", "[[:print:]]", false},
		{"x", "[[:graph:]]", true},
		{"]", "[!]]", false},
		{"a", "[^a-]", false},
		// A never-matching class swallows the rest of the segment, even
		// after a literal, and negation does not make it match.
		{"a", "a[z-a]", false},
		{"b", "[!z-a]", false},
		// `!` negates only as the class's first character.
		{"!", "[a!]", true},
		{"b", "[a!]", false},
	} {
		g, err := compileGlob(tc.pattern, true, false)
		if err != nil {
			t.Errorf("compileGlob(%q): %v", tc.pattern, err)
			continue
		}
		if got := g.MatchString(tc.path); got != tc.want {
			t.Errorf("%q matches %q = %v; want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}
