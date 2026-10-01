package codemode

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// stripFuzzEdgeSeeds are TypeScript and malformed-input edge cases for the
// type stripper: unterminated strings/templates/comments/regex literals,
// bare angle brackets, generic arrows, and assorted TS-only syntax.
var stripFuzzEdgeSeeds = []string{
	"const x: number = 1",
	"function f<T,>(a: T): T { return a }",
	"(<T,>(x: T) => x)",
	"const g = <T extends object = {}>(v: T): T => v;",
	"class A { private x?: string; constructor(public y: number) {} }",
	"declare abstract class Q { abstract m(): void }",
	"function h(this: Window, n: number) { return n }",
	"let a = b as any satisfies C!",
	"x?.y!.z",
	"import type { X } from 'y'",
	"export type { Y }",
	"interface I { a: string }",
	"type T = { [k: string]: number }",
	"enum E { A }",
	"for (const [a, b]: [number, string] of c) {}",
	"a < b > c",
	"`${a<b>(c)}` /re<g>/g",
	"<",
	">",
	"<<>>",
	"{",
	"((",
	")",
	"'",
	"\"unterminated",
	"`",
	"`${",
	"`${`${",
	"/*",
	"/* unterminated block",
	"//",
	"/",
	"/[",
	"/unterminated regex",
	"x = /[/]/g",
	"\\",
	"\x00",
	"\xef\xbb\xbf",
	"",
}

// FuzzStripTypeScriptAnnotations checks the stripper on arbitrary input:
// the unguarded core must not panic and must return, the guarded entry
// point must agree with it, and plain-JavaScript seeds must come back
// byte-identical. Under plain `go test` only the seed corpus runs.
func FuzzStripTypeScriptAnnotations(f *testing.F) {
	plain := make(map[string]bool, len(plainJSSnippets))
	for _, s := range plainJSSnippets {
		plain[s] = true
		f.Add(s)
	}
	for _, s := range stripFuzzEdgeSeeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		type result struct {
			out   string
			err   error
			panic interface{}
		}
		done := make(chan result, 1)
		go func() {
			// Recover only to report the panic as a test failure; the
			// unchecked core itself must not panic.
			defer func() {
				if r := recover(); r != nil {
					done <- result{panic: r}
				}
			}()
			out, err := stripTypeScriptAnnotationsUnchecked(src)
			done <- result{out: out, err: err}
		}()

		var res result
		select {
		case res = <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("stripper did not return within 5s for %q", src)
		}
		if res.panic != nil {
			t.Fatalf("stripper panicked on %q: %v", src, res.panic)
		}

		guarded, guardedErr := stripTypeScriptAnnotations(src)
		if guarded != res.out || (guardedErr == nil) != (res.err == nil) {
			t.Fatalf("guarded and unchecked stripper disagree on %q: (%q, %v) vs (%q, %v)", src, guarded, guardedErr, res.out, res.err)
		}
		if res.err != nil && res.out != src {
			t.Fatalf("on error the stripper must return the source unmodified for %q, got %q", src, res.out)
		}
		if plain[src] && (res.err != nil || res.out != src) {
			t.Fatalf("plain JS must be returned byte-identical:\ngot:  %q (err %v)\nwant: %q", res.out, res.err, src)
		}
	})
}

// TestStripTypeScriptAnnotations_RecoversPanic exercises the recover path:
// a panicking core yields the original source unmodified plus an error,
// which run_code_mode.go treats like any other strip error (the raw source
// runs and the engine reports any syntax error).
func TestStripTypeScriptAnnotations_RecoversPanic(t *testing.T) {
	src := "const x: number = 1;"
	for _, tc := range []struct {
		name  string
		value interface{}
	}{
		{"string", "boom"},
		{"error", errors.New("boom")},
		{"runtime", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core := func(string) (string, error) {
				if tc.value == nil {
					var tokens []tsToken
					_ = tokens[1] // index out of range
				}
				panic(tc.value)
			}
			out, err := stripWithRecover(src, core)
			if out != src {
				t.Fatalf("out = %q, want the source unmodified %q", out, src)
			}
			if err == nil || !strings.Contains(err.Error(), "panicked") {
				t.Fatalf("err = %v, want a recovered-panic error", err)
			}
		})
	}

	// A non-panicking core passes its result and error through unchanged.
	wantErr := fmt.Errorf("strip failed")
	out, err := stripWithRecover(src, func(string) (string, error) { return src, wantErr })
	if out != src || !errors.Is(err, wantErr) {
		t.Fatalf("pass-through = (%q, %v), want (%q, %v)", out, err, src, wantErr)
	}
	out, err = stripWithRecover(src, func(string) (string, error) { return "const x = 1;", nil })
	if out != "const x = 1;" || err != nil {
		t.Fatalf("pass-through = (%q, %v), want (%q, nil)", out, err, "const x = 1;")
	}
}
