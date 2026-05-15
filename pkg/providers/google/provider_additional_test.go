package google

import (
	"errors"
	"os"
	"testing"
)

func TestProvider_CreateAliasesAndName(t *testing.T) {
	t.Parallel()

	p1 := CreateGoogle(Config{APIKey: "k1"})
	if p1 == nil {
		t.Fatal("CreateGoogle returned nil provider")
	}
	if p1.Name() != "google.generative-ai" {
		t.Fatalf("Name() = %q, want %q", p1.Name(), "google.generative-ai")
	}

	p2 := CreateGoogleGenerativeAI(Config{APIKey: "k2", Name: "custom-google"})
	if p2 == nil {
		t.Fatal("CreateGoogleGenerativeAI returned nil provider")
	}
	if p2.Name() != "custom-google" {
		t.Fatalf("Name() = %q, want %q", p2.Name(), "custom-google")
	}
}

func TestProvider_ModelFactoriesAndUnsupportedMethods(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "test-key"})
	if p.Client() == nil {
		t.Fatal("Client() returned nil")
	}
	if p.Files() == nil {
		t.Fatal("Files() returned nil")
	}

	embed, err := p.EmbeddingModel(EmbeddingModelGeminiEmbedding001)
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	if embed.Provider() != "google.generative-ai" {
		t.Fatalf("Embedding provider = %q, want google.generative-ai", embed.Provider())
	}

	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("EmbeddingModel(\"\") expected error")
	}

	if _, err := p.SpeechModel("any"); err == nil {
		t.Fatal("SpeechModel expected unsupported error")
	}
	if _, err := p.TranscriptionModel("any"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}
	if _, err := p.RerankingModel("any"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
}

func TestProvider_APIKeyFallbackToEnvironment(t *testing.T) {
	t.Parallel()

	const envKey = "env-google-key"
	orig := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	t.Cleanup(func() {
		if orig == "" {
			_ = os.Unsetenv("GOOGLE_GENERATIVE_AI_API_KEY")
			return
		}
		_ = os.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", orig)
	})
	if err := os.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", envKey); err != nil {
		t.Fatalf("Setenv failed: %v", err)
	}

	p := New(Config{})
	if got := p.APIKey(); got != envKey {
		t.Fatalf("APIKey() = %q, want %q", got, envKey)
	}
}

func TestGoogleSupportsImageInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		modelID string
		want    bool
	}{
		{modelID: "gemini-pro-vision", want: true},
		{modelID: "gemini-1.5-pro", want: true},
		{modelID: "gemini-2.0-flash", want: true},
		{modelID: "gemini-3.1-pro-preview", want: true},
		{modelID: "gemini-pro", want: false},
		{modelID: "text-embedding-004", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.modelID, func(t *testing.T) {
			t.Parallel()
			if got := googleSupportsImageInput(tc.modelID); got != tc.want {
				t.Fatalf("googleSupportsImageInput(%q) = %v, want %v", tc.modelID, got, tc.want)
			}
		})
	}
}

func TestEmbeddingHandleErrorWrapsProviderError(t *testing.T) {
	t.Parallel()

	m := NewEmbeddingModel(New(Config{APIKey: "k"}), "gemini-embedding-001")
	err := m.handleError(errors.New("boom"))
	if err == nil {
		t.Fatal("handleError returned nil")
	}

	var pe interface{ Unwrap() error }
	if !errors.As(err, &pe) {
		t.Fatalf("expected wrapped provider error type, got %T", err)
	}
	if pe.Unwrap() == nil || pe.Unwrap().Error() != "boom" {
		t.Fatalf("unexpected wrapped cause: %v", pe.Unwrap())
	}
}

func TestOptsHeadersNilSafe(t *testing.T) {
	t.Parallel()
	if got := optsHeaders(nil); got != nil {
		t.Fatalf("optsHeaders(nil) = %#v, want nil", got)
	}
}
