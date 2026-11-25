package codemode

import (
	"strings"
	"testing"
	"time"
)

// A line of unterminated regex candidates (`/[` repeated) made the tokenizer
// rescan to the end of the line at every `/`: quadratic time, about 12 s for a
// source of the default size limit.
func TestTokenizeBoundedOnUnterminatedRegexCandidates(t *testing.T) {
	cases := map[string]string{
		"slash-class":       strings.Repeat("/[", DefaultMaxSourceBytes/2),
		"paren-slash-class": strings.Repeat("(/[", DefaultMaxSourceBytes/3),
		"in-template":       "`${" + strings.Repeat("(/[", DefaultMaxSourceBytes/3) + "}`",
	}
	for name, src := range cases {
		start := time.Now()
		_, _ = stripTypeScriptAnnotations(src)
		if d := time.Since(start); d > 4*time.Second {
			t.Errorf("%s: %d bytes took %v", name, len(src), d)
		}
	}
}

// The memo must not change what is a regex literal.
func TestRegexMemoKeepsRegexLiterals(t *testing.T) {
	src := "const a: RegExp = /[/]x/g; const b = [/[/[/]/, /y/]; const c: string = `${/[`]/.source}`;"
	got, err := stripTypeScriptAnnotations(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "const a         = /[/]x/g; const b = [/[/[/]/, /y/]; const c         = `${/[`]/.source}`;"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
