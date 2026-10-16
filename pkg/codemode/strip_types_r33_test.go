package codemode

import "testing"

// Call arguments are never an arrow's parameter list: after `f(x)` the `:` of
// `c ? f(x) : y => 1` belongs to the conditional, not to a return type. The
// expected strings are Node 24.20 stripTypeScriptTypes output.
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
