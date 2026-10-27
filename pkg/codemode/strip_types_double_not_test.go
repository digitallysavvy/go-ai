package codemode

import "testing"

// A double NOT `!!x` is two prefix operators, not a NOT followed by a
// non-null assertion: the stripper used to drop the second `!`, turning the
// boolean coercion into a negation. (v0.6.0 blanks stripped annotations
// to keep source positions, hence the padded expectations.)
func TestStripKeepsDoubleNot(t *testing.T) {
	cases := map[string]string{
		"const y = !!x;":           "const y = !!x;",
		"const y: boolean = !!x;":  "const y          = !!x;",
		"if (!!x) { y = 1 }":       "if (!!x) { y = 1 }",
		"return !!(a && b);":       "return !!(a && b);",
		"f(!!a, !!b)":              "f(!!a, !!b)",
		"const y = !!!x;":          "const y = !!!x;",
		"const z = !x!;":           "const z = !x ;",
		"const w = a!.b + !!c!.d;": "const w = a .b + !!c .d;",
	}
	for in, want := range cases {
		got, err := stripTypeScriptAnnotations(in)
		if err != nil || got != want {
			t.Errorf("strip(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
