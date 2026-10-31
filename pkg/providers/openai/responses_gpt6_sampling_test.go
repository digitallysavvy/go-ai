package openai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from TS #22304: GPT-6 Sol/Luna keep temperature/topP/logprobs when
// the effective reasoning effort (after configuration updates) is "none".
func TestResponsesGPT6SolLunaSamplingParameters(t *testing.T) {
	cases := []struct {
		name      string
		messages  []types.Message
		opts      map[string]interface{}
		supported bool
	}{
		{"request update enables reasoning", []types.Message{userMsg("a")}, map[string]interface{}{"reasoningEffort": "none", "reasoningEffortUpdate": "low"}, false},
		{"request update disables reasoning", []types.Message{userMsg("a")}, map[string]interface{}{"reasoningEffort": "low", "reasoningEffortUpdate": "none"}, true},
		{"positioned update enables reasoning", []types.Message{userMsg("a"), updateMsg("openai", "low", ""), userMsg("b")}, map[string]interface{}{"reasoningEffort": "none"}, false},
		{"last positioned update disables reasoning", []types.Message{userMsg("a"), updateMsg("openai", "low", ""), userMsg("b"), updateMsg("openai", "none", ""), userMsg("c")}, map[string]interface{}{"reasoningEffort": "none"}, true},
		{"positioned update overrides prepended request update", []types.Message{userMsg("a"), updateMsg("openai", "low", ""), userMsg("b")}, map[string]interface{}{"reasoningEffort": "low", "reasoningEffortUpdate": "none"}, false},
		{"positioned update disables default reasoning", []types.Message{updateMsg("openai", "none", ""), userMsg("a")}, map[string]interface{}{}, true},
	}
	for _, modelID := range []string{ModelGPT6Sol, ModelGPT6Luna} {
		for _, tc := range cases {
			t.Run(modelID+"/"+tc.name, func(t *testing.T) {
				p := New(Config{APIKey: "test-key"})
				m := NewResponsesLanguageModel(p, modelID)
				temp, topP := 0.0, 0.9
				o := map[string]interface{}{"logprobs": 2, "include": []interface{}{"message.output_text.logprobs", "reasoning.encrypted_content"}}
				for k, v := range tc.opts {
					o[k] = v
				}
				body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
					Prompt:          types.Prompt{Messages: tc.messages},
					Temperature:     &temp,
					TopP:            &topP,
					ProviderOptions: map[string]interface{}{"openai": o},
				}, false)
				if err != nil {
					t.Fatalf("buildRequest: %v", err)
				}
				_, hasTemp := body["temperature"]
				_, hasTopP := body["top_p"]
				_, hasTop := body["top_logprobs"]
				if hasTemp != tc.supported || hasTopP != tc.supported || hasTop != tc.supported {
					t.Errorf("temperature=%v top_p=%v top_logprobs=%v, want all %v", hasTemp, hasTopP, hasTop, tc.supported)
				}
				wantInclude := []string{"reasoning.encrypted_content"}
				var wantWarn []string
				if tc.supported {
					wantInclude = []string{"message.output_text.logprobs", "reasoning.encrypted_content"}
				} else {
					wantWarn = []string{"temperature", "topP", "logprobs"}
				}
				if inc, _ := body["include"].([]string); !reflect.DeepEqual(inc, wantInclude) {
					t.Errorf("include = %#v, want %#v", body["include"], wantInclude)
				}
				var gotWarn []string
				for _, w := range warnings {
					gotWarn = append(gotWarn, w.Feature)
				}
				if !reflect.DeepEqual(gotWarn, wantWarn) {
					t.Errorf("warnings = %v, want %v", gotWarn, wantWarn)
				}
			})
		}
	}
}
