package opencode

import (
	"encoding/json"
	"os"
	"testing"
)

// TS normalizeDomain is new URL(...).host (WHATWG: IDNA, IPv4 forms,
// percent-decoded host, non-special schemes keep their case) and, when that
// throws, strips ONE leading http(s):// (/^https?:\/\//). Recorded with node
// (testdata/r86b_tools_normalize_domain_ts.json).
func TestR86btoolsNormalizeDomainMatchesNode(t *testing.T) {
	data, err := os.ReadFile("testdata/r86b_tools_normalize_domain_ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input string `json:"input"`
			TS    string `json:"ts"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range fixture.Cases {
		if got := normalizeDomain(c.Input); got != c.TS {
			t.Errorf("normalizeDomain(%q) = %q, want %q", c.Input, got, c.TS)
		}
	}
}
