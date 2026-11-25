package perplexity

import (
	"encoding/json"
	"testing"
)

// TS perplexityErrorToMessage returns `data.message ?? 'unknown error'`: an
// explicit empty message is kept.
func TestPerplexityErrorToMessageKeepsEmptyTopLevelMessage(t *testing.T) {
	for body, want := range map[string]string{
		`{"message":""}`:         "",
		`{"message":"boom"}`:     "boom",
		`{}`:                     "unknown error",
		`{"detail":"d"}`:         "d",
		`{"error":{"type":"t"}}`: "t",
	} {
		var env perplexityErrorEnvelope
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			t.Fatal(err)
		}
		if got := perplexityErrorToMessage(env); got != want {
			t.Fatalf("%s: got %q want %q", body, got, want)
		}
	}
}
