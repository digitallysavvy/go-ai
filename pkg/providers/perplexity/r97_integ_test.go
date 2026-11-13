package perplexity

import (
	"strings"
	"testing"
)

// TestR97integRecordIssueInJSKeyOrder (cr-r97-carry-to-integrator): zod
// walks a record in the JS key order of the parsed object, so with two bad
// answers the first issue names the first key on the wire ("z"), not the
// first sorted key.
func TestR97integRecordIssueInJSKeyOrder(t *testing.T) {
	body := []byte(`{"answers":{"z":{"type":"bogus"},"a":{"type":"bogus"}}}`)
	err := validatePerplexityDecisionResponse(200, nil, body)
	if err == nil {
		t.Fatal("want a schema error")
	}
	msg := err.Error()
	z, a := strings.Index(msg, `"answers","z"`), strings.Index(msg, `"answers","a"`)
	if z < 0 || (a >= 0 && a < z) {
		t.Fatalf("err = %s, want the answers.z issue first", msg)
	}
}
