package cerebras

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// TS openai-compatible getArgs pushes the deprecated 'openai-compatible'
// key warning once; Go DoGenerate reported it twice (the base model's and
// the cerebras option resolution's).
func TestR85toolsDeprecatedKeyWarningOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{"openai-compatible": map[string]interface{}{"user": "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range res.Warnings {
		if w.Type == "deprecated" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("deprecated warnings = %d (%v), want 1", n, res.Warnings)
	}
}
