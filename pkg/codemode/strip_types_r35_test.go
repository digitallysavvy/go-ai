package codemode

import "testing"

// A return-typed arrow in the true branch of a conditional is recognised when
// the body holds `as`/`satisfies` types with commas (`Map<string, number>`)
// and an operator after the type. Expected strings are Node 24.20
// stripTypeScriptTypes output.
func TestStripReturnTypedArrowWithAsTypeCommasInConditional(t *testing.T) {
	for _, c := range [][2]string{
		{"let v = ok ? (y: any): T => (a) satisfies Map<string, number> >= a : [1, 2];", "let v = ok ? (y     )    => (a)                               >= a : [1, 2];"},
		{`const g = ok ? (n: unknown): R => g("s") as Record<string, Array<number>> - a + b : b;`, `const g = ok ? (n         )    => g("s")                                  - a + b : b;`},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}

// `case (x): y => 1:` is a case label whose expression is a return-typed
// arrow when the arrow body is followed by the label's `:`; without it the
// colon after `(x)` is the label's own. Expected strings are Node 24.20 output.
func TestStripReturnTypedArrowInCaseLabelMatchesNode(t *testing.T) {
	for _, c := range [][2]string{
		{"switch (v) { case (x: T): R => 1: break; }", "switch (v) { case (x   )    => 1: break; }"},
		{"switch (a) { case (x): y => 1: break; }", "switch (a) { case (x)    => 1: break; }"},
		{"switch (a) { case (x): y => 1: z }", "switch (a) { case (x)    => 1: z }"},
		{"switch (a) { case (x): y => 1; }", "switch (a) { case (x): y => 1; }"},
		{"switch (v) { case (x: number): string => a < b: return 1; default: return 2 }", "switch (v) { case (x        )         => a < b: return 1; default: return 2 }"},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}
