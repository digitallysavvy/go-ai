package together

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported (behaviorally) from ai/packages/togetherai/src/togetherai-image-model.test.ts's
// non-diffusion (google/gemini-3-pro-image) coverage.

func TestImageModel_NonDiffusionModel_OmitsSeedAndDiffusionOptions(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "google/gemini-3-pro-image")

	seed := 42
	opts := &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		Seed:   &seed,
		ProviderOptions: map[string]interface{}{
			"togetherai": map[string]interface{}{
				"steps":                  float64(20),
				"guidance":               float64(7.5),
				"negative_prompt":        "blurry",
				"disable_safety_checker": true,
				"response_format":        "base64",
			},
		},
	}

	body := m.buildRequestBody(opts)
	if _, ok := body["seed"]; ok {
		t.Fatalf("expected seed to be omitted for a non-diffusion model, got %#v", body["seed"])
	}
	for _, key := range []string{"steps", "guidance", "negative_prompt", "disable_safety_checker"} {
		if _, ok := body[key]; ok {
			t.Fatalf("expected %s to be stripped for a non-diffusion model, got %#v", key, body[key])
		}
	}

	warnings := togetherWarnings(m.modelID, opts)
	foundSeedWarning := false
	for _, w := range warnings {
		if w.Feature == "seed" {
			foundSeedWarning = true
		}
	}
	if !foundSeedWarning {
		t.Fatalf("expected an unsupported-seed warning, got %+v", warnings)
	}
}

func TestImageModel_DiffusionModel_KeepsSeedAndOptions(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "black-forest-labs/FLUX.1-schnell")

	seed := 42
	opts := &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		Seed:   &seed,
		ProviderOptions: map[string]interface{}{
			"togetherai": map[string]interface{}{
				"steps": float64(20),
			},
		},
	}

	body := m.buildRequestBody(opts)
	if body["seed"] != 42 {
		t.Fatalf("expected seed to be forwarded for a diffusion model, got %#v", body["seed"])
	}
	if body["steps"] != float64(20) {
		t.Fatalf("expected steps to be forwarded for a diffusion model, got %#v", body["steps"])
	}

	warnings := togetherWarnings(m.modelID, opts)
	for _, w := range warnings {
		if w.Feature == "seed" {
			t.Fatalf("did not expect an unsupported-seed warning for a diffusion model, got %+v", warnings)
		}
	}
}

func TestLanguageModel_StreamingIncludesUsageOption(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewLanguageModel(p, "test-model")
	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	}, true)

	streamOptions, ok := body["stream_options"].(map[string]interface{})
	if !ok {
		t.Fatalf("stream_options type = %T", body["stream_options"])
	}
	if streamOptions["include_usage"] != true {
		t.Fatalf("include_usage = %#v, want true", streamOptions["include_usage"])
	}
}

func TestLanguageModel_NonStreamingOmitsStreamOptions(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model := NewLanguageModel(p, "test-model")
	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	}, false)

	if _, ok := body["stream_options"]; ok {
		t.Fatalf("did not expect stream_options on a non-streaming request, got %#v", body["stream_options"])
	}
}
