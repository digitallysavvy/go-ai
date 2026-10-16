package perplexity

import "testing"

func TestParsePerplexityAgentOptionsAcceptsGoTypedValues(t *testing.T) {
	opts, _, err := parsePerplexityAgentOptions(map[string]interface{}{
		"max_steps": 3,
		"tools":     []map[string]interface{}{{"type": "web_search"}},
		"models":    []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.MaxSteps == nil || *opts.MaxSteps != 3 || len(opts.Tools) != 1 || len(opts.Models) != 2 {
		t.Fatalf("opts = %+v", opts)
	}
}
