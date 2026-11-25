package together

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS togetherai-provider.test.ts (146396263d) structuredOutputSupportCases:
// 'should set supportsStructuredOutputs for %s to %s' and
// 'returns structured output support for %s as %s'.
func TestPortprovbStructuredOutputSupportCases(t *testing.T) {
	cases := []struct {
		modelID  string
		expected bool
	}{
		{"deepseek-ai/DeepSeek-V4-Flash-0731", true},
		{"deepseek-ai/DeepSeek-V4.1-Flash", true},
		{"deepseek-ai/DeepSeek-V4.1-Flash-1017", true},
		{"openai/gpt-oss-120b", true},
		{"zai-org/GLM-5.2", true},
		{"zai-org/GLM-5.3-Flash", true},
		{"moonshotai/Kimi-K3", true},
		{"MiniMaxAI/MiniMax-M3.1-Flash", true},
		{"zai-org/GLM-5.3", false},
		{"custom-model-id", false},
	}
	schemaRF := &provider.ResponseFormat{Type: "json", Schema: responseFormatTestSchema}
	for _, tc := range cases {
		if got := SupportsStructuredOutputs(tc.modelID); got != tc.expected {
			t.Errorf("SupportsStructuredOutputs(%q) = %v, want %v", tc.modelID, got, tc.expected)
		}
		model := NewLanguageModel(New(Config{APIKey: "k"}), tc.modelID)
		if got := model.SupportsStructuredOutput(); got != tc.expected {
			t.Errorf("%q: model support = %v, want %v", tc.modelID, got, tc.expected)
		}
		body, _ := model.buildRequestBodyWithWarnings(responseFormatOpts(schemaRF, nil), false)
		if tc.expected {
			assertResponseFormat(t, body, wantJSONSchemaStrict)
		} else {
			assertResponseFormat(t, body, wantJSONObject)
		}
	}
}
