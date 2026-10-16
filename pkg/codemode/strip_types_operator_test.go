package codemode

import "testing"

// Expected outputs are Node 24 stripTypeScriptTypes results (snippet wrapped
// in `async function __runUser__(){` ... `}` as the run package does).
func TestStripTypeScriptAnnotationsOperatorAfterType(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"const x = a as number + 1;", "const x = a           + 1;"},
		{"const x = a as number / 2;", "const x = a           / 2;"},
		{"const x = a as string === b;", "const x = a           === b;"},
		{"const x = a as number && b;", "const x = a           && b;"},
		{"const x = a as number ?? b;", "const x = a           ?? b;"},
		{"const x = a as number ? 1 : 2;", "const x = a           ? 1 : 2;"},
		{"const x = a as number >> 2;", "const x = a           >> 2;"},
		{"const x = a as number < 3;", "const x = a           < 3;"},
		{"const x = a as any in b;", "const x = a        in b;"},
		{"const x = a as any instanceof B;", "const x = a        instanceof B;"},
		{"const x = a satisfies number / 2;", "const x = a                  / 2;"},
		{"const x = a as number[] / 2;", "const x = a             / 2;"},
		{"x += y as number * 2;", "x += y           * 2;"},
		{"let x = y++ as any;", "let x = y++       ;"},
		{"let x = y[0]++ as any as number;", "let x = y[0]++                 ;"},
		// Conditional types keep their `?`.
		{"let x: A extends B ? C : D = y;", "let x                      = y;"},
		// A statement boundary the erased construct provided is kept as `;`.
		{"x = y as T\n(z)", "x = y ;   \n(z)"},
		{"x = y satisfies T\n[1]", "x = y ;          \n[1]"},
		{"x = y as T\n`z`", "x = y ;   \n`z`"},
		{"x = y as T\n+z", "x = y     \n+z"},
		{"x = y as const\n(z)", "x = y         \n(z)"},
		{"type A = B\n(z)", ";         \n(z)"},
		{"interface I {}\n+z", ";             \n+z"},
		{"function f(): void;\n/z/g", ";                  \n/z/g"},
		{"type A = B\nz", "          \nz"},
		// A `declare` field is erased whole (Node 24.20).
		{"class A { declare x: number }", "class A {                   }"},
		{"class A { declare x: number\n y = 1 }", "class A {                  \n y = 1 }"},
		{"class A { declare static x: number; y = 1 }", "class A {                           y = 1 }"},
		// `override` in a class without a base class is rejected: unmodified.
		{"class A { override m() {} }", "class A { override m() {} }"},
	} {
		got, err := stripTypeScriptAnnotations(c.src)
		if err != nil || got != c.want {
			t.Errorf("%q\n got  %q (%v)\n want %q", c.src, got, err, c.want)
		}
	}
}
