package openai

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r85provbCapabilitiesTS is the TS getOpenAILanguageModelCapabilities shape.
type r85provbCapabilitiesTS struct {
	IsReasoningModel                       bool     `json:"isReasoningModel"`
	SystemMessageMode                      string   `json:"systemMessageMode"`
	SupportsFlexProcessing                 bool     `json:"supportsFlexProcessing"`
	SupportsPriorityProcessing             bool     `json:"supportsPriorityProcessing"`
	SupportsConfigurationUpdate            bool     `json:"supportsConfigurationUpdate"`
	SupportsAsyncToolCalling               bool     `json:"supportsAsyncToolCalling"`
	SupportedReasoningEfforts              []string `json:"supportedReasoningEfforts"`
	SupportsChatFunctionToolsWithReasoning bool     `json:"supportsChatFunctionToolsWithReasoning"`
	SupportsNonReasoningParameters         bool     `json:"supportsNonReasoningParameters"`
}

// TestR85provbCapabilitiesMatchNode compares GetLanguageModelCapabilities
// with TS getOpenAILanguageModelCapabilities (openai-model-catalog.ts at
// 30548ee7f9, run under Node 24) for every Go model ID constant plus edge
// IDs (dated GPT-5 variants, pro/codex/chat, GPT-6.1 Terra, unknown IDs).
// TS d07b475bc8 (GPT-5.x efforts, chat function tools) and 01d7a03b05
// (GPT-6.1 Terra).
func TestR85provbCapabilitiesMatchNode(t *testing.T) {
	f, err := os.Open("testdata/tscases/r85_capabilities_node.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]r85provbCapabilitiesTS
	if err := json.NewDecoder(zr).Decode(&want); err != nil {
		t.Fatal(err)
	}
	if len(want) < 100 {
		t.Fatalf("only %d recorded IDs", len(want))
	}
	for id, w := range want {
		c := GetLanguageModelCapabilities(id)
		got := r85provbCapabilitiesTS{
			IsReasoningModel:                       c.IsReasoningModel,
			SystemMessageMode:                      c.SystemMessageMode,
			SupportsFlexProcessing:                 c.SupportsFlexProcessing,
			SupportsPriorityProcessing:             c.SupportsPriorityProcessing,
			SupportsConfigurationUpdate:            c.SupportsConfigurationUpdate,
			SupportsAsyncToolCalling:               c.SupportsAsyncToolCalling,
			SupportedReasoningEfforts:              c.SupportedReasoningEfforts,
			SupportsChatFunctionToolsWithReasoning: c.SupportsChatFunctionToolsWithReasoning,
			SupportsNonReasoningParameters:         c.SupportsNonReasoningParameters,
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s:\n got  %+v\n want %+v", id, got, w)
		}
	}
}

func r85provbChatBody(t *testing.T, modelID string, opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning) {
	t.Helper()
	m := NewLanguageModel(New(Config{APIKey: "k"}), modelID)
	if opts.Prompt.Text == "" && len(opts.Prompt.Messages) == 0 {
		opts.Prompt = types.Prompt{Text: "hi"}
	}
	body, warnings, err := m.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	return body, warnings
}

func r85provbLevel(l types.ReasoningLevel) *types.ReasoningLevel { return &l }

// TS d07b475bc8 chat tests: unsupported top-level reasoning rounds to a
// supported effort; Chat never sends "max"; GPT-5.6 with function tools
// sends "none"; GPT-5 keeps prompt_cache_retention.
func TestR85provbChatReasoningEfforts(t *testing.T) {
	fnTools := []types.Tool{{Type: types.ToolTypeFunction, Name: "test-tool", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}}
	compat := func(from, to string) []types.Warning {
		return []types.Warning{{Type: "compatibility", Feature: "reasoning", Details: `reasoning "` + from + `" is not directly supported by this model. mapped to effort "` + to + `".`}}
	}
	toolsWarning := []types.Warning{{Type: "compatibility", Feature: "reasoning", Details: `gpt-5.6-luna does not support function tools with reasoning in the Chat Completions API. reasoning effort set to "none". Use the Responses API to combine reasoning with function tools.`}}
	tests := []struct {
		name     string
		model    string
		opts     provider.GenerateOptions
		effort   interface{}
		warnings []types.Warning
	}{
		{"gpt-5.5 minimal->low", "gpt-5.5", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMinimal)}, "low", compat("minimal", "low")},
		{"gpt-6-sol max->xhigh", "gpt-6-sol", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMax)}, "xhigh", compat("max", "xhigh")},
		{"gpt-5.6 max->xhigh on chat", "gpt-5.6", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMax)}, "xhigh", compat("max", "xhigh")},
		{"gpt-5.1 xhigh->high", "gpt-5.1", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningXHigh)}, "high", compat("xhigh", "high")},
		{"gpt-5-nano none->minimal", "gpt-5-nano", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningNone)}, "minimal", compat("none", "minimal")},
		{"unknown id sends max", "my-model", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMax)}, "max", nil},
		{"gpt-5.6-luna tools low->none", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningLow), Tools: fnTools}, "none", toolsWarning},
		{"gpt-5.6-luna tools none", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningNone), Tools: fnTools}, "none", nil},
		{"gpt-5.6-luna tools default", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningDefault), Tools: fnTools}, "none", nil},
		{"gpt-5.6-luna tools unset", "gpt-5.6-luna", provider.GenerateOptions{Tools: fnTools}, "none", nil},
		{"gpt-5.6-luna tools provider effort", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningMedium), Tools: fnTools, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "low"}}}, "low", nil},
		{"gpt-5.6-luna no tools", "gpt-5.6-luna", provider.GenerateOptions{Reasoning: r85provbLevel(types.ReasoningLow)}, "low", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			body, warnings := r85provbChatBody(t, tt.model, &opts)
			if body["reasoning_effort"] != tt.effort {
				t.Errorf("reasoning_effort = %#v, want %#v", body["reasoning_effort"], tt.effort)
			}
			if !reflect.DeepEqual(warnings, tt.warnings) {
				t.Errorf("warnings = %#v, want %#v", warnings, tt.warnings)
			}
		})
	}

	body, warnings := r85provbChatBody(t, "gpt-5.5", &provider.GenerateOptions{ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"promptCacheRetention": "24h"}}})
	if body["prompt_cache_retention"] != "24h" || len(warnings) != 0 {
		t.Errorf("gpt-5.5 prompt_cache_retention = %#v, warnings %#v", body["prompt_cache_retention"], warnings)
	}
}

// TS d07b475bc8 responses tests: GPT-5 keeps logprobs and
// prompt_cache_retention; GPT-5.6 sends max.
func TestR85provbResponsesReasoningEfforts(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5.5")
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "low", "logprobs": 5, "promptCacheRetention": "24h"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if body["top_logprobs"] != 5 || body["prompt_cache_retention"] != "24h" || len(warnings) != 0 {
		t.Errorf("gpt-5.5: top_logprobs %#v, prompt_cache_retention %#v, include %#v, warnings %#v", body["top_logprobs"], body["prompt_cache_retention"], body["include"], warnings)
	}

	m = NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-5.6-luna")
	body, _, warnings, err = m.buildRequest(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: r85provbLevel(types.ReasoningMax)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := body["reasoning"].(map[string]interface{}); r["effort"] != "max" || len(warnings) != 0 {
		t.Errorf("gpt-5.6-luna: reasoning %#v, warnings %#v", body["reasoning"], warnings)
	}
}
