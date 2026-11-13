package codemode

import "testing"

// Expected values are the output of Node 24.20 stripTypeScriptTypes on
// `async function __runUser__(){\n<src>\n}`: an ambient `declare var/let/const`
// goes with its initializer (`declare const a = 1` is valid TS), instead of
// leaving a dangling `= 1;` behind.
func TestR32StripDeclareVariableWithInitializer(t *testing.T) {
	for _, tc := range [][2]string{
		{"declare const a = 2, b = 3;", "                           "},
		{"declare const a = (1+2) * 3;", "                            "},
		{"declare const a = { x: 1, y: [1,2] }, b: number;", "                                                "},
		{"declare const a = 1\ndeclare const b = 2\nlet q = 5", "                   \n                   \nlet q = 5"},
		{"declare let a = f(1, 2);", "                        "},
		{"declare var a = 1 + \n 2;", "                    \n   "},
		{"declare const a = x ? 1 : 2; let q = 1", "                             let q = 1"},
		{"declare const a: number = 2, b = 3\nlet r", "                                  \nlet r"},
		{"declare const a = 1 // c\nlet q", "                    // c\nlet q"},
		{"declare const a = `x${1}`\nlet z = 1", "                         \nlet z = 1"},
		{"declare const a = 1 /* c */ , b = 2;", "                                    "},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}

// Expected values are the output of Node 24.20 stripTypeScriptTypes on
// `async function __runUser__(){\n<src>\n}` (differential run over ~25k
// generated snippets). A `<` after the end of an asserted type is a
// comparison; a `!` ends a type scan; the commas and colons of an asserted
// type or of call type arguments are not those of a conditional's arrow body;
// `>=` is one operator, not the end of type arguments; and `<T>(x) => x` can
// start a statement after a block's `}`.
func TestR32StripMatchesNodeOnGenericsAndConditionals(t *testing.T) {
	for _, tc := range [][2]string{
		{"const v = a as Array<T> < c;",
			"const v = a             < c;"},
		{"const v = a as { k: T } < c;",
			"const v = a             < c;"},
		{"const v = (a satisfies Array<Bar<T>> < [] ? 1 : 2);",
			"const v = (a                         < [] ? 1 : 2);"},
		{"let q = c ? x(c) : y(y)! satisfies { k: T };",
			"let q = c ? x(c) : y(y)                    ;"},
		{"const x = c ? (d: U): T[] => [] as Map<K, V> : x;",
			"const x = c ? (d   )      => []              : x;"},
		{"let y = b || null ? (y: Foo): Array<T> => null satisfies Set<Map<K, V>> : (c: Foo): string => a as number;",
			"let y = b || null ? (y     )           => null                          : (c     )         => a          ;"},
		{"const r = c ? (x): T => f<A, B>(x) : y;",
			"const r = c ? (x)    => f      (x) : y;"},
		{"x = b < c >= d",
			"x = b < c >= d"},
		{"x = b < c > = d",
			"x = b       = d"},
		{"if (a) { b() }\n<T>(x: T) => x;",
			"if (a) { b() }\n   (x   ) => x;"},
		{"const d = b ? g : null ? g(y, []) : x! satisfies (a: T) => U ? 1 : y;",
			"const d = b ? g : null ? g(y, []) : x                        ? 1 : y;"},
	} {
		got, err := stripTypeScriptAnnotations(tc[0])
		if err != nil || got != tc[1] {
			t.Errorf("%q\n got  %q (err %v)\n want %q", tc[0], got, err, tc[1])
		}
	}
}

// Expected values are the output of Node 24.20 stripTypeScriptTypes: the `:`
// of a conditional expression is not a method return type just because a
// block follows on the next line.
func TestR32StripConditionalBeforeBlockIsUnchanged(t *testing.T) {
	for _, src := range []string{
		"c ? x(c) : y\n{ z() }",
		"q = c ? x(c) : y\n{ let z = 1 }",
		"a ? b(c) : d\n{ let z = 1 }",
		"let q = c ? x(c) : (y) => { return 1 }",
	} {
		got, err := stripTypeScriptAnnotations(src)
		if err != nil || got != src {
			t.Errorf("%q stripped to %q (err %v), want it unchanged", src, got, err)
		}
	}
}
