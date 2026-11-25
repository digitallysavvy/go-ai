package replicate

import (
	"strings"
	"testing"
)

// Header sweep (integrator, review round 99): TS combines the default and
// configured (or call) headers in entry order and lower-cases them, so the
// later header wins whatever its spelling. Go merged by exact spelling, so
// two spellings stayed and the sorted lower-casing (or map order) picked one.
func r99integHeaderValues(h map[string]string, name string) []string {
	var out []string
	for k, v := range h {
		if strings.EqualFold(k, name) {
			out = append(out, v)
		}
	}
	return out
}

func TestR99integCredentialedCallHeaderAnySpellingWins(t *testing.T) {
	p := New(Config{APIKey: "k", Headers: map[string]string{"x-custom": "config"}})
	h := p.credentialedHeaders(p.baseURL()+"/predictions/1", map[string]string{"X-CUSTOM": "call", "AUTHORIZATION": "Bearer call"})
	if got := r99integHeaderValues(h, "x-custom"); len(got) != 1 || got[0] != "call" {
		t.Errorf("x-custom = %v", got)
	}
	if got := r99integHeaderValues(h, "authorization"); len(got) != 1 || got[0] != "Bearer call" {
		t.Errorf("authorization = %v", got)
	}
}
