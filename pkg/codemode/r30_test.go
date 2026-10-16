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

// Expected values are the output of Node 24.20 stripTypeScriptTypes on
// `async function __runUser__(){\n<src>\n}`. With an accessibility/readonly/
// override modifier erased, a member that would now start with `*` or `[`
// gets a `;` over the first erased character, so it cannot continue the
// previous member's initializer.
func TestR30StripGuardsMembersStartingWithStarOrBracket(t *testing.T) {
	for _, tc := range [][2]string{
		{"class A { x = 1\n  private *m(){} } return A;", "class A { x = 1\n  ;       *m(){} } return A;"},
		{"class A { x = 1\n  public [Symbol.iterator]() {} } return A;", "class A { x = 1\n  ;      [Symbol.iterator]() {} } return A;"},
		{"class A { readonly [k] = 1 } return A;", "class A { ;        [k] = 1 } return A;"},
		{"class A { private async *m(){} } return A;", "class A { ;       async *m(){} } return A;"},
		{"class A { private async m(){} } return A;", "class A {         async m(){} } return A;"},
		{"class A { public static *m(){} } return A;", "class A {        static *m(){} } return A;"},
		// An anonymous class: `extends` is the heritage clause, not the name.
		{"const x = class extends B<T> {}; return x;", "const x = class extends B    {}; return x;"},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}

// Node 24.20 output: after `return` or a postfix ++/-- and a line break, a type
// alias, interface or declare statement starts a new statement and is erased.
func TestR30StripDeclarationAfterReturnOrPostfixIncrement(t *testing.T) {
	for _, tc := range [][2]string{
		{"return\ntype T = number\n(f)()", "return\n;              \n(f)()"},
		{"a++\ntype T = number\n(f)()", "a++\n;              \n(f)()"},
		{"a++\ndeclare const d: number\n(f)()", "a++\n;                      \n(f)()"},
		{"a++\ninterface I { a: number }\n(f)()", "a++\n;                        \n(f)()"},
		{"a++\ntype T = number\nlet z = 1", "a++\n               \nlet z = 1"},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}
