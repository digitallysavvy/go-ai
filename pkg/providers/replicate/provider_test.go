package replicate

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProviderFactoriesAndUnsupported(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "replicate" {
		t.Fatalf("Name = %q", p.Name())
	}

	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("LanguageModel should require model ID")
	}
	if em, err := p.EmbeddingModel("x"); em != nil || err == nil {
		t.Fatalf("EmbeddingModel expected unsupported error, got model=%v err=%v", em, err)
	}
	im, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel: %v", err)
	}
	if im.ModelID() == "" {
		t.Fatal("default image model ID should not be empty")
	}
	if vm, err := p.VideoModel(""); vm != nil || err == nil {
		t.Fatalf("VideoModel should require model ID, got model=%v err=%v", vm, err)
	}
	if sm, err := p.SpeechModel("x"); sm != nil || err == nil {
		t.Fatalf("SpeechModel expected unsupported error, got model=%v err=%v", sm, err)
	}
	if tm, err := p.TranscriptionModel("x"); tm != nil || err == nil {
		t.Fatalf("TranscriptionModel expected unsupported error, got model=%v err=%v", tm, err)
	}
	if rm, err := p.RerankingModel("x"); rm != nil || err == nil {
		t.Fatalf("RerankingModel expected unsupported error, got model=%v err=%v", rm, err)
	}
}

func TestLanguageModelBuildAndConvert(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "model-version")
	temp := 0.4
	max := 100

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: "system",
					Content: []types.ContentPart{
						types.TextContent{Text: "be brief"},
					},
				},
				{
					Role: "user",
					Content: []types.ContentPart{
						types.TextContent{Text: "hello"},
					},
				},
			},
		},
		Temperature: &temp,
		MaxTokens:   &max,
	})
	if body["version"] != "model-version" {
		t.Fatalf("version mismatch: %#v", body["version"])
	}
	input := body["input"].(map[string]interface{})
	if input["temperature"] != temp || input["max_tokens"] != max {
		t.Fatalf("optional params missing: %#v", input)
	}

	gr := m.convertResponse(replicatePrediction{Output: "hello"})
	if gr.Text != "hello" {
		t.Fatalf("string output mismatch: %#v", gr)
	}
	gr = m.convertResponse(replicatePrediction{Output: []interface{}{"a", "b"}})
	if gr.Text != "ab" {
		t.Fatalf("slice output mismatch: %#v", gr)
	}
}

func TestImageModelBuildAndConvertErrors(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "img-model")
	n := 3
	body := m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "draw",
		N:      &n,
		Size:   "1280x720",
	})
	if body["version"] != "img-model" {
		t.Fatalf("version mismatch: %#v", body["version"])
	}
	input := body["input"].(map[string]interface{})
	if input["num_outputs"] != 3 || input["width"] != 1280 || input["height"] != 720 {
		t.Fatalf("input mismatch: %#v", input)
	}

	if _, err := m.convertResponse(t.Context(), replicateImagePrediction{Output: nil}); err == nil {
		t.Fatal("convertResponse should fail when image URL is missing")
	}
}

func TestVideoModelHelpers(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewVideoModel(p, "video-model")
	dur := 5.0
	fps := 24
	seed := 42
	req := m.buildPredictionRequest(&provider.VideoModelV3CallOptions{
		Prompt:      "A running horse",
		AspectRatio: "16:9",
		Duration:    &dur,
		FPS:         &fps,
		Seed:        &seed,
		ProviderOptions: map[string]interface{}{
			"replicate": map[string]interface{}{
				"pollIntervalMs": 500,
				"pollTimeoutMs":  20000,
				"motion_bucket":  127,
			},
		},
	})
	if req["version"] != "video-model" {
		t.Fatalf("version mismatch: %#v", req["version"])
	}
	input := req["input"].(map[string]interface{})
	if input["aspect_ratio"] != "16:9" || input["duration"] != dur || input["fps"] != fps {
		t.Fatalf("video input mismatch: %#v", input)
	}
	if _, ok := input["pollIntervalMs"]; ok {
		t.Fatalf("pollIntervalMs should not be forwarded in input: %#v", input)
	}
	if input["motion_bucket"] != 127 {
		t.Fatalf("provider option should be forwarded: %#v", input)
	}

	pollOpts := m.getPollOptions(map[string]interface{}{
		"replicate": map[string]interface{}{
			"pollIntervalMs": 800,
			"pollTimeoutMs":  12000,
		},
	})
	if pollOpts.PollIntervalMs != 800 || pollOpts.PollTimeoutMs != 12000 {
		t.Fatalf("poll options mismatch: %#v", pollOpts)
	}

	if _, err := m.convertResponse(t.Context(), &replicateVideoPrediction{}); err == nil {
		t.Fatal("convertResponse should fail when output is empty")
	}
}
