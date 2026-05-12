package huggingface

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProviderFactoriesAndDefaults(t *testing.T) {
	p := New(Config{APIKey: "hf"})
	if p.Name() != "huggingface" {
		t.Fatalf("Name = %q", p.Name())
	}

	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("LanguageModel should require model ID")
	}
	em, err := p.EmbeddingModel("")
	if err != nil {
		t.Fatalf("EmbeddingModel: %v", err)
	}
	if em.ModelID() != "sentence-transformers/all-MiniLM-L6-v2" {
		t.Fatalf("default embedding model = %q", em.ModelID())
	}
	im, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel: %v", err)
	}
	if im.ModelID() != "stabilityai/stable-diffusion-2-1" {
		t.Fatalf("default image model = %q", im.ModelID())
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

func TestLanguageModelBuildRequestBodyAndConversions(t *testing.T) {
	p := New(Config{APIKey: "hf"})
	m := NewLanguageModel(p, "meta/test")
	temp := 0.3
	maxTokens := 128

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: "user",
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
			},
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	})
	if _, ok := body["inputs"]; !ok {
		t.Fatalf("inputs missing from body: %#v", body)
	}
	params, ok := body["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters missing from body: %#v", body)
	}
	if params["temperature"] != temp || params["max_new_tokens"] != maxTokens {
		t.Fatalf("unexpected parameters: %#v", params)
	}

	res, err := m.convertResponse([]byte(`[{"generated_text":"ok"}]`))
	if err != nil || res.Text != "ok" {
		t.Fatalf("array convertResponse = %#v err=%v", res, err)
	}
	res, err = m.convertResponse([]byte(`{"generated_text":"ok2"}`))
	if err != nil || res.Text != "ok2" {
		t.Fatalf("object convertResponse = %#v err=%v", res, err)
	}
	if _, err := m.convertResponse([]byte(`{"error":"rate limit"}`)); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected API error, got %v", err)
	}
	if _, err := m.convertResponse([]byte(`{"foo":"bar"}`)); err == nil {
		t.Fatal("expected unexpected-format error")
	}
}

func TestStreamChunks(t *testing.T) {
	s := &huggingfaceStream{
		result: &types.GenerateResult{Text: "abcdefghijk"},
	}
	ch1, err := s.Next()
	if err != nil || ch1.Type != "text" {
		t.Fatalf("chunk1 = %#v err=%v", ch1, err)
	}
	ch2, err := s.Next()
	if err != nil || ch2.Type != "text" {
		t.Fatalf("chunk2 = %#v err=%v", ch2, err)
	}
	ch3, err := s.Next()
	if err != nil || ch3.Type != "finish" {
		t.Fatalf("chunk3 = %#v err=%v", ch3, err)
	}
	if _, err := s.Next(); err == nil {
		t.Fatal("expected stream exhausted error")
	}
}
