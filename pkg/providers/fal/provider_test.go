package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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
	if img.ModelID() != "fal-ai/fast-sdxl" {
		t.Fatalf("default image model = %q", img.ModelID())
	}

	videoAny, err := p.VideoModel("")
	if err != nil {
		t.Fatalf("VideoModel() error = %v", err)
	}
	video := videoAny.(*VideoModel)
	if video.ModelID() != "fal-ai/luma-ray" {
		t.Fatalf("default video model = %q", video.ModelID())
	}

	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("expected language unsupported error")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("expected embedding unsupported error")
	}

	speechAny, err := p.SpeechModel("fal-ai/minimax/speech-02-hd")
	if err != nil {
		t.Fatalf("SpeechModel() error = %v", err)
	}
	if speechAny.ModelID() != "fal-ai/minimax/speech-02-hd" {
		t.Fatalf("speech model id = %q", speechAny.ModelID())
	}

	transcriptionAny, err := p.TranscriptionModel("wizper")
	if err != nil {
		t.Fatalf("TranscriptionModel() error = %v", err)
	}
	if transcriptionAny.ModelID() != "wizper" {
		t.Fatalf("transcription model id = %q", transcriptionAny.ModelID())
	}

	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected reranking unsupported error")
	}
}

func TestProviderDefaultBaseURLConstant(t *testing.T) {
	// TS: `const defaultBaseURL = 'https://fal.run';` (fal-provider.ts).
	if defaultBaseURL != "https://fal.run" {
		t.Fatalf("defaultBaseURL = %q, want https://fal.run", defaultBaseURL)
	}
	if falRunHost != "https://fal.run" {
		t.Fatalf("falRunHost = %q, want https://fal.run", falRunHost)
	}
	if falQueueHost != "https://queue.fal.run" {
		t.Fatalf("falQueueHost = %q, want https://queue.fal.run", falQueueHost)
	}

	p := New(Config{APIKey: "k"})
	if p.speechHost != falRunHost || p.queueHost != falQueueHost {
		t.Fatalf("unexpected default hosts: speechHost=%q queueHost=%q", p.speechHost, p.queueHost)
	}
}

func TestProviderDefaultBaseURLAndRequestPath(t *testing.T) {
	// B1-4: default base URL must be "https://fal.run" (no "/fal-ai"
	// segment), so fully-qualified model IDs like "fal-ai/kling-video/..."
	// aren't double-prefixed into "https://fal.run/fal-ai/fal-ai/...".
	var gotPath string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"images":[{"url":"data:image/png;base64,ZmFrZQ==","content_type":"image/png"}]}`))
	}))
	defer apiSrv.Close()

	// Simulate the default base URL by pointing BaseURL at the test server
	// root (equivalent to "https://fal.run" for path-construction purposes).
	p2 := New(Config{APIKey: "k", BaseURL: apiSrv.URL})
	m := NewImageModel(p2, "fal-ai/kling-video/v2.5-turbo/pro/image-to-video")
	if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "test"}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotPath != "/fal-ai/kling-video/v2.5-turbo/pro/image-to-video" {
		t.Fatalf("request path = %q, want /fal-ai/kling-video/v2.5-turbo/pro/image-to-video (no doubled fal-ai prefix)", gotPath)
	}
}
