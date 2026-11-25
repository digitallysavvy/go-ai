package codemode

import "testing"

// A `//` comment ends at CR, U+2028 and U+2029 as well as LF; the import( on
// the next "line" is code the engine runs.
func TestAssertNoDynamicImportLineTerminators(t *testing.T) {
	for name, src := range map[string]string{
		"cr":  "// c\rawait import('qjs:std')",
		"ls":  "// c await import('qjs:std')",
		"ps":  "// c await import('qjs:std')",
		"tpl": "`${ 1 // c }` ; await import('qjs:std')",
		// The regex in the interpolation holds a backtick that is not a
		// template delimiter.
		"regex in interpolation": "const s = `a${ /`/.test('x') }b`; await import('qjs:std')",
	} {
		if assertNoDynamicImport(src) == nil {
			t.Errorf("%s: import( not rejected in %q", name, src)
		}
	}
	for _, src := range []string{
		"const s = `${a++ / 2} and ${b / 3}`; return s",
		"const s = `${x / 2}`; // c\nreturn s",
	} {
		if err := assertNoDynamicImport(src); err != nil {
			t.Errorf("%q rejected: %v", src, err)
		}
	}
	got, _ := stripTypeScriptAnnotations("// c\rlet x: number = 1")
	if got != "// c\rlet x         = 1" {
		t.Errorf("CR ends a line comment in the stripper: %q", got)
	}
}
