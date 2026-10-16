package zai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS transformZaiRequestBody drops user and verbosity, and forwards an empty
// thinking object (`thinking !== undefined`).
func TestZaiRequestBodyDropsUserVerbosityKeepsEmptyThinking(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "glm-5.3")
	body, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"zai":              map[string]interface{}{"user": "u-1", "textVerbosity": "low", "thinking": map[string]interface{}{}},
			"openaiCompatible": map[string]interface{}{"user": "u-2", "textVerbosity": "high"},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"user", "verbosity"} {
		if v, ok := body[k]; ok {
			t.Errorf("%s = %#v must not be sent", k, v)
		}
	}
	if th, ok := body["thinking"].(map[string]interface{}); !ok || len(th) != 0 {
		t.Errorf("thinking = %#v, want {}", body["thinking"])
	}

	unset, _, _ := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := unset["thinking"]; ok {
		t.Errorf("thinking must stay absent when unset")
	}
}
