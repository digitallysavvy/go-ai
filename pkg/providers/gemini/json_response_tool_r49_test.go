package gemini

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs gates the JSON response tool on /(^|\/)gemini-/i, so only a
// leading "gemini-" or a "/" prefix qualifies; a "." prefix (eu.gemini-...)
// does not, although the capability patterns accept it.
func TestWithJSONResponseToolModelPattern(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"gemini-2.0-flash", true},
		{"models/gemini-2.0-flash", true},
		{"publishers/google/models/GEMINI-2.0-flash", true},
		{"eu.gemini-2.0-flash", false},
		{"gemma-3-27b", false},
	}
	for _, tc := range cases {
		m := NewLanguageModel(Config{ProviderName: "google.generative-ai"}, tc.id)
		opts := &provider.GenerateOptions{
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
			Tools:          []types.Tool{{Type: types.ToolTypeFunction, Name: "lookup"}},
		}
		_, name := m.withJSONResponseTool(opts)
		if (name != "") != tc.want {
			t.Errorf("%s: response tool = %q, want rewrite %v", tc.id, name, tc.want)
		}
	}
}
