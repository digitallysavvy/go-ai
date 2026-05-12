package fal

import "testing"

func TestProviderDefaultsAndUnsupportedModels(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "fal" || p.Client() == nil {
		t.Fatalf("unexpected provider: %+v", p)
	}

	imgAny, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	img := imgAny.(*ImageModel)
	if img.ModelID() != "fast-sdxl" {
		t.Fatalf("default image model = %q", img.ModelID())
	}

	videoAny, err := p.VideoModel("")
	if err != nil {
		t.Fatalf("VideoModel() error = %v", err)
	}
	video := videoAny.(*VideoModel)
	if video.ModelID() != "luma-ray" {
		t.Fatalf("default video model = %q", video.ModelID())
	}

	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("expected language unsupported error")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("expected embedding unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected speech unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected transcription unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected reranking unsupported error")
	}
}
