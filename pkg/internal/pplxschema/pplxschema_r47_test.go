package pplxschema

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
)

// A malformed handled output item fails every member of TS
// perplexityOutputItemSchema (a z.union), so the issue is an invalid_union
// whose last member is the refined catch-all; an unknown type still passes.
func TestR47_MalformedHandledItemIsInvalidUnion(t *testing.T) {
	msg := zodlite.ValidationMessage(Response, `{"id":"a","created_at":1,"model":"m","object":"response","status":"s","output":[{"type":"message","content":"x"}]}`)
	for _, want := range []string{`"code": "invalid_union"`, `"output"`, `expected \"search_results\"`, `"code": "custom"`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %s in:\n%s", want, msg)
		}
	}
	ok := zodlite.ValidationMessage(Response, `{"id":"a","created_at":1,"model":"m","object":"response","status":"s","output":[{"type":"finance_results","results":5}]}`)
	if ok != "" {
		t.Fatalf("unhandled type rejected: %s", ok)
	}
	if bad := zodlite.ValidationMessage(Response, `{"id":"a","created_at":1,"model":"m","object":"response","status":"s","output":[{"type":5}]}`); !strings.Contains(bad, "invalid_union") {
		t.Fatalf("non-string type: %s", bad)
	}
}
