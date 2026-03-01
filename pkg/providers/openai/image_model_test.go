package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// BUG-T17: models whose IDs begin with recognized prefixes (chatgpt-image, gpt-image-1, etc.)
// manage their own response format and must NOT receive an explicit
// response_format=b64_json in the request body (#12838).
func TestHasDefaultResponseFormat(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		// Models that have a built-in default — must not get response_format override.
		{"chatgpt-image-1", true},
		{"chatgpt-image-latest", true},
		{"gpt-image-1", true},
		{"gpt-image-1-mini", true},
		{"gpt-image-1.5", true},
		{"gpt-image-1.5-turbo", true},
		// Classic DALL-E models that need the explicit b64_json format.
		{"dall-e-3", false},
		{"dall-e-2", false},
		{"dall-e-2-hd", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := hasDefaultResponseFormat(tt.modelID)
			if got != tt.expected {
				t.Errorf("hasDefaultResponseFormat(%q) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

// TestBuildRequestBody_NoResponseFormatForChatGPTImage verifies that the
// response_format field is absent from the request body for chatgpt-image models.
func TestBuildRequestBody_NoResponseFormatForChatGPTImage(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewImageModel(p, "chatgpt-image-1")

	body := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "a cat"})

	if _, ok := body["response_format"]; ok {
		t.Errorf("response_format must not be set for chatgpt-image models, got %v", body["response_format"])
	}
}

// TestBuildRequestBody_ResponseFormatSetForDALLE verifies that dall-e models
// still receive response_format=b64_json.
func TestBuildRequestBody_ResponseFormatSetForDALLE(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewImageModel(p, "dall-e-3")

	body := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "a cat"})

	rf, ok := body["response_format"]
	if !ok {
		t.Errorf("response_format must be set for dall-e models")
	}
	if rf != "b64_json" {
		t.Errorf("response_format = %v, want b64_json", rf)
	}
}
