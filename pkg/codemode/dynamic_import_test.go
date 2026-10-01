package codemode

import "testing"

// Unit tests for assertNoDynamicImport (sandbox_hardening.go), exercised
// directly against the tokenizer rather than through a full RunCodeMode
// round trip, so every case -- true positive and false positive alike --
// is fast and precise. See sandbox_escape_routes_test.go for the
// end-to-end RunCodeMode-level coverage of the same routes (plus the
// engine-level findings, like the Unicode-escape SyntaxError, that this
// function deliberately leaves to the engine itself).

func TestAssertNoDynamicImport_RejectsDynamicImportCalls(t *testing.T) {
	cases := map[string]string{
		"bare":                    `import('qjs:std')`,
		"bare_plain_specifier":    `import('std')`,
		"space_before_paren":      `import ('qjs:std')`,
		"tab_before_paren":        "import\t('qjs:std')",
		"newline_before_paren":    "import\n('qjs:std')",
		"block_comment_between":   `import/* sneaky */('qjs:std')`,
		"line_comment_between":    "import// sneaky\n('qjs:std')",
		"multiple_comments_space": "import /* a */ /* b */ ('qjs:std')",
		"inside_async_iife":       `(async () => { const m = await import('qjs:std'); return m; })();`,
		"inside_try_catch":        `try { const m = await import('qjs:os'); return m; } catch (e) { return null; }`,
		"second_statement":        `const x = 1; import('qjs:bjson');`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			if err := assertNoDynamicImport(js); err == nil {
				t.Fatalf("expected assertNoDynamicImport to reject %q, got nil error", js)
			}
		})
	}
}

func TestAssertNoDynamicImport_RejectionIsAnUnsupportedSyntaxError(t *testing.T) {
	err := assertNoDynamicImport(`import('qjs:std')`)
	if err == nil {
		t.Fatal("expected an error")
	}
	unsupported, ok := err.(*UnsupportedSyntaxError)
	if !ok {
		t.Fatalf("expected *UnsupportedSyntaxError, got %T (%v)", err, err)
	}
	if unsupported.ErrorCode() != "CODE_MODE_UNSUPPORTED_SYNTAX" {
		t.Fatalf("unexpected error code: %s", unsupported.ErrorCode())
	}
}

// TestAssertNoDynamicImport_DoesNotFlagNonImportText is the core of this
// review round: every one of these must pass through unrejected, because
// none of them is an ImportCall -- a plain regex (`\bimport\s*\(`) flagged
// every single one of these as a false positive before this function was
// rewritten to use tokenizeTS.
func TestAssertNoDynamicImport_DoesNotFlagNonImportText(t *testing.T) {
	cases := map[string]string{
		"double_quoted_string":               `return "import(";`,
		"single_quoted_string":               `return 'import(something)';`,
		"string_with_full_call_text":         `return "await import(\"qjs:std\")";`,
		"template_literal_text":              "return `prefix import(x) suffix`;",
		"template_literal_interp":            "const x = 1; return `value: ${x} import(`;",
		"line_comment":                       "// import(x) -- just a comment\nreturn 1;",
		"block_comment":                      "/* import(x) in a block comment */\nreturn 1;",
		"block_comment_multiline":            "/*\n * import(x)\n */\nreturn 1;",
		"regex_literal":                      `return /import\(/.test("import(x)");`,
		"regex_literal_in_context":           `const re = /^import\(.*\)$/; return re.test(x);`,
		"member_access_dot":                  `return tools.import(5);`,
		"member_access_optional":             `return tools?.import(5);`,
		"member_access_nested":               `return a.b.import(5);`,
		"member_access_no_call":              `return tools.import;`,
		"import_meta_not_a_call":             `return typeof import.meta;`,
		"identifier_containing_import":       `const importantValue = 5; function doImportantThing() { return importantValue; } return doImportantThing();`,
		"import_token_not_followed_by_paren": `return typeof import;`,
	}
	for name, js := range cases {
		name, js := name, js
		t.Run(name, func(t *testing.T) {
			if err := assertNoDynamicImport(js); err != nil {
				t.Fatalf("expected assertNoDynamicImport to accept %q, got error: %v", js, err)
			}
		})
	}
}

// TestAssertNoDynamicImport_StaticImportDeclarationIsNotThisFunctionsJob
// documents that a static `import ... from ...` declaration is NOT flagged
// by this function at all (it has no `import(` call shape to match) --
// it's rejected by the engine itself with a SyntaxError instead, since
// RunCodeMode always evaluates in script/global mode. See
// sandbox_escape_routes_test.go's
// TestSandboxEscapeRoutes_StaticImportDeclarationIsASyntaxError for that
// end-to-end behavior.
func TestAssertNoDynamicImport_StaticImportDeclarationIsNotThisFunctionsJob(t *testing.T) {
	js := `import * as os from 'qjs:os'; return typeof os;`
	if err := assertNoDynamicImport(js); err != nil {
		t.Fatalf("static import declarations are the engine's SyntaxError to raise, not assertNoDynamicImport's; got: %v", err)
	}
}

// TestAssertNoDynamicImport_ObjectLiteralMethodNamedImportFailsClosed
// documents the one known, deliberate over-rejection this function makes:
// an object-literal or class method literally named `import` is lexically
// indistinguishable from an ImportCall without full parser-level
// object-literal-context tracking, so it is rejected too. See
// assertNoDynamicImport's doc comment for why this trade-off is acceptable
// (TypeScript's own sandbox supports no form of `import` either, so there
// is no parity requirement to accept this).
func TestAssertNoDynamicImport_ObjectLiteralMethodNamedImportFailsClosed(t *testing.T) {
	js := `const obj = { import(x) { return x; } }; return obj.import(5);`
	if err := assertNoDynamicImport(js); err == nil {
		t.Fatal("expected this documented edge case to still be rejected (fails closed)")
	}
}
