package codemode

import "testing"

// `c ? (b) : d => e` is a conditional whose false branch is the arrow
// `d => e`. A `:` in a LATER statement (a typed arrow, a type annotation) must
// not make the scanner read `(b) : d` as an arrow with a return type; expected
// outputs were checked against Node's stripTypeScriptTypes.
func TestStripConditionalArrowFalseBranchIgnoresLaterStatements(t *testing.T) {
	for src, want := range map[string]string{
		"const x = a ? (b) : c => d\nlet fn: typeof import('x');":      "const x = a ? (b) : c => d\nlet fn                    ;",
		"const x = a ? (b) : c => d\nconst y = (z): d => e":            "const x = a ? (b) : c => d\nconst y = (z)    => e",
		"const x = a ? (b) : c => d\n\nconst y = (z): d => e":          "const x = a ? (b) : c => d\n\nconst y = (z)    => e",
		"const x = a ? (b) : (c): d => e":                              "const x = a ? (b) : (c)    => e",
		"const x = a ? (b: number): string => 'a' : c;":                "const x = a ? (b        )         => 'a' : c;",
	} {
		got, err := stripTypeScriptAnnotations(src)
		if err != nil || got != want {
			t.Errorf("%q\n got  %q (err %v)\n want %q", src, got, err, want)
		}
	}
}
