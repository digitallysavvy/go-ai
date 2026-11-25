package quiverai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS quiverai-language-model.ts: `Unsupported reasoning effort:
// ${reasoningEffort}` uses JS String() (an array joins with ",", an object
// is "[object Object]").
func TestR89toolsQuiverAIReasoningOptionErrorText(t *testing.T) {
	for _, tc := range []struct {
		key  string
		v    interface{}
		want string
	}{
		{"reasoningEffort", []interface{}{"a", "b"}, "Unsupported reasoning effort: a,b"},
		{"reasoningEffort", map[string]interface{}{"x": 1}, "Unsupported reasoning effort: [object Object]"},
		{"reasoningEffort", 2, "Unsupported reasoning effort: 2"},
		{"reasoningSummary", 1.5, "Unsupported reasoning summary: 1.5"},
		{"reasoningSummary", false, "Unsupported reasoning summary: false"},
	} {
		_, _, err := prepareQuiverAICall(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"quiverai": map[string]interface{}{tc.key: tc.v}}})
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v: err = %v, want %q", tc.v, err, tc.want)
		}
	}
}
