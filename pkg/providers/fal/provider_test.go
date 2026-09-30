package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestProviderAPIKeyFromEnv ports loadFalApiKey's precedence (fal-provider.ts:
// explicit apiKey wins; otherwise FAL_API_KEY, falling back to FAL_KEY).
func TestProviderAPIKeyFromEnv(t *testing.T) {
	t.Run("explicit APIKey wins over env vars", func(t *testing.T) {
		t.Setenv("FAL_API_KEY", "from-fal-api-key")
		t.Setenv("FAL_KEY", "from-fal-key")
		p := New(Config{APIKey: "explicit-key"})
		if got := p.client.Headers()["authorization"]; got != "Key explicit-key" {
			t.Fatalf("Authorization = %q, want Key explicit-key", got)
		}
	})

	t.Run("FAL_API_KEY used when APIKey unset", func(t *testing.T) {
		t.Setenv("FAL_API_KEY", "from-fal-api-key")
		t.Setenv("FAL_KEY", "from-fal-key")
		p := New(Config{})
		if got := p.client.Headers()["authorization"]; got != "Key from-fal-api-key" {
			t.Fatalf("Authorization = %q, want Key from-fal-api-key", got)
		}
	})

	t.Run("falls back to FAL_KEY when FAL_API_KEY unset", func(t *testing.T) {
		t.Setenv("FAL_API_KEY", "")
		t.Setenv("FAL_KEY", "from-fal-key")
		p := New(Config{})
		if got := p.client.Headers()["authorization"]; got != "Key from-fal-key" {
			t.Fatalf("Authorization = %q, want Key from-fal-key", got)
		}
	})
}

// TestProviderConfigHeaders_AppliedToAllModelRequests verifies
// FalProviderSettings.headers (Go: Config.Headers) is merged into every
// model type's requests -- image, video, speech, transcription -- matching
// the TS SDK's single getHeaders() shared by every model factory
// (fal-provider.ts).
func TestProviderConfigHeaders_AppliedToAllModelRequests(t *testing.T) {
	var gotImageHeaders, gotSpeechHeaders, gotTranscriptionSubmitHeaders http.Header

	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/fast-sdxl", func(w http.ResponseWriter, r *http.Request) {
		gotImageHeaders = r.Header
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"images":[{"url":"data:image/png;base64,ZmFrZQ==","content_type":"image/png"}]}`))
	})
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		gotSpeechHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(make([]byte, 10), nil))
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		gotTranscriptionSubmitHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"request_id":"test-id"}`))
	})
	mux.HandleFunc("/fal-ai/wizper/requests/test-id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"text":"hi"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New(Config{
		APIKey:  "k",
		BaseURL: srv.URL,
		Headers: map[string]string{"X-Custom-Provider-Header": "provider-value"},
	})
	p.speechHost = srv.URL
	p.queueHost = srv.URL

	img := NewImageModel(p, "fal-ai/fast-sdxl")
	if _, err := img.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "test"}); err != nil {
		t.Fatalf("ImageModel.DoGenerate() error = %v", err)
	}
	if got := gotImageHeaders.Get("X-Custom-Provider-Header"); got != "provider-value" {
		t.Fatalf("image request X-Custom-Provider-Header = %q", got)
	}

	speech := NewSpeechModel(p, "fal-ai/minimax/speech-02-hd")
	if _, err := speech.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"}); err != nil {
		t.Fatalf("SpeechModel.DoGenerate() error = %v", err)
	}
	if got := gotSpeechHeaders.Get("X-Custom-Provider-Header"); got != "provider-value" {
		t.Fatalf("speech request X-Custom-Provider-Header = %q", got)
	}

	transcription := NewTranscriptionModel(p, "wizper")
	if _, err := transcription.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"}); err != nil {
		t.Fatalf("TranscriptionModel.DoTranscribe() error = %v", err)
	}
	if got := gotTranscriptionSubmitHeaders.Get("X-Custom-Provider-Header"); got != "provider-value" {
		t.Fatalf("transcription request X-Custom-Provider-Header = %q", got)
	}
}

// TestProviderConfigHeaders_RequestHeadersOverrideProviderHeaders verifies
// combineHeaders' precedence: a per-request header of the same name
// overrides the provider-level Config.Headers value.
func TestProviderConfigHeaders_RequestHeadersOverrideProviderHeaders(t *testing.T) {
	var gotHeaders http.Header
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(make([]byte, 10), nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New(Config{
		APIKey:  "k",
		Headers: map[string]string{"X-Custom-Header": "provider-value"},
	})
	p.speechHost = srv.URL

	speech := NewSpeechModel(p, "fal-ai/minimax/speech-02-hd")
	_, err := speech.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:    "hi",
		Headers: map[string]string{"X-Custom-Header": "request-value"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if got := gotHeaders.Get("X-Custom-Header"); got != "request-value" {
		t.Fatalf("X-Custom-Header = %q, want request-value (request header should win)", got)
	}
}

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
