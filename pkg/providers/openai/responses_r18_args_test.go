package openai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS resolves reasoningEffort once (option ?? top-level reasoning) and then
// applies the GPT-6+ supported-efforts check to the result, so a top-level
// `reasoning: minimal` on gpt-6 is warned about and dropped.
func TestResponsesTopLevelReasoningCheckedAgainstSupportedEfforts(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), ModelGPT6Sol)
	lvl := types.ReasoningMinimal
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &lvl,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := body["reasoning"].(map[string]interface{}); ok {
		if _, has := r["effort"]; has {
			t.Fatalf("effort must be dropped, got %v", r)
		}
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "reasoningEffort" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected reasoningEffort warning, got %v", warnings)
	}
}

// TS include order: user values, logprobs, web search sources, code
// interpreter outputs, then reasoning.encrypted_content.
func TestResponsesIncludeOrderMatchesTS(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "o3")
	body, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Name: "web_search", ProviderID: "openai.web_search", Type: "provider"},
			{Name: "code_interpreter", ProviderID: "openai.code_interpreter", Type: "provider"},
		},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"store": false, "logprobs": true}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"message.output_text.logprobs", "web_search_call.action.sources", "code_interpreter_call.outputs", "reasoning.encrypted_content"}
	if got, _ := body["include"].([]string); !reflect.DeepEqual(got, want) {
		t.Fatalf("include = %v, want %v", body["include"], want)
	}
}

// logprobs: 0 is sent as top_logprobs 0 but, being falsy in TS, adds no include.
func TestResponsesZeroLogprobsSentWithoutInclude(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	body, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": 0}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := body["top_logprobs"]; !ok || v != 0 {
		t.Fatalf("top_logprobs = %v (present=%v), want 0", v, ok)
	}
	if _, ok := body["include"]; ok {
		t.Fatalf("include must be absent, got %v", body["include"])
	}
}

// When the GPT-6 logprobs filter empties the include list TS sends none.
func TestResponsesEmptiedIncludeIsOmitted(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), ModelGPT6Sol)
	body, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": 3, "reasoningEffort": "low"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := body["include"]; ok {
		t.Fatalf("include must be absent, got %v", v)
	}
	if _, ok := body["top_logprobs"]; ok {
		t.Fatal("top_logprobs must be dropped")
	}
}
