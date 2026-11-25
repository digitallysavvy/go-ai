package replicate

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS `prediction.error ?? 'Unknown error'` keeps an empty error string, and
// wait=${n} is JS number formatting.
func TestR33_EmptyPredictionErrorAndWaitFormat(t *testing.T) {
	empty := ""
	_, err := completedImagePrediction(replicateImagePrediction{Status: "failed", Error: &empty})
	if err == nil || !strings.HasSuffix(err.Error(), "Replicate image generation failed: ") {
		t.Errorf("empty error: %v", err)
	}
	_, err = completedImagePrediction(replicateImagePrediction{Status: "failed"})
	if err == nil || !strings.Contains(err.Error(), "Unknown error") {
		t.Errorf("nil error: %v", err)
	}

	m := NewImageModel(New(Config{APIKey: "k"}), "owner/model")
	_, _, prefer, err := m.buildRequest(&provider.ImageGenerateOptions{Prompt: "p", ProviderOptions: map[string]interface{}{
		"replicate": map[string]interface{}{"maxWaitTimeInSeconds": 1000000.0},
	}})
	if err != nil || prefer != "wait=1000000" {
		t.Errorf("prefer = %q, err = %v", prefer, err)
	}
}

// A system prompt outside Messages is part of the prompt text.
func TestR33_LanguagePromptIncludesSystem(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "owner/model:v")
	body := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{System: "be brief", Text: "hi"}})
	if got := body["input"].(map[string]interface{})["prompt"]; got != "System: be brief\nhi" {
		t.Errorf("prompt = %q", got)
	}
}
