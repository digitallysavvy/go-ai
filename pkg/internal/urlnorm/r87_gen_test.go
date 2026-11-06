package urlnorm

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"
)

func r87genReadGz(t *testing.T, path string, v interface{}) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(zr).Decode(v); err != nil {
		t.Fatal(err)
	}
}

// TestR87genRequestOriginMatchesNode replays TS getRequestOrigin (934100cad4,
// ai/src/telemetry/get-request-origin.ts) run by Node v24.20.0 over every
// input of node_href_corpus.json.gz: testdata/node_origin_corpus.json.gz
// holds, in corpus order, `new URL(x).origin` for http(s) URLs and null
// otherwise (generator: testdata/gen_origin_corpus.mjs).
func TestR87genRequestOriginMatchesNode(t *testing.T) {
	var corpus [][2]*string
	r87genReadGz(t, "testdata/node_href_corpus.json.gz", &corpus)
	var origins []*string
	r87genReadGz(t, "testdata/node_origin_corpus.json.gz", &origins)
	if len(origins) != len(corpus) {
		t.Fatalf("origin corpus has %d entries, href corpus %d", len(origins), len(corpus))
	}
	known := map[string]bool{}
	for _, in := range []string{"http://Xn---Ⅻ/", "http://Xn--ｘｎ--Ⅻﬀxn--255ＡＢ/", "http://Xn--ｘｎ--0xgﬀ/"} {
		known[in] = true // TestKnownDifferencesFromNode
	}
	nonNull, failures := 0, 0
	for i, c := range corpus {
		input := *c[0]
		if known[input] {
			continue
		}
		got := RequestOrigin(input)
		ok := got != ""
		want := origins[i]
		if want != nil {
			nonNull++
		}
		switch {
		case want == nil && !ok:
			continue
		case want == nil:
			t.Errorf("RequestOrigin(%q) = %q, want none", input, got)
		case !ok:
			t.Errorf("RequestOrigin(%q) = none, want %q", input, *want)
		case got != *want:
			t.Errorf("RequestOrigin(%q) = %q, want %q", input, got, *want)
		default:
			continue
		}
		if failures++; failures > 40 {
			t.Fatal("too many failures")
		}
	}
	if nonNull < 500 {
		t.Fatalf("only %d http(s) origins in the corpus", nonNull)
	}
}

// TestR87genRequestOriginTSCases are the get-request-origin.test.ts inputs plus
// a few spot checks, with the Node results.
func TestR87genRequestOriginTSCases(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"not a URL":              "",
		"/relative":              "",
		"file:///etc/passwd":     "",
		"data:text/plain,secret": "",
		"https://user:password@example.com:8443/private?key=secret#fragment":        "https://example.com:8443",
		"https://user:password@model.example.test:8443/private?key=secret#fragment": "https://model.example.test:8443",
		"HTTP://EXAMPLE.com:80/a":     "http://example.com",
		"https://[::1]:443/x":         "https://[::1]",
		"https://[2001:db8::1]:8443/": "https://[2001:db8::1]:8443",
		"http://bücher.de/":           "http://xn--bcher-kva.de",
		"blob:https://x.com/uuid":     "",
		"ws://x.com/":                 "",
		"https://x.com:0/":            "https://x.com:0",
	} {
		if got := RequestOrigin(in); got != want {
			t.Errorf("RequestOrigin(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"blob:https://x.com/uuid": "https://x.com",
		"blob:foo://x/":           "null",
		"ws://X.com:80/":          "ws://x.com",
		"file:///etc":             "null",
		"mailto:a@b":              "null",
	} {
		u, err := Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := u.Origin(); got != want {
			t.Errorf("Origin(%q) = %q, want %q", in, got, want)
		}
	}
}
