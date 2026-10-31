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

func TestParsePerplexityAgentOptionsSkillsAndEmptyStrings(t *testing.T) {
	bad := []interface{}{
		map[string]interface{}{"type": "builtin", "name": "office/odt"},
		map[string]interface{}{"type": "inline", "name": "n", "description": "d"},
		map[string]interface{}{"type": "other"},
	}
	for _, skill := range bad {
		if _, _, err := parsePerplexityAgentOptions(map[string]interface{}{"skills": []interface{}{skill}}); err == nil {
			t.Fatalf("skill %v: want validation error", skill)
		}
	}
	good := map[string]interface{}{
		"skills": []interface{}{map[string]interface{}{"type": "builtin", "name": "office/pdf"}},
		"tools":  []interface{}{map[string]interface{}{"type": "mcp", "server_label": "", "server_url": ""}},
	}
	if _, _, err := parsePerplexityAgentOptions(good); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
}
