package openai

import "testing"

func TestImageMaxImagesPerCallAndResponseFormat(t *testing.T) {
	for id, want := range map[string]int{"dall-e-3": 1, "dall-e-2": 10, "gpt-image-1": 10, "gpt-image-9": 10, "chatgpt-image-latest": 10, "other": 1} {
		if got := NewImageModel(New(Config{APIKey: "k"}), id).MaxImagesPerCall(); got != want {
			t.Errorf("%s: %d, want %d", id, got, want)
		}
	}
	if !hasDefaultResponseFormat("gpt-image-3") || hasDefaultResponseFormat("dall-e-3") {
		t.Error("hasDefaultResponseFormat must be prefix gpt-image-/chatgpt-image-")
	}
}
