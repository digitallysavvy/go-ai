package bfl

import (
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestProviderFactoriesAndUnsupported(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "bfl" {
		t.Fatalf("Name = %q", p.Name())
	}

	lm, err := p.LanguageModel("x")
	if lm != nil || err == nil {
		t.Fatalf("LanguageModel expected unsupported error, got model=%v err=%v", lm, err)
	}
	em, err := p.EmbeddingModel("x")
	if em != nil || err == nil {
		t.Fatalf("EmbeddingModel expected unsupported error, got model=%v err=%v", em, err)
	}
	sm, err := p.SpeechModel("x")
	if sm != nil || err == nil {
		t.Fatalf("SpeechModel expected unsupported error, got model=%v err=%v", sm, err)
	}
	tm, err := p.TranscriptionModel("x")
	if tm != nil || err == nil {
		t.Fatalf("TranscriptionModel expected unsupported error, got model=%v err=%v", tm, err)
	}
	rm, err := p.RerankingModel("x")
	if rm != nil || err == nil {
		t.Fatalf("RerankingModel expected unsupported error, got model=%v err=%v", rm, err)
	}

	im, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel: %v", err)
	}
	if im.ModelID() != "flux-pro" {
		t.Fatalf("default model ID = %q, want flux-pro", im.ModelID())
	}
}

func TestProviderDefaultsMatchTypeScript(t *testing.T) {
	orig := os.Getenv("BFL_API_KEY")
	t.Cleanup(func() {
		if orig == "" {
			_ = os.Unsetenv("BFL_API_KEY")
		} else {
			_ = os.Setenv("BFL_API_KEY", orig)
		}
	})
	if err := os.Setenv("BFL_API_KEY", "env-key"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	p := New(Config{})
	if p.baseURL() != "https://api.bfl.ai/v1" {
		t.Fatalf("baseURL = %q", p.baseURL())
	}
	if p.config.APIKey != "env-key" {
		t.Fatalf("APIKey = %q", p.config.APIKey)
	}
}

func TestImageModelEndpointSelection(t *testing.T) {
	p := New(Config{APIKey: "k"})
	tests := map[string]string{
		"flux-pro":     "/flux-pro",
		"flux-pro-1.1": "/flux-pro-1.1",
		"flux-dev":     "/flux-dev",
		"flux-schnell": "/flux-schnell",
		"unknown":      "/unknown",
	}
	for id, want := range tests {
		m := NewImageModel(p, id)
		if got := m.getEndpoint(); got != want {
			t.Fatalf("getEndpoint(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestBuildRequestBodyAndConvertErrors(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "flux-pro")

	body := m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "a fox",
		Size:   "1280x720",
	})
	if body["prompt"] != "a fox" {
		t.Fatalf("prompt = %#v", body["prompt"])
	}
	if body["width"] != 1280 || body["height"] != 720 {
		t.Fatalf("size parse failed: %#v", body)
	}
	if body["aspect_ratio"] != "16:9" {
		t.Fatalf("aspect_ratio = %#v, want 16:9", body["aspect_ratio"])
	}
	warnings := bflWarnings(&provider.ImageGenerateOptions{Size: "1280x720"})
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "size" {
		t.Fatalf("warnings = %#v", warnings)
	}

	body = m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:      "a fox",
		Size:        "1280x720",
		AspectRatio: "1:1",
		ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{
			"promptUpsampling":    true,
			"unsupportedProperty": "value",
			"pollIntervalMillis":  1,
		}},
	})
	if body["aspect_ratio"] != "1:1" || body["width"] != 1280 || body["height"] != 720 {
		t.Fatalf("aspectRatio override body = %#v", body)
	}
	if body["prompt_upsampling"] != true {
		t.Fatalf("prompt_upsampling = %#v", body["prompt_upsampling"])
	}
	if _, ok := body["unsupportedProperty"]; ok {
		t.Fatalf("unsupported provider option should be stripped: %#v", body)
	}
	if _, ok := body["pollIntervalMillis"]; ok {
		t.Fatalf("pollIntervalMillis should not be sent in request body: %#v", body)
	}
	warnings = bflWarnings(&provider.ImageGenerateOptions{Size: "1280x720", AspectRatio: "1:1"})
	if len(warnings) != 1 || !strings.Contains(warnings[0].Details, "ignores size") {
		t.Fatalf("aspect warning = %#v", warnings)
	}

	fill := NewImageModel(p, "flux-pro-1.0-fill")
	fillBody := fill.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "fill it",
		Files:  []provider.ImageFile{{Data: []byte("image"), MediaType: "image/png"}},
	})
	if _, hasOldKey := fillBody["input_image"]; hasOldKey {
		t.Fatalf("fill model must use image key, got %#v", fillBody)
	}
	if fillBody["image"] != "aW1hZ2U=" {
		t.Fatalf("fill image = %#v", fillBody["image"])
	}

	if _, err := m.convertResponse(t.Context(), bflResult{}, bflCreateResponse{}, nil, nil); err == nil {
		t.Fatal("convertResponse should fail on empty sample URL")
	}
}
