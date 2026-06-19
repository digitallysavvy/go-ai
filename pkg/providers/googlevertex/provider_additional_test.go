package googlevertex

import (
	"os"
	"testing"
)

func TestVertexProvider_CreateAliasesAndClient(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
	}
	p1, err := CreateGoogleVertex(cfg)
	if err != nil {
		t.Fatalf("CreateGoogleVertex() error = %v", err)
	}
	if p1 == nil || p1.Client() == nil {
		t.Fatal("CreateGoogleVertex returned nil provider or nil client")
	}

	p2, err := CreateVertex(cfg)
	if err != nil {
		t.Fatalf("CreateVertex() error = %v", err)
	}
	if p2 == nil || p2.Client() == nil {
		t.Fatal("CreateVertex returned nil provider or nil client")
	}
}

func TestVertexProviderEnvironmentFallbacks(t *testing.T) {
	const envKey = "env-vertex-key"
	const envProject = "env-project"
	const envLocation = "env-location"
	origKey := os.Getenv("GOOGLE_VERTEX_API_KEY")
	origProject := os.Getenv("GOOGLE_VERTEX_PROJECT")
	origLocation := os.Getenv("GOOGLE_VERTEX_LOCATION")
	t.Cleanup(func() {
		if origKey == "" {
			_ = os.Unsetenv("GOOGLE_VERTEX_API_KEY")
		} else {
			_ = os.Setenv("GOOGLE_VERTEX_API_KEY", origKey)
		}
		if origProject == "" {
			_ = os.Unsetenv("GOOGLE_VERTEX_PROJECT")
		} else {
			_ = os.Setenv("GOOGLE_VERTEX_PROJECT", origProject)
		}
		if origLocation == "" {
			_ = os.Unsetenv("GOOGLE_VERTEX_LOCATION")
		} else {
			_ = os.Setenv("GOOGLE_VERTEX_LOCATION", origLocation)
		}
	})
	if err := os.Setenv("GOOGLE_VERTEX_API_KEY", envKey); err != nil {
		t.Fatalf("Setenv GOOGLE_VERTEX_API_KEY failed: %v", err)
	}
	if err := os.Setenv("GOOGLE_VERTEX_PROJECT", envProject); err != nil {
		t.Fatalf("Setenv GOOGLE_VERTEX_PROJECT failed: %v", err)
	}
	if err := os.Setenv("GOOGLE_VERTEX_LOCATION", envLocation); err != nil {
		t.Fatalf("Setenv GOOGLE_VERTEX_LOCATION failed: %v", err)
	}

	p, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.config.APIKey != envKey || p.config.Project != envProject || p.config.Location != envLocation {
		t.Fatalf("config = %#v", p.config)
	}
}

func TestVertexHostMatchesTypeScriptProvider(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"global":      "aiplatform.googleapis.com",
		"eu":          "aiplatform.eu.rep.googleapis.com",
		"us":          "aiplatform.us.rep.googleapis.com",
		"us-central1": "us-central1-aiplatform.googleapis.com",
	}
	for location, want := range tests {
		if got := vertexHost(location); got != want {
			t.Fatalf("vertexHost(%q) = %q, want %q", location, got, want)
		}
	}
}

func TestVertexProvider_ModelFactoriesAndUnsupportedMethods(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	em, err := p.EmbeddingModel("text-embedding-005")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	if em.Provider() != "google-vertex" {
		t.Fatalf("EmbeddingModel provider = %q, want google-vertex", em.Provider())
	}
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("EmbeddingModel(\"\") expected error")
	}

	speech, err := p.SpeechModel("gemini-2.5-flash-tts")
	if err != nil {
		t.Fatalf("SpeechModel() error = %v", err)
	}
	if speech.Provider() != "google.vertex.speech" || speech.ModelID() != "gemini-2.5-flash-tts" {
		t.Fatalf("SpeechModel metadata = %s/%s", speech.Provider(), speech.ModelID())
	}
	emptySpeech, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel(\"\") should preserve the caller model ID, got error %v", err)
	}
	if emptySpeech.ModelID() != "" {
		t.Fatalf("SpeechModel(\"\").ModelID() = %q, want empty string", emptySpeech.ModelID())
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
	if _, err := p.VideoModel(""); err == nil {
		t.Fatal("VideoModel(\"\") expected validation error")
	}
	if _, err := p.VideoModel("veo-3.0-generate-preview"); err == nil {
		t.Fatal("VideoModel expected not implemented error")
	}
}
