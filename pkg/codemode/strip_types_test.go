package codemode

import (
	"context"
	"testing"
)

// strip mirrors calling stripTypeScriptAnnotations and failing the test on
// an unexpected error, to keep the table below concise.
func strip(t *testing.T, src string) string {
	t.Helper()
	got, err := stripTypeScriptAnnotations(src)
	if err != nil {
		t.Fatalf("stripTypeScriptAnnotations(%q) returned unexpected error: %v", src, err)
	}
	return got
}

// TestStripTypeScriptAnnotations_TSCoreTests ports the two TypeScript-syntax
// cases from the TypeScript SDK's own code-mode test suite
// (packages/code-mode/src/core.test.ts: "strips simple TypeScript
// annotations" and "strips interface and satisfies syntax from snippets").
// Those tests run the snippet end-to-end and assert on the returned value;
// here we assert on the stripped source directly and separately (in
// run_code_mode_test.go) via the full RunCodeMode path.
func TestStripTypeScriptAnnotations_TSCoreTests(t *testing.T) {
	t.Run("strips simple TypeScript annotations", func(t *testing.T) {
		got := strip(t, "const value: number = 7; return { value };")
		want := "const value = 7; return { value };"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("strips interface and satisfies syntax from snippets", func(t *testing.T) {
		src := "interface Item { value: number }\nconst item = { value: 12 } satisfies Item;\nreturn item;"
		got := strip(t, src)
		want := "\nconst item = { value: 12 } ;\nreturn item;"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// TestStripTypeScriptAnnotations_NeverTouchesPlainJS guards the highest
// priority invariant: this scanner must never alter source that doesn't
// contain TypeScript-only syntax, however superficially TS-adjacent it
// looks.
func TestStripTypeScriptAnnotations_NeverTouchesPlainJS(t *testing.T) {
	cases := map[string]string{
		"plain object literal":         "const o = { a: 1, b: 2 }; return o.a + o.b;",
		"property named type":          "const o = { type: 'x' }; return o.type;",
		"property named as":            "const o = { as: 1 }; return o.as;",
		"property named interface":     "const o = { interface: 1 }; return o.interface;",
		"string and comment content":   "const s = 'interface Foo {}'; // interface Bar { x: number }\nreturn s;",
		"as used as a variable name":   "const as = 1; return as;",
		"ternary comparison":           "const a=1,b=2,c=3,d=4; const r = a<b ? c : d; return r;",
		"comparison chain":             "const a=1,b=2,c=3; const r = a < b > c; return r;",
		"template literal with colon":  "const x = `a: ${1}`; return x;",
		"regex literal with colon":     "const re = /a:b/; return re.test('a:b');",
		"module.exports-style access":  "const module = { exports: {} }; module.exports = 1; return module.exports;",
		"optional chaining (real JS)":  "const x = {a:1}; return x?.a;",
		"private field (real JS)":      "class C { #x = 1; getX() { return this.#x; } } return new C().getX();",
		"static block (real JS)":       "class C { static { globalThis.ran = 1; } } return globalThis.ran;",
		"decorator syntax (untouched)": "@Foo class C {} return 1;",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := strip(t, src)
			if got != src {
				t.Fatalf("expected unchanged, got %q, want %q", got, src)
			}
		})
	}
}

// TestStripTypeScriptAnnotations_ErasableSyntax covers the erasable subset
// this stripper mirrors from Node's stripTypeScriptTypes (verified against
// Node 24 directly -- see the package doc comment in strip_types.go). Each
// case documents the expected stripped output; the important invariant
// under test is documented per-case with a follow-up functional check
// where the stripped output's meaning matters (RunCodeMode-level coverage
// lives in run_code_mode_test.go for the trickiest ones).
func TestStripTypeScriptAnnotations_ErasableSyntax(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"multiple declarators",
			"let a: number = 1, b: string = \"x\"; return a;",
			"let a = 1, b = \"x\"; return a;",
		},
		{
			"as assertion",
			"const x = (1 as number); return x;",
			"const x = (1 ); return x;",
		},
		{
			"chained as assertions",
			"const x = (1 as unknown) as string; return x;",
			"const x = (1 ) ; return x;",
		},
		{
			"as const",
			"const x = [1,2,3] as const; return x;",
			"const x = [1,2,3] ; return x;",
		},
		{
			"generic call",
			"function f(x){return x;} const x = f<number>(1); return x;",
			"function f(x){return x;} const x = f(1); return x;",
		},
		{
			"generic function declaration",
			"function f<T>(x: T): T { return x; } return f(1);",
			"function f(x) { return x; } return f(1);",
		},
		{
			"generic call inside comparison-like expression",
			"function f(x){return x;} const a=1,b=2; if (a<f<number>(b)) { return 1; } return 0;",
			"function f(x){return x;} const a=1,b=2; if (a<f(b)) { return 1; } return 0;",
		},
		{
			"non-null assertion",
			"const x = foo!.bar; return x;",
			"const x = foo.bar; return x;",
		},
		{
			"chained optional and non-null",
			"const x = foo?.bar!.baz; return x;",
			"const x = foo?.bar.baz; return x;",
		},
		{
			"definite assignment assertion",
			"let x!: number; x = 1; return x;",
			"let x; x = 1; return x;",
		},
		{
			"optional parameter",
			"function f(x?: number) { return x; } return f();",
			"function f(x) { return x; } return f();",
		},
		{
			"arrow function typed param",
			"const f = (x: number) => x; return f(1);",
			"const f = (x) => x; return f(1);",
		},
		{
			"arrow function typed param and return type",
			"const isFoo = (x: unknown): x is string => typeof x === \"string\"; return isFoo(1);",
			"const isFoo = (x) => typeof x === \"string\"; return isFoo(1);",
		},
		{
			"import type statement",
			"import type { Foo } from \"bar\";\nconst x = 1; return x;",
			"\nconst x = 1; return x;",
		},
		{
			"export type statement",
			"const x = 1; export type Foo = number;",
			"const x = 1; ",
		},
		{
			"readonly class field",
			"class C { readonly x: number = 1; } return 1;",
			// The "readonly" keyword is dropped outright (not blanked),
			// so the single space that separated it from "{" on one side
			// and "x" on the other both survive, landing adjacent -- a
			// harmless double space, not a single dropped one.
			"class C {  x = 1; } return 1;",
		},
		{
			"public method",
			"class C { public foo(): number { return 1; } } return new C().foo();",
			"class C {  foo() { return 1; } } return new C().foo();",
		},
		{
			"protected static field",
			"class C { protected static x: number = 1; } return 1;",
			"class C {  static x = 1; } return 1;",
		},
		{
			"override method",
			"class Base { foo(): number { return 1; } } class C extends Base { override foo(): number { return 2; } } return new C().foo();",
			"class Base { foo() { return 1; } } class C extends Base {  foo() { return 2; } } return new C().foo();",
		},
		{
			"abstract method erased, concrete method kept",
			"abstract class C { abstract foo(): void; bar() { return 1; } } return 1;",
			"class C { bar() { return 1; } } return 1;",
		},
		{
			"function overload signatures erased",
			"function f(x: number): number; function f(x: string): string; function f(x) { return x; } return f(1);",
			"  function f(x) { return x; } return f(1);",
		},
		{
			"class implements erased, generics stripped",
			"interface Foo<T> {} class C implements Foo<number> {} return 1;",
			" class C {} return 1;",
		},
		{
			"class extends keeps base, strips generics",
			"class Base<T> {} class C extends Base<number> {} return 1;",
			"class Base {} class C extends Base {} return 1;",
		},
		{
			"nested interface inside function",
			"function f() { interface Foo { x: number } const y: Foo = { x: 1 }; return y; } return f();",
			"function f() {  const y = { x: 1 }; return y; } return f();",
		},
		{
			"nested type alias inside function",
			"function f() { type T = number; const x: T = 1; return x; } return f();",
			"function f() {  const x = 1; return x; } return f();",
		},
		{
			"declare var erased",
			"declare var x: number; return 1;",
			" return 1;",
		},
		{
			"declare function erased",
			"declare function f(x: number): number; return 1;",
			" return 1;",
		},
		{
			"declare class erased",
			"declare class C { x: number; } return 1;",
			" return 1;",
		},
		{
			"new with call generics",
			"class Box<T> { constructor(value) { this.value = value; } } const b = new Box<number>(1); return 1;",
			"class Box { constructor(value) { this.value = value; } } const b = new Box(1); return 1;",
		},
		{
			"tuple type annotation",
			"const x: [number, string] = [1, \"a\"]; return x;",
			"const x = [1, \"a\"]; return x;",
		},
		{
			"union type parameter",
			"function f(x: number | string) { return x; } return f(1);",
			"function f(x) { return x; } return f(1);",
		},
		{
			"conditional type alias",
			"type T<X> = X extends string ? true : false; return 1;",
			" return 1;",
		},
		{
			"method literally named get (not an accessor)",
			"class C { get(): number { return 1; } } return new C().get();",
			"class C { get() { return 1; } } return new C().get();",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strip(t, c.src)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestStripTypeScriptAnnotations_UnsupportedSyntax covers TypeScript syntax
// that Node's stripTypeScriptTypes recognizes but rejects
// (ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX in strip-only mode): enums,
// namespaces, and constructor parameter properties.
func TestStripTypeScriptAnnotations_UnsupportedSyntax(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"enum", "enum Color { Red, Green } return Color.Red;"},
		{"const enum", "const enum Color { Red, Green } return Color.Red;"},
		{"namespace", "namespace NS { export const x = 1; } return NS.x;"},
		{"module with body", "module NS { export const x = 1; } return NS.x;"},
		{
			"constructor parameter property",
			"class Box { constructor(public value) {} } return 1;",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := stripTypeScriptAnnotations(c.src)
			if err == nil {
				t.Fatalf("expected an error for %q, got none", c.src)
			}
			if got != c.src {
				t.Fatalf("expected the original source unmodified on error, got %q, want %q", got, c.src)
			}
		})
	}
}

// TestStripTypeScriptAnnotations_LexerEdgeCases exercises the lexer's
// string/template-literal/regex/comment awareness directly, per the task's
// required coverage: generics vs comparisons, arrow functions with typed
// params, `as const`, template literals containing `:`, and object
// literals vs type annotations (the last two are also covered above).
func TestStripTypeScriptAnnotations_LexerEdgeCases(t *testing.T) {
	t.Run("nested template literal interpolation with object literal", func(t *testing.T) {
		src := "const x = `a${ {b: 1}.b }c`; return x;"
		got := strip(t, src)
		if got != src {
			t.Fatalf("template literal contents must be untouched: got %q, want %q", got, src)
		}
	})

	t.Run("regex literal containing generic-looking characters", func(t *testing.T) {
		src := "const re = /<[a-z]+>/; return re.test('<a>');"
		got := strip(t, src)
		if got != src {
			t.Fatalf("regex literal contents must be untouched: got %q, want %q", got, src)
		}
	})

	t.Run("division is not mistaken for a regex", func(t *testing.T) {
		src := "const a = 10; const b = 2; return a / b;"
		got := strip(t, src)
		if got != src {
			t.Fatalf("got %q, want %q", got, src)
		}
	})

	t.Run("comment containing type-looking syntax", func(t *testing.T) {
		src := "/* interface Foo { x: number } */ const x: number = 1; return x;"
		got := strip(t, src)
		want := "/* interface Foo { x: number } */ const x = 1; return x;"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// TestStripTypeScriptAnnotations_FunctionalViaRunCodeMode runs a handful of
// the trickier TypeScript snippets end-to-end through RunCodeMode (the same
// level TypeScript's own core.test.ts asserts at), confirming the stripped
// source is not just textually plausible but actually executes with the
// intended value.
func TestStripTypeScriptAnnotations_FunctionalViaRunCodeMode(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		js   string
		want interface{}
	}{
		{"typed variable", "const value: number = 7; return { value };", map[string]interface{}{"value": float64(7)}},
		{
			"interface and satisfies",
			"interface Item { value: number }\nconst item = { value: 12 } satisfies Item;\nreturn item;",
			map[string]interface{}{"value": float64(12)},
		},
		{"generic function", "function f<T>(x: T): T { return x; } return f(5);", float64(5)},
		{"call generics", "function f(x){return x;} return f<number>(9);", float64(9)},
		{"as const", "const x = [1,2,3] as const; return x[0];", float64(1)},
		{"non-null assertion", "const foo = { bar: 3 }; const x = foo!.bar; return x;", float64(3)},
		{"arrow typed params", "const add = (a: number, b: number): number => a + b; return add(2, 3);", float64(5)},
		{"class with modifiers", "class C { private x: number = 5; getX(): number { return this.x; } } return new C().getX();", float64(5)},
		{
			"comparison not mistaken for generics",
			"const a = 3, b = 1, c = 2; return (a < b) === (b > c);",
			true, // (3<1)===(1>2) -> false===false -> true; the point of this case is that it parses as comparisons at all, not the boolean value.
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RunCodeMode(ctx, RunInput{JS: c.js, Tools: ToolSet{}})
			if err != nil {
				t.Fatalf("RunCodeMode(%q) returned error: %v", c.js, err)
			}
			assertDeepEqual(t, got, c.want)
		})
	}
}
