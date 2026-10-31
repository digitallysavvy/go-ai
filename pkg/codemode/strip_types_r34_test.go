package codemode

import "testing"

// JSDoc-style postfix `!` and `?.` are not type syntax: Node (24.20) reads
// `a<b!>(c)` and `x < items?.n > (n)` as comparison chains, stripping only the
// non-null `!`, instead of treating `<...>` as call type arguments. The
// expected strings are Node 24.20 stripTypeScriptTypes output.
func TestStripBangAndOptionalChainInAngleBracketsAreComparisons(t *testing.T) {
	for _, c := range [][2]string{
		{"a<b!>(c);", "a<b >(c);"},
		{"x < items?.n > (n);", "x < items?.n > (n);"},
		{"a<b!>(c)", "a<b >(c)"},
		{"f<a?.b>(x);", "f<a?.b>(x);"},
		{"f<a!>(x);", "f<a >(x);"},
		{"f<a!.b>(x);", "f<a .b>(x);"},
		{"f<a!, b>(x);", "f<a , b>(x);"},
		{"if (a < b! && c > (d)) {}", "if (a < b  && c > (d)) {}"},
		{"const r = a < b! > (c);", "const r = a < b  > (c);"},
		{"const r = a<b?.c>(d);", "const r = a<b?.c>(d);"},
		{"const q = x < y?.z > (w);", "const q = x < y?.z > (w);"},
		{"f<string!>(x)", "f<string >(x)"},
		{"new A<b!>(c)", "new A<b >(c)"},
		{"a<b!>`t`", "a<b >`t`"},
		{"a<b?.c>`t`", "a<b?.c>`t`"},
		{"f<(x!)>(1)", "f<(x )>(1)"},
		{"g<A<b!>>(1)", "g<A<b >>(1)"},
		{"a<b!>c", "a<b >c"},
		{"a<b!> c;", "a<b > c;"},
		{"a < b?.c > d;", "a < b?.c > d;"},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}
