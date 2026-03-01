package openai

import "testing"

// TestOpenAIModelIDs verifies that all model ID constants have the correct string values.
func TestOpenAIModelIDs(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		// GPT-5.3 Codex
		{"GPT53Codex", ModelGPT53Codex, "gpt-5.3-codex"},

		// GPT-5 series
		{"GPT5", ModelGPT5, "gpt-5"},
		{"GPT5Mini", ModelGPT5Mini, "gpt-5-mini"},
		{"GPT5Nano", ModelGPT5Nano, "gpt-5-nano"},
		{"GPT5ChatLatest", ModelGPT5ChatLatest, "gpt-5-chat-latest"},

		// GPT-5.1 series
		{"GPT51", ModelGPT51, "gpt-5.1"},
		{"GPT51ChatLatest", ModelGPT51ChatLatest, "gpt-5.1-chat-latest"},

		// GPT-5.2 series
		{"GPT52", ModelGPT52, "gpt-5.2"},
		{"GPT52Pro", ModelGPT52Pro, "gpt-5.2-pro"},
		{"GPT52ChatLatest", ModelGPT52ChatLatest, "gpt-5.2-chat-latest"},

		// GPT-4.1 series
		{"GPT41", ModelGPT41, "gpt-4.1"},
		{"GPT41Mini", ModelGPT41Mini, "gpt-4.1-mini"},
		{"GPT41Nano", ModelGPT41Nano, "gpt-4.1-nano"},

		// GPT-4o series
		{"GPT4o", ModelGPT4o, "gpt-4o"},
		{"GPT4oMini", ModelGPT4oMini, "gpt-4o-mini"},
		{"GPT4oSearchPreview", ModelGPT4oSearchPreview, "gpt-4o-search-preview"},
		{"GPT4oMiniSearchPreview", ModelGPT4oMiniSearchPreview, "gpt-4o-mini-search-preview"},
		{"GPT4oAudioPreview", ModelGPT4oAudioPreview, "gpt-4o-audio-preview"},

		// Reasoning models
		{"O1", ModelO1, "o1"},
		{"O3Mini", ModelO3Mini, "o3-mini"},
		{"O3", ModelO3, "o3"},
		{"O4Mini", ModelO4Mini, "o4-mini"},

		// Legacy models
		{"GPT4Turbo", ModelGPT4Turbo, "gpt-4-turbo"},
		{"GPT4", ModelGPT4, "gpt-4"},
		{"GPT35Turbo", ModelGPT35Turbo, "gpt-3.5-turbo"},

		// Image models
		{"DallE3", ModelDallE3, "dall-e-3"},
		{"DallE2", ModelDallE2, "dall-e-2"},
		{"GPTImage1", ModelGPTImage1, "gpt-image-1"},
		{"GPTImage1Mini", ModelGPTImage1Mini, "gpt-image-1-mini"},
		{"GPTImage15", ModelGPTImage15, "gpt-image-1.5"},
		{"ChatGPTImageLatest", ModelChatGPTImageLatest, "chatgpt-image-latest"},

		// Embedding models
		{"TextEmbedding3Small", ModelTextEmbedding3Small, "text-embedding-3-small"},
		{"TextEmbedding3Large", ModelTextEmbedding3Large, "text-embedding-3-large"},
		{"TextEmbeddingAda002", ModelTextEmbeddingAda002, "text-embedding-ada-002"},

		// Speech models
		{"TTS1", ModelTTS1, "tts-1"},
		{"TTS1HD", ModelTTS1HD, "tts-1-hd"},

		// Transcription models
		{"Whisper1", ModelWhisper1, "whisper-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("model ID constant %s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// TestGPT53CodexAccepted verifies gpt-5.3-codex can be used with the provider.
func TestGPT53CodexAccepted(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model, err := p.LanguageModel(ModelGPT53Codex)
	if err != nil {
		t.Fatalf("LanguageModel(%q) returned error: %v", ModelGPT53Codex, err)
	}
	if model.ModelID() != ModelGPT53Codex {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), ModelGPT53Codex)
	}
}
