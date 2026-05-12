package stability

import (
	"encoding/base64"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestProviderFactoriesAndUnsupported(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "stability" {
		t.Fatalf("Name = %q", p.Name())
	}

	if lm, err := p.LanguageModel("x"); lm != nil || err == nil {
		t.Fatalf("LanguageModel expected unsupported error, got model=%v err=%v", lm, err)
	}
	if em, err := p.EmbeddingModel("x"); em != nil || err == nil {
		t.Fatalf("EmbeddingModel expected unsupported error, got model=%v err=%v", em, err)
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

	im, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel: %v", err)
	}
	if im.ModelID() != "stable-diffusion-xl-1024-v1-0" {
		t.Fatalf("default model ID = %q", im.ModelID())
	}
}

func TestImageModelBuildAndConvert(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "stable")

	body := m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "cat",
		Size:   "1024x1024",
	})
	if body["samples"] != 1 {
		t.Fatalf("samples default = %#v", body["samples"])
	}
	if body["width"] != 1024 || body["height"] != 1024 {
		t.Fatalf("size defaults missing: %#v", body)
	}

	body = m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "cat",
		N:      intPtr(2),
	})
	if body["samples"] != 2 {
		t.Fatalf("samples override = %#v", body["samples"])
	}

	pngBytes := []byte{0x89, 'P', 'N', 'G'}
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	got, err := m.convertResponse(stabilityImageResponse{
		Artifacts: []struct {
			Base64       string `json:"base64"`
			FinishReason string `json:"finishReason"`
			Seed         int    `json:"seed"`
		}{
			{Base64: encoded},
		},
	})
	if err != nil {
		t.Fatalf("convertResponse: %v", err)
	}
	if string(got.Image) != string(pngBytes) {
		t.Fatalf("decoded bytes mismatch: %#v", got.Image)
	}
}

func intPtr(v int) *int { return &v }
