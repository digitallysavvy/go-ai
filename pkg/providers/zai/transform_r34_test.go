package zai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS transformZaiRequestBody forwards an empty thinking object
// (`thinking !== undefined`). TS drops user and verbosity, but v0.5.2 sent
// them, so Go keeps forwarding them (Known difference, compat).
func TestZaiRequestBodyKeepsUserVerbosityAndEmptyThinking(t *testing.T) {
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
	if body["user"] != "u-1" || body["verbosity"] != "low" {
		t.Errorf("user = %#v, verbosity = %#v; want the v0.5.2 forwarding u-1/low", body["user"], body["verbosity"])
	}
	if th, ok := body["thinking"].(map[string]interface{}); !ok || len(th) != 0 {
		t.Errorf("thinking = %#v, want {}", body["thinking"])
	}

	unset, _, _ := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := unset["thinking"]; ok {
		t.Errorf("thinking must stay absent when unset")
	}
}
