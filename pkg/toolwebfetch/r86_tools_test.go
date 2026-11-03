package toolwebfetch

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// testdata/r86_tools_domains_ts.json is TS normalizeDomains (domain-filter.ts
// at 30548ee7f9) run with node: "bare" is the /[\s/\\:*@?#[\]]/ rejection
// (JS \s includes U+00A0, U+2028 and U+FEFF), "invalid" a failed
// new URL(`https://${trimmed}`), otherwise the normalized hostname.
func TestR86toolsNormalizeDomainsMatchesTS(t *testing.T) {
	data, err := os.ReadFile("testdata/r86_tools_domains_ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Domain   string `json:"domain"`
		Error    string `json:"error"`
		Hostname string `json:"hostname"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		out, err := normalizeDomains("allowedDomains", []string{c.Domain})
		switch c.Error {
		case "bare":
			if err == nil || !strings.Contains(err.Error(), "Use bare domains") {
				t.Errorf("%q: got %q %v, want the bare-domain error", c.Domain, out, err)
			}
		case "invalid":
			if err == nil || strings.Contains(err.Error(), "Use bare domains") {
				t.Errorf("%q: got %q %v, want the invalid-domain error", c.Domain, out, err)
			}
		default:
			if err != nil || len(out) != 1 || out[0] != c.Hostname {
				t.Errorf("%q: got %q %v, want %q", c.Domain, out, err, c.Hostname)
			}
		}
	}
}

// testdata/r86_tools_urls_ts.json is node `new URL(u)` with `hash = ''`
// (the TS tool's URL handling): canonicalURL must fail exactly where it
// throws (forbidden host code points, UTS46-disallowed and bidi-invalid
// hosts, bad ports) and otherwise serialize the same href.
func TestR86toolsCanonicalURLMatchesNode(t *testing.T) {
	data, err := os.ReadFile("testdata/r86_tools_urls_ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		URL   string `json:"url"`
		Href  string `json:"href"`
		Error bool   `json:"error"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, ok := canonicalURL(c.URL)
		if c.Error {
			if ok {
				t.Errorf("%q: got %q, want a parse failure", c.URL, got)
			}
			continue
		}
		if !ok || got.String() != c.Href {
			t.Errorf("%q: got %v %v, want %q", c.URL, got, ok, c.Href)
		}
	}
}
