package gemini

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

const r87dgooglefamSanitizeTest = "replaces const with enum while preserving JSON Schema"

// r87dgooglefamPorted are the TS plain assertions the replay cannot evaluate,
// each reproduced by the named Go test with the same input and expected value.
var r87dgooglefamPorted = map[string]string{
	// the call must not mutate its argument: Go's argument after the call,
	// which the replay does not expose
	tsfixture.PortedKey(r87dgooglefamSanitizeTest, "schema", "toMatchObject"): "TestR87dgooglefamSanitizeKeepsInputSchema",
}

func init() {
	tsfixture.RequireEvaluated()
	tsfixture.RegisterPorted(r87dgooglefamPorted)
}

func TestMain(m *testing.M) { os.Exit(tsfixture.PortedMain(m, r87dgooglefamPorted)) }

// TestR87dgooglefamSanitizeKeepsInputSchema ports the second assertion of TS
// sanitize-response-json-schema.test.ts "replaces const with enum while
// preserving JSON Schema": after sanitizeResponseJsonSchema(schema), the
// caller's schema still holds `const: 'fruit'` (the function copies).
func TestR87dgooglefamSanitizeKeepsInputSchema(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/sanitize_schema.json.gz")
	c, a, ok := tsfixture.FindAssertion(cases, r87dgooglefamSanitizeTest, "schema", "toMatchObject")
	if !ok || len(c.FnArgs) != 1 {
		t.Fatal("case not found")
	}
	schema := c.FnArgs[0]
	before, _ := json.Marshal(schema)
	out := sanitizeResponseJSONSchema(schema)
	if msg, ok := tsfixture.MatchAssertion(a, schema); !ok || msg != "" {
		t.Errorf("schema after the call: %s (evaluated %v)", msg, ok)
	}
	after, _ := json.Marshal(schema)
	if string(before) != string(after) {
		t.Errorf("the input schema changed:\nbefore %s\nafter  %s", before, after)
	}
	if b, _ := json.Marshal(out); string(b) == string(after) {
		t.Errorf("the result is the unsanitized input: %s", b)
	}
}
