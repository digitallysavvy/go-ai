package together

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestTogetherOpenAICompatibleProviderOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "meta-llama/Llama-3.3-70B-Instruct-Turbo")

	opts := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"openaiCompatible": map[string]interface{}{
				"reasoningEffort": "medium",
				"textVerbosity":   "high",
				"user":            "user-123",
			},
		},
	}
	body, warnings := model.buildRequestBodyWithWarnings(opts, false)

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for camelCase provider options, got %#v", warnings)
	}
	if body["reasoning_effort"] != "medium" {
		t.Errorf("expected reasoning_effort=medium, got %v", body["reasoning_effort"])
	}
	if body["verbosity"] != "high" {
		t.Errorf("expected verbosity=high, got %v", body["verbosity"])
	}
	if body["user"] != "user-123" {
		t.Errorf("expected user=user-123, got %v", body["user"])
	}
}
