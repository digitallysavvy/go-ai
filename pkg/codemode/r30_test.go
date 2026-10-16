package codemode

import (
	"strconv"
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

// Node 24.20 keeps the prefix NOT in `await !x` and `for (k of !x)`.
func TestR30StripKeepsPrefixNotAfterAwaitAndOf(t *testing.T) {
	for _, tc := range [][2]string{
		{"for (const k of !(a as T)) {}", "for (const k of !(a     )) {}"},
		{"await !(a as T);", "await !(a     );"},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}

// rt decodes <hex> markers into runes so the cases stay readable.
func rt(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			if end := strings.IndexByte(s[i:], '>'); end > 0 {
				if v, err := strconv.ParseInt(s[i+1:i+end], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += end
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Expected values are Node 24.20 stripTypeScriptTypes output on
// `async function __runUser__(){\n<src>\n}`: identifiers may hold non-ASCII
// letters, any ECMAScript whitespace separates tokens, and an erased
// character keeps its UTF-8 length (two bytes: no-break space, three: en
// space, four: space and BOM).
func TestR30StripUnicodeIdentifiersAndWhitespace(t *testing.T) {
	for _, tc := range [][2]string{
		{rt("const café: number = 1; return café;"), rt("const café         = 1; return café;")},
		{rt("function 変数(a: number): number { return a; } return 変数;"), rt("function 変数(a        )         { return a; } return 変数;")},
		{rt("type café = number; return 1;"), rt("        <a0>           return 1;")},
		{rt("const<a0>a:<a0>number<a0>=<a0>1;<a0>return a;"), rt("const<a0>a <a0>      <a0>=<a0>1;<a0>return a;")},
		{rt("const<3000>a:<3000>number = 1; return a;"), rt("const<3000>a <3000>       = 1; return a;")},
		{rt("<feff>const a: number = 1; return a;"), rt("<feff>const a         = 1; return a;")},
		{rt("const x = y<a0>as<a0>number; return x;"), rt("const x = y<a0>  <a0>      ; return x;")},
		{rt("let x: \"<1f600>\" = 1;"), rt("let x    <feff>  = 1;")},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}

// An erased arrow return type that spans lines must not leave a line break
// between `)` and `=>` (a JavaScript syntax error). Node 24.20 writes the `)`
// over the last character of the erased type.
func TestR30StripArrowReturnTypeSpanningLines(t *testing.T) {
	for _, tc := range [][2]string{
		{"const f = async (x: string): Promise<{\n  a: number;\n}> => ({ a: 1 }); return f;", "const f = async (x                    \n            \n ) => ({ a: 1 }); return f;"},
		{"const f = (x: string): Promise<{\n a: number }> => 1; return f;", "const f = (x                    \n            ) => 1; return f;"},
		{"const f = (x: string)\n: number => 1; return f;", "const f = (x         \n       ) => 1; return f;"},
		{"const f = <T>(x: T): Promise<{\n a: T }> => 1; return f;", "const f =    (x               \n       ) => 1; return f;"},
		// A line break before `)` or in a function (not arrow) leaves it in place.
		{"const f = (x: string,\n y: number): number => 1; return f;", "const f = (x        ,\n y        )         => 1; return f;"},
		{"function g(x: number): {\n  a: number;\n} { return { a: x }; } return g;", "function g(x        )   \n            \n  { return { a: x }; } return g;"},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}

// Node 24.20 keeps the `<T>` of `new C<T>` when the next token continues a
// comparison (`+`, `-`, a bracket); it strips it before `;`, a line break or
// `(`.
func TestR30StripNewTypeArgsFollowTheComparisonRule(t *testing.T) {
	for _, tc := range [][2]string{
		{"x = new C<T> + a;", "x = new C<T> + a;"},
		{"x = new C<T>[0];", "x = new C<T>[0];"},
		{"x = new C<T>;", "x = new C   ;"},
		{"x = new C<T>\nfoo();", "x = new C   \nfoo();"},
		{"x = new C<T>();", "x = new C   ();"},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}
