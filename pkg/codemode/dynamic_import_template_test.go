package codemode

import (
	"context"
	"testing"
)

// TestAssertNoDynamicImport_InsideTemplateInterpolation covers an escape
// found during integration: a template literal is a single string token,
// so an import() that appears only inside a ${...} interpolation was not
// seen by the token scan and reached qjs:std at run time.
func TestAssertNoDynamicImport_InsideTemplateInterpolation(t *testing.T) {
	rejected := []string{
		"const t = `${await import('qjs:std')}`;",
		"const t = `a${1}b${ import ('qjs:os') }c`;",
		"const t = `${`${import('qjs:std')}`}`;",
		"const t = `${ (() => import/**/('qjs:bjson'))() }`;",
		"const r = /import('qjs:std')/;",
	}
	for _, js := range rejected {
		if err := assertNoDynamicImport(js); err == nil {
			t.Errorf("assertNoDynamicImport(%q) = nil, want an error", js)
		}
	}

	allowed := []string{
		"const t = `import('qjs:std') is just text`;",
		"const t = `${'import(\"x\")'}`;",
		"const t = `${obj.import(1)}`;",
		"const t = `${a}${b}`;",
		"const r = /imports?/g;",
	}
	for _, js := range allowed {
		if err := assertNoDynamicImport(js); err != nil {
			t.Errorf("assertNoDynamicImport(%q) = %v, want nil", js, err)
		}
	}
}

// TestRunCodeMode_TemplateInterpolationImportCannotReachStd runs the
// original escape end to end: the native module must not be loaded.
func TestRunCodeMode_TemplateInterpolationImportCannotReachStd(t *testing.T) {
	js := "const t = `${await import('qjs:std').then(m => (globalThis.__m = m, 1))}`; " +
		"return typeof (globalThis.__m && globalThis.__m.getenv);"
	res, err := RunCodeMode(context.Background(), RunInput{JS: js})
	if err == nil {
		t.Fatalf("RunCodeMode returned %v with no error; the import must be rejected", res)
	}
	if res == "function" {
		t.Fatalf("qjs:std was reachable through a template interpolation")
	}
}

// FuzzAssertNoDynamicImport checks that the dynamic-import scan never
// panics or hangs on arbitrary (model-written) input, and that a direct
// import( call wrapped in a template interpolation is always rejected.
func FuzzAssertNoDynamicImport(f *testing.F) {
	for _, s := range []string{
		"`${import('qjs:std')}`", "`${", "`${`", "`${'}`", "`\\${import(1)}`",
		"/import(/", "/[/]import(/", "import", "import(", "x.import(1)",
		"`${a}${`${b}`}`", "", "`", "${", "}",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_ = assertNoDynamicImport(src)
		_ = templateInterpolations("`" + src + "`")
		if err := assertNoDynamicImport("const t = `${import('qjs:std')}`;" + "\n" + src); err == nil {
			t.Fatalf("template-interpolated import not rejected when followed by %q", src)
		}
	})
}
