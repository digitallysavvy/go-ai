package bytedance

import (
	"testing"
)

func TestNewProvider(t *testing.T) {
	t.Run("returns error when no API key", func(t *testing.T) {
		t.Setenv("BYTEDANCE_API_KEY", "")

		_, err := New(Config{})
		if err == nil {
			t.Error("expected error when no API key provided")
		}
	})

	t.Run("uses explicit API key", func(t *testing.T) {
		prov, err := New(Config{APIKey: "test-key"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prov == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("uses BYTEDANCE_API_KEY env var", func(t *testing.T) {
		t.Setenv("BYTEDANCE_API_KEY", "env-test-key")

		prov, err := New(Config{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prov == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("uses default base URL", func(t *testing.T) {
		prov, err := New(Config{APIKey: "test-key"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prov.config.BaseURL != defaultBaseURL {
			t.Errorf("expected base URL %s, got %s", defaultBaseURL, prov.config.BaseURL)
		}
	})

	t.Run("uses custom base URL", func(t *testing.T) {
		customURL := "https://custom.bytedance.example.com/api/v3"
		prov, err := New(Config{
			APIKey:  "test-key",
			BaseURL: customURL,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prov.config.BaseURL != customURL {
			t.Errorf("expected base URL %s, got %s", customURL, prov.config.BaseURL)
		}
	})
}

func TestProvider_Name(t *testing.T) {
	prov, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prov.Name() != "bytedance" {
		t.Errorf("expected name 'bytedance', got '%s'", prov.Name())
	}
}

func TestProvider_VideoModel(t *testing.T) {
	prov, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("returns error for empty model ID", func(t *testing.T) {
		_, err := prov.VideoModel("")
		if err == nil {
			t.Error("expected error for empty model ID")
		}
	})

	t.Run("returns model for valid model ID", func(t *testing.T) {
		model, err := prov.VideoModel(string(ModelSeedance10Pro))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model == nil {
			t.Fatal("expected non-nil model")
		}
	})

	t.Run("model has correct provider name", func(t *testing.T) {
		model, err := prov.VideoModel(string(ModelSeedance10Pro))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model.Provider() != "bytedance.video" {
			t.Errorf("expected provider 'bytedance.video', got '%s'", model.Provider())
		}
	})

	t.Run("model has correct model ID", func(t *testing.T) {
		modelID := string(ModelSeedance15Pro)
		model, err := prov.VideoModel(modelID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model.ModelID() != modelID {
			t.Errorf("expected model ID '%s', got '%s'", modelID, model.ModelID())
		}
	})

	t.Run("model has correct specification version", func(t *testing.T) {
		model, err := prov.VideoModel(string(ModelSeedance10Pro))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model.SpecificationVersion() != "v3" {
			t.Errorf("expected specification version 'v3', got '%s'", model.SpecificationVersion())
		}
	})
}

func TestProvider_UnsupportedModels(t *testing.T) {
	prov, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("language model returns error", func(t *testing.T) {
		_, err := prov.LanguageModel("gpt-4")
		if err == nil {
			t.Error("expected error for language model")
		}
	})

	t.Run("embedding model returns error", func(t *testing.T) {
		_, err := prov.EmbeddingModel("text-embedding-3")
		if err == nil {
			t.Error("expected error for embedding model")
		}
	})

	t.Run("image model returns error", func(t *testing.T) {
		_, err := prov.ImageModel("dalle-3")
		if err == nil {
			t.Error("expected error for image model")
		}
	})

	t.Run("speech model returns error", func(t *testing.T) {
		_, err := prov.SpeechModel("tts-1")
		if err == nil {
			t.Error("expected error for speech model")
		}
	})

	t.Run("transcription model returns error", func(t *testing.T) {
		_, err := prov.TranscriptionModel("whisper-1")
		if err == nil {
			t.Error("expected error for transcription model")
		}
	})

	t.Run("reranking model returns error", func(t *testing.T) {
		_, err := prov.RerankingModel("reranker-1")
		if err == nil {
			t.Error("expected error for reranking model")
		}
	})
}

func TestModelConstants(t *testing.T) {
	expectedModels := map[ByteDanceVideoModelID]string{
		ModelSeedance15Pro:     "seedance-1-5-pro-251215",
		ModelSeedance10Pro:     "seedance-1-0-pro-250528",
		ModelSeedance10ProFast: "seedance-1-0-pro-fast-251015",
		ModelSeedance10LiteT2V: "seedance-1-0-lite-t2v-250428",
		ModelSeedance10LiteI2V: "seedance-1-0-lite-i2v-250428",
	}

	for model, expected := range expectedModels {
		if string(model) != expected {
			t.Errorf("model constant %v: expected %s, got %s", model, expected, string(model))
		}
	}
}
