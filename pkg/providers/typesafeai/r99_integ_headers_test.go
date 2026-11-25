package typesafeai

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

func TestR99integConfigHeaderAnySpellingWins(t *testing.T) {
	p := New(Config{APIKey: "k", Headers: map[string]string{"AUTHORIZATION": "Bearer custom"}})
	h, err := p.resolveHeaders()
	if err != nil {
		t.Fatal(err)
	}
	if got := r99integHeaderValues(h, "authorization"); len(got) != 1 || got[0] != "Bearer custom" {
		t.Errorf("authorization = %v", got)
	}
}
