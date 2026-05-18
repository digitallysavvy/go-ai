package xai

import (
	"strings"
	"testing"
)

// TestGetAPIKeyDirectValue verifies an explicit API key takes priority over env.
func TestGetAPIKeyDirectValue(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-key")

	got := getAPIKey("direct-key")
	if got != "direct-key" {
		t.Errorf("getAPIKey(explicit) = %q, want %q", got, "direct-key")
	}
}

// TestGetAPIKeyEnvVar verifies XAI_API_KEY is read from the environment.
func TestGetAPIKeyEnvVar(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-api-key")

	got := getAPIKey("")
	if got != "env-api-key" {
		t.Errorf("getAPIKey(empty) with XAI_API_KEY set = %q, want %q", got, "env-api-key")
	}
}

// TestGetAPIKeyEmpty verifies empty string is returned when no key is configured.
func TestGetAPIKeyEmpty(t *testing.T) {
	// Ensure XAI_API_KEY is not set
	t.Setenv("XAI_API_KEY", "")

	got := getAPIKey("")
	if got != "" {
		t.Errorf("getAPIKey(empty) with no env var = %q, want empty string", got)
	}
}

// TestNewUsesEnvVar verifies that New() picks up XAI_API_KEY from the environment.
func TestNewUsesEnvVar(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-api-key")

	p := New(Config{})
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.Name() != "xai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "xai")
	}
}

func TestNormalizeBaseURLMatchesTypeScriptBaseURLShape(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"default", "", "https://api.x.ai"},
		{"root", "https://api.x.ai", "https://api.x.ai"},
		{"root trailing slash", "https://api.x.ai/", "https://api.x.ai"},
		{"typescript v1 base url", "https://api.x.ai/v1", "https://api.x.ai"},
		{"typescript v1 base url trailing slash", "https://api.x.ai/v1/", "https://api.x.ai"},
		{"custom v1 base url", "https://example.test/proxy/v1", "https://example.test/proxy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBaseURL(tt.in); got != tt.want {
				t.Fatalf("normalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestXAIDefaultUsesResponsesAPI verifies that LanguageModel() returns a Responses API model.
func TestXAIDefaultUsesResponsesAPI(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	model, err := p.LanguageModel("grok-3")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}

	// The Responses API model identifies itself as "xai.responses".
	if model.Provider() != "xai.responses" {
		t.Errorf("LanguageModel().Provider() = %q, want %q", model.Provider(), "xai.responses")
	}
	if _, ok := model.(*ResponsesLanguageModel); !ok {
		t.Errorf("LanguageModel() returned %T, want *ResponsesLanguageModel", model)
	}
}

// TestXAIChatCompletionsLanguageModelIsLegacy verifies that ChatCompletionsLanguageModel()
// returns a Chat Completions model, not a Responses API model.
func TestXAIChatCompletionsLanguageModelIsLegacy(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	model, err := p.ChatCompletionsLanguageModel("grok-3")
	if err != nil {
		t.Fatalf("ChatCompletionsLanguageModel() error: %v", err)
	}

	if model.Provider() != "xai" {
		t.Errorf("ChatCompletionsLanguageModel().Provider() = %q, want %q", model.Provider(), "xai")
	}
	if _, ok := model.(*LanguageModel); !ok {
		t.Errorf("ChatCompletionsLanguageModel() returned %T, want *LanguageModel", model)
	}
}

// TestRemovedModelsNotInList verifies that removed model IDs are not present in model_ids.go.
// Grok 2 IDs were shut down by XAI and must not be re-added.
func TestRemovedModelsNotInList(t *testing.T) {
	removed := []string{"grok-2", "grok-2-vision-1212", "grok-2-image", "grok-2-image-1212"}

	// Collect all defined model ID constant values.
	defined := []string{
		ModelGrokBeta,
		ModelGrok3,
		ModelGrok3Mini,
		ModelGrokImagineImage,
		ModelGrokImagineImagePro,
	}

	for _, removedID := range removed {
		for _, id := range defined {
			if strings.EqualFold(id, removedID) {
				t.Errorf("removed model ID %q should not be in model_ids.go (got constant value %q)", removedID, id)
			}
		}
	}
}

func TestCurrentChatModelIDConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Grok420NonReasoning", ModelGrok420NonReasoning, "grok-4.20-non-reasoning"},
		{"Grok420Reasoning", ModelGrok420Reasoning, "grok-4.20-reasoning"},
		{"Grok43", ModelGrok43, "grok-4.3"},
		{"GrokLatest", ModelGrokLatest, "grok-latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("model ID = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
