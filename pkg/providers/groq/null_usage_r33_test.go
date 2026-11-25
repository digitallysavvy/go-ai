package groq

import (
	"encoding/json"
	"testing"
)

// TS convert usage(undefined) is the null usage (unknown counts), not zero.
func TestResponseWithoutUsageIsNullUsageR33(t *testing.T) {
	var r groqResponse
	if err := json.Unmarshal([]byte(`{"id":"x","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}]}`), &r); err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(New(Config{APIKey: "k"}), "m")
	res, err := m.convertResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens != nil || res.Usage.OutputTokens != nil || res.Usage.TotalTokens != nil {
		t.Fatalf("usage = %+v, want null usage", res.Usage)
	}
}
