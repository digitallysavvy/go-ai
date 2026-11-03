package cerebras

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS 5006d83c4e (openai-compatible-chat-language-model.ts 295-303): an
// explicit null reasoningEffort is written as reasoning_effort: null, which
// the transport layer applies over any top-level reasoning effort.
func TestR85toolsNullReasoningEffort(t *testing.T) {
	extras, _ := resolveCerebrasOptions(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{
		"cerebras": map[string]interface{}{"reasoningEffort": nil},
	}})
	v, ok := extras["reasoning_effort"]
	if !ok || v != nil {
		t.Fatalf("extras = %#v, want reasoning_effort: null", extras)
	}
	extras, _ = resolveCerebrasOptions(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{
		"cerebras": map[string]interface{}{"user": "u"},
	}})
	if _, ok := extras["reasoning_effort"]; ok {
		t.Fatalf("an unset reasoningEffort must not be written: %#v", extras)
	}
	body := map[string]interface{}{"reasoning_effort": "high"}
	applyCerebrasRequestExtras(body, cerebrasRequestExtras{"reasoning_effort": nil})
	if v, ok := body["reasoning_effort"]; !ok || v != nil {
		t.Fatalf("body = %#v, want the null to beat the mapped effort", body)
	}
}
