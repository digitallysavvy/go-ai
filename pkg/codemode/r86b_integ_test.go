package codemode

import "testing"

// cr-r86b-tools-to-integrator 1 (JS \s class): JavaScript accepts any
// WhiteSpace or LineTerminator (U+00A0, U+FEFF, U+2028, ...) and multi-line
// block comments between `import` and "(", so the regex-token guard matches
// them too (RE2 \s is ASCII only and "." stopped at a newline).
func TestR86bintegDynamicImportGuardJSWhitespace(t *testing.T) {
	for _, src := range []string{
		"/import\u00a0(x)/",
		"/import\ufeff(x)/",
		"/import\u2028(x)/",
		"/import/*a\nb*/(x)/",
		"/import (x)/",
	} {
		if !regexDynamicImport.MatchString(src) {
			t.Errorf("regexDynamicImport missed %q", src)
		}
	}
	for _, src := range []string{"/imports(x)/", "/import.meta/"} {
		if regexDynamicImport.MatchString(src) {
			t.Errorf("regexDynamicImport matched %q", src)
		}
	}
	for _, src := range []string{"import\u00a0('x')", "import\ufeff('x')", "import/*a\nb*/('x')"} {
		if err := assertNoDynamicImport(src); err == nil {
			t.Errorf("assertNoDynamicImport(%q) = nil, want the dynamic import error", src)
		}
	}
}
