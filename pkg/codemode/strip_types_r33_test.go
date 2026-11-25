package codemode

import "testing"

// The `:` of `c ? f(x) : y => 1` belongs to the conditional, not to a return
// type of `(x)`. The expected strings are Node 24.20 stripTypeScriptTypes
// output.
func TestStripCallArgumentsAreNotArrowParams(t *testing.T) {
	for _, c := range [][2]string{
		{"let z = c ? f(x) : (y: T): R => 1;", "let z = c ? f(x) : (y   )    => 1;"},
		{"let z = c ? f(x) : y => 1;", "let z = c ? f(x) : y => 1;"},
		{"let z = c ? x.y(1) : async () => 1;", "let z = c ? x.y(1) : async () => 1;"},
		{"let z = c ? f(x)(y) : async (a: T) => a;", "let z = c ? f(x)(y) : async (a   ) => a;"},
		{"const r = ok ? new C() : async x => x;", "const r = ok ? new C() : async x => x;"},
		{"let z = c ? f(x) : async (a): Promise<number> => 1;", "let z = c ? f(x) : async (a)                  => 1;"},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}

// A `declare` statement's heritage clause can hold an object type, whose `{`
// is not the body's.
func TestStripDeclareClassWithObjectTypeHeritage(t *testing.T) {
	src := "declare class DC<T> extends B<{a: number}> { m(): string }\nlet v = 1;"
	want := "                                                          \nlet v = 1;"
	got, err := stripTypeScriptAnnotations(src)
	if err != nil || got != want {
		t.Errorf("got %q (%v), want %q", got, err, want)
	}
}

// Conditionals that hold arrows and comparisons: the `:` after an arrow's
// parenthesized body, after an optional call, and the `?` inside `a < b ? ...`
// are the conditional's own. Expected strings are Node 24.20 output.
func TestStripConditionalArrowsAndComparisonsMatchNode(t *testing.T) {
	for _, c := range [][2]string{
		{"const z = c ? () => ({}) : a => a;", "const z = c ? () => ({}) : a => a;"},
		{"const q = this.v ? () => (x) : async () => 1;", "const q = this.v ? () => (x) : async () => 1;"},
		{"const t = a < b ? y > (x) : z;", "const t = a < b ? y > (x) : z;"},
		{"const t = x ?? y < g() ? (x) > (x, y) : a > b;", "const t = x ?? y < g() ? (x) > (x, y) : a > b;"},
		{"const z = c ? f?.(1) : a => a;", "const z = c ? f?.(1) : a => a;"},
		{"const z = c ? (a: T): R => (a) : y => 1;", "const z = c ? (a   )    => (a) : y => 1;"},
		{"const z = c ? () => (x): R => x : y;", "const z = c ? () => (x)    => x : y;"},
		{"const o = { a: c ? () => ({}) : (a: T): R => a };", "const o = { a: c ? () => ({}) : (a   )    => a };"},
		{"const f = (a: T): R => (b: U): V => a;", "const f = (a   )    => (b   )    => a;"},
		{"let z = a ? b ? () => (x) : (y: T): R => y : (c: T): R => c;", "let z = a ? b ? () => (x) : (y   )    => y : (c   )    => c;"},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}

// The `:` after a `case` expression, a call or a computed key is the label's
// or the property's, not a return type. Expected strings are Node 24.20 output.
func TestStripCaseLabelsAndCallsBeforeArrowsMatchNode(t *testing.T) {
	for _, c := range [][2]string{
		{"switch (a) { case f(x): async () => 1; }", "switch (a) { case f(x): async () => 1; }"},
		{"switch (a) { case f(x): y => 1; }", "switch (a) { case f(x): y => 1; }"},
		{"switch (a) { case f(x): (y) => 1; }", "switch (a) { case f(x): (y) => 1; }"},
		{"switch (a) { case f?.(x): async () => 1; break; }", "switch (a) { case f?.(x): async () => 1; break; }"},
		{"switch (a) { case (x): async () => 1; }", "switch (a) { case (x): async () => 1; }"},
		{"const o = { [f(x)]: async () => 1 };", "const o = { [f(x)]: async () => 1 };"},
		{"const o = { a: f(x), b: async () => 1 };", "const o = { a: f(x), b: async () => 1 };"},
		{"let z = c ? d : f(x) ? async () => 1 : y => 1;", "let z = c ? d : f(x) ? async () => 1 : y => 1;"},
		{"switch (a) { case g(1) + (x): (y): R => 1; }", "switch (a) { case g(1) + (x): (y)    => 1; }"},
		{"switch (a) { case (x): (y: T): R => 1; }", "switch (a) { case (x): (y   )    => 1; }"},
		{"switch (a) { case 1: (y: T): R => 1; }", "switch (a) { case 1: (y   )    => 1; }"},
		{"switch (a) { case 1: { (y: T): R => 1; } }", "switch (a) { case 1: { (y   )    => 1; } }"},
		{"switch (a) { case 'a': case (x): async (y: T) => 1; break; default: (z) => 2; }", "switch (a) { case 'a': case (x): async (y   ) => 1; break; default: (z) => 2; }"},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}

// A `:` or a call directly inside `<...>` is no type argument list: these are
// comparisons. `keyof (A | B)`, `new () => T` and `import('x')` still are.
// Expected strings are Node 24.20 output.
func TestStripComparisonsWithColonOrCallBetweenAngles(t *testing.T) {
	for _, c := range [][2]string{
		{"const r = cb ? q < len : { c: c } > (x);", "const r = cb ? q < len : { c: c } > (x);"},
		{"foo([x, c] < c(b) > `${b}`);", "foo([x, c] < c(b) > `${b}`);"},
		{"const r = a < f(b) > (c);", "const r = a < f(b) > (c);"},
		{"const r = x ? a < b : c > (d);", "const r = x ? a < b : c > (d);"},
		{"const r = f<keyof (A | B)>(x);", "const r = f               (x);"},
		{"const r = f<new () => T>(x);", "const r = f             (x);"},
		{"const r = f<typeof import('x')>(x);", "const r = f                    (x);"},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}
