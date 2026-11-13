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

// TS OpenAICompatibleImageModel: maxImagesPerCall is a constant 10 and
// file/mask input support is undefined, whatever the model id is.
func TestCompatImageModelCapabilitiesAreConstant(t *testing.T) {
	p := New(Config{APIKey: "k", OpenAICompatible: true, Name: "my-host"})
	for _, id := range []string{"dall-e-3", "flux-1", "gpt-image-1"} {
		m := NewImageModel(p, id)
		if got := m.MaxImagesPerCall(); got != 10 {
			t.Errorf("%s: MaxImagesPerCall = %d, want 10", id, got)
		}
		if m.SupportsFileInputs() != nil || m.SupportsMaskInputs() != nil {
			t.Errorf("%s: compat file/mask support must be unknown (nil)", id)
		}
	}
}
