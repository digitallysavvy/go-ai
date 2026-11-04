package providerutils

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"slices"
	"testing"
)

// JSRegexpSpace and JSRegexpDot match exactly the runes JavaScript's /\s/ and
// /./ do, and the (?-i:[sS]) / (?-i:[kK]) spellings the documented JS /i
// letters; recorded with node over every code point
// (testdata/r86b_tools_js_regexp.json).
func TestR86btoolsJSRegexpClassesMatchNode(t *testing.T) {
	data, err := os.ReadFile("testdata/r86b_tools_js_regexp.json")
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Space  []int `json:"space"`
		NotDot []int `json:"notDot"`
		S      []int `json:"s"`
		K      []int `json:"k"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	space := regexp.MustCompile(`^` + JSRegexpSpace + `$`)
	notSpace := regexp.MustCompile(`^[^` + JSRegexpSpaceChars + `]$`)
	dot := regexp.MustCompile(`^` + JSRegexpDot + `$`)
	s := regexp.MustCompile(`(?i)^(?-i:[sS])$`)
	k := regexp.MustCompile(`(?i)^(?-i:[kK])$`)
	var gotSpace, gotNotDot, gotS, gotK []int
	for r := rune(0); r <= 0x10ffff; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		ch := string(r)
		if space.MatchString(ch) {
			gotSpace = append(gotSpace, int(r))
			if !IsJSWhiteSpace(r) {
				t.Errorf("U+%04X: JSRegexpSpace matches but IsJSWhiteSpace is false", r)
			}
		} else if IsJSWhiteSpace(r) {
			t.Errorf("U+%04X: IsJSWhiteSpace but JSRegexpSpace does not match", r)
		}
		if notSpace.MatchString(ch) == space.MatchString(ch) {
			t.Errorf("U+%04X: [^JSRegexpSpaceChars] is not the complement", r)
		}
		if !dot.MatchString(ch) {
			gotNotDot = append(gotNotDot, int(r))
		}
		if s.MatchString(ch) {
			gotS = append(gotS, int(r))
		}
		if k.MatchString(ch) {
			gotK = append(gotK, int(r))
		}
	}
	for name, pair := range map[string][2][]int{"space": {gotSpace, want.Space}, "notDot": {gotNotDot, want.NotDot}, "s": {gotS, want.S}, "k": {gotK, want.K}} {
		if !slices.Equal(pair[0], pair[1]) {
			t.Errorf("%s: Go %v, node %v", name, pair[0], pair[1])
		}
	}
}

// FetchHeaderValue / FetchHeaderText are TS fetch Headers.get: repeated lines
// joined with ", " (node fetch returns "text/plain; charset=x, a/b" for two
// Content-Type lines), each byte one character in the text form (node reads
// a raw "\xc3\xa9" header as "Ã©").
func TestR86btoolsFetchHeader(t *testing.T) {
	h := http.Header{"Content-Type": {"text/plain;\xa0charset=x", "a/b"}, "X-U": {"\xc3\xa9z"}}
	if got := FetchHeaderValue(h, "content-type"); got != "text/plain;\xa0charset=x, a/b" {
		t.Errorf("FetchHeaderValue = %q", got)
	}
	if got := FetchHeaderText(h, "content-type"); got != "text/plain; charset=x, a/b" {
		t.Errorf("FetchHeaderText = %q", got)
	}
	if got := FetchHeaderText(h, "x-u"); got != "Ã©z" {
		t.Errorf("FetchHeaderText = %q", got)
	}
	if got := FetchHeaderText(h, "missing"); got != "" {
		t.Errorf("absent = %q", got)
	}
	if got := FetchHeaderText(nil, "x-u"); got != "" {
		t.Errorf("nil header = %q", got)
	}
}
