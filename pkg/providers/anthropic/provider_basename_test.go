package anthropic

import "testing"

// TS derives the batch/decision provider ids from providerName with a trailing
// ".messages" removed.
func TestProviderIDsDropMessagesSuffix(t *testing.T) {
	p := New(Config{APIKey: "k", Name: "custom.messages"})
	if got := NewBatch(p).Provider(); got != "custom.batch" {
		t.Fatalf("batch provider = %q, want custom.batch", got)
	}
	dm, err := p.DecisionModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	if got := dm.Provider(); got != "custom.decision" {
		t.Fatalf("decision provider = %q, want custom.decision", got)
	}
}
