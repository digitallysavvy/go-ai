package providerutils

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 5006d83c4e (openai-compatible-chat-language-model.test.ts): an
// explicit null reasoningEffort is sent as null and wins over the top-level
// reasoning; an unset option does not overwrite a raw reasoning_effort
// passthrough; the option validates as z.string().nullish().
func TestR85provbCompatReasoningEffortNull(t *testing.T) {
	high := types.ReasoningHigh
	build := func(providerOptions map[string]interface{}, reasoning *types.ReasoningLevel) string {
		body := map[string]interface{}{}
		ApplyOpenAICompatibleCallSettings(body, nil, nil, nil, nil, "test-provider", providerOptions)
		options, _ := ResolveOpenAICompatibleProviderOptions("test-provider", providerOptions)
		if IsCustomReasoning(reasoning) {
			body["reasoning_effort"] = string(*reasoning)
		}
		ApplyOpenAICompatibleCommonRequestOptions(body, options)
		b, _ := json.Marshal(body)
		return string(b)
	}
	for _, tt := range []struct {
		name      string
		options   map[string]interface{}
		reasoning *types.ReasoningLevel
		want      string
	}{
		{"null", map[string]interface{}{"reasoningEffort": nil}, nil, `{"reasoning_effort":null}`},
		{"null beats top-level", map[string]interface{}{"reasoningEffort": nil}, &high, `{"reasoning_effort":null}`},
		{"string", map[string]interface{}{"reasoningEffort": "low"}, &high, `{"reasoning_effort":"low"}`},
		{"unset", map[string]interface{}{}, nil, `{}`},
		{"raw null passthrough", map[string]interface{}{"reasoning_effort": nil}, nil, `{"reasoning_effort":null}`},
		{"raw passthrough overwritten by top-level", map[string]interface{}{"reasoning_effort": "low"}, &high, `{"reasoning_effort":"high"}`},
	} {
		if got := build(map[string]interface{}{"test-provider": tt.options}, tt.reasoning); got != tt.want {
			t.Errorf("%s: body = %s, want %s", tt.name, got, tt.want)
		}
	}
	if err := ValidateOpenAICompatibleChatOptions(map[string]interface{}{"test-provider": map[string]interface{}{"reasoningEffort": nil}}, "test-provider"); err != nil {
		t.Errorf("null reasoningEffort must validate: %v", err)
	}
	if w := OpenAICompatibleCommonOptionWarnings(map[string]interface{}{"reasoning_effort": nil}); len(w) != 0 {
		t.Errorf("a raw reasoning_effort is a passthrough, not a deprecated option: %#v", w)
	}

	v, ok := ResolveOpenAICompatibleReasoningEffort(map[string]interface{}{}, &high, func(*types.ReasoningLevel) (string, bool) { return "mapped", true })
	if !ok || v != "mapped" {
		t.Errorf("mapReasoning: %v %v", v, ok)
	}
	if _, ok := ResolveOpenAICompatibleReasoningEffort(map[string]interface{}{}, nil, nil); ok {
		t.Error("no reasoning must resolve to nothing")
	}
}
