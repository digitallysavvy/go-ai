package urlnorm

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// TestNodeHrefCorpus replays testdata/node_href_corpus.json.gz: every input
// with Node's `new URL(input).href` (null when Node throws).
func TestNodeHrefCorpus(t *testing.T) {
	f, err := os.Open("testdata/node_href_corpus.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var corpus [][2]*string
	if err := json.NewDecoder(zr).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus) < 4000 {
		t.Fatalf("corpus has %d cases", len(corpus))
	}
	failures := 0
	for _, c := range corpus {
		input, want := *c[0], c[1]
		got, err := Href(input)
		switch {
		case want == nil && err == nil:
			t.Errorf("Href(%q) = %q, want Invalid URL", input, got)
		case want != nil && err != nil:
			t.Errorf("Href(%q) error %v, want %q", input, err, *want)
		case want != nil && got != *want:
			t.Errorf("Href(%q) = %q, want %q", input, got, *want)
		default:
			continue
		}
		if failures++; failures > 40 {
			t.Fatal("too many failures")
		}
	}
}

func TestInvalidURLError(t *testing.T) {
	_, err := Parse("not a url")
	if !errors.Is(err, ErrInvalidURL) || err.Error() != "Invalid URL" {
		t.Fatalf("err = %v", err)
	}
}

func TestPathname(t *testing.T) {
	for input, want := range map[string]string{
		"https://x.com/a b/c.PNG?q": "/a%20b/c.PNG",
		"https://x.com":             "/",
		"mailto:a@b":                "a@b",
		"foo://x":                   "",
	} {
		u, err := Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if got := u.Pathname(); got != want {
			t.Errorf("Pathname(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestKnownDifferencesFromNode pins the inputs where Parse and Node v24.20.0
// (Ada) disagree. All need a domain with a non-ASCII code point and an
// "xn--" label after UTS #46 mapping; see domainToASCII.
func TestKnownDifferencesFromNode(t *testing.T) {
	for _, c := range []struct {
		input, node, goHref string
	}{
		// RFC 3492 Punycode with its only delimiter first: x/net rejects it.
		{"http://Xn---Ⅻ/", "http://xn---xii/", ""},
		// RFC 3492-valid Punycode that Ada's decoder rejects.
		{"http://Xn--ｘｎ--Ⅻﬀxn--255ＡＢ/", "", "http://xn--xn--xiiffxn--255ab/"},
		// Decodes to Arabic Extended-B (Unicode 14) after "x": Ada does not
		// treat those code points as RTL, so it skips the Bidi rule.
		{"http://Xn--ｘｎ--0xgﬀ/", "http://xn--xn--0xgff/", ""},
	} {
		got, err := Href(c.input)
		if err != nil {
			got = ""
		}
		if got != c.goHref {
			t.Errorf("Href(%q) = %q (%v); recorded Go result %q (Node %q)", c.input, got, err, c.goHref, c.node)
		}
	}
}
