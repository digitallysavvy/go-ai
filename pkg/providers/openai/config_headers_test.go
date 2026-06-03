package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestConfigHeadersApplyToAllOpenAIRequestPathsAndCanOverrideSDKHeaders(t *testing.T) {
	captured := map[string]http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured[r.URL.Path] = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		case "/responses":
			_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
		case "/embeddings":
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1],"index":0}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
		case "/images/generations":
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`))
		case "/audio/speech":
			_, _ = w.Write([]byte("audio-bytes"))
		case "/audio/transcriptions":
			_, _ = w.Write([]byte(`{"text":"hello","duration":1}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New(Config{
		APIKey:       "sdk-key",
		BaseURL:      server.URL,
		Organization: "sdk-org",
		Project:      "sdk-project",
		Headers: map[string]string{
			"X-Custom-Header":     "custom",
			"Authorization":       "Bearer user-key",
			"OpenAI-Organization": "user-org",
			"OpenAI-Project":      "user-project",
		},
	})

	languageModel, err := p.ChatModel(ModelGPT4oMini)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := languageModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:  types.Prompt{Text: "hello"},
		Headers: map[string]string{"X-Call-Header": "chat"},
	}); err != nil {
		t.Fatalf("language DoGenerate error = %v", err)
	}

	responsesModel, err := p.ResponsesModel(ModelGPT5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := responsesModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:  types.Prompt{Text: "hello"},
		Headers: map[string]string{"X-Call-Header": "responses"},
	}); err != nil {
		t.Fatalf("responses DoGenerate error = %v", err)
	}

	embeddingModel, err := p.EmbeddingModel(ModelTextEmbedding3Small)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := embeddingModel.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{
		Headers: map[string]string{"X-Call-Header": "embeddings"},
	}); err != nil {
		t.Fatalf("embedding DoEmbed error = %v", err)
	}

	imageModel, err := p.ImageModel(ModelGPTImage1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := imageModel.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:  "a cat",
		Headers: map[string]string{"X-Call-Header": "images"},
	}); err != nil {
		t.Fatalf("image DoGenerate error = %v", err)
	}

	speechModel, err := p.SpeechModel(ModelTTS1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := speechModel.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:    "hello",
		Headers: map[string]string{"X-Call-Header": "speech"},
	}); err != nil {
		t.Fatalf("speech DoGenerate error = %v", err)
	}

	transcriptionModel, err := p.TranscriptionModel(ModelWhisper1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transcriptionModel.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/wav",
		Headers:  map[string]string{"X-Call-Header": "transcription"},
	}); err != nil {
		t.Fatalf("transcription DoTranscribe error = %v", err)
	}

	wantCallHeaders := map[string]string{
		"/chat/completions":     "chat",
		"/responses":            "responses",
		"/embeddings":           "embeddings",
		"/images/generations":   "images",
		"/audio/speech":         "speech",
		"/audio/transcriptions": "transcription",
	}
	for _, path := range []string{"/chat/completions", "/responses", "/embeddings", "/images/generations", "/audio/speech", "/audio/transcriptions"} {
		headers, ok := captured[path]
		if !ok {
			t.Fatalf("path %s was not called", path)
		}
		if headers.Get("X-Custom-Header") != "custom" {
			t.Fatalf("%s X-Custom-Header = %q, want custom", path, headers.Get("X-Custom-Header"))
		}
		if headers.Get("X-Call-Header") != wantCallHeaders[path] {
			t.Fatalf("%s X-Call-Header = %q, want %q", path, headers.Get("X-Call-Header"), wantCallHeaders[path])
		}
		if headers.Get("Authorization") != "Bearer user-key" {
			t.Fatalf("%s Authorization = %q, want user key", path, headers.Get("Authorization"))
		}
		if headers.Get("OpenAI-Organization") != "user-org" {
			t.Fatalf("%s OpenAI-Organization = %q, want user-org", path, headers.Get("OpenAI-Organization"))
		}
		if headers.Get("OpenAI-Project") != "user-project" {
			t.Fatalf("%s OpenAI-Project = %q, want user-project", path, headers.Get("OpenAI-Project"))
		}
	}

	if contentType := captured["/audio/transcriptions"].Get("Content-Type"); !strings.HasPrefix(contentType, "multipart/form-data") {
		t.Fatalf("transcription Content-Type = %q, want multipart/form-data", contentType)
	}
}

func TestRequestHeadersOverrideProviderHeaders(t *testing.T) {
	var captured http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "sdk-key",
		BaseURL: server.URL,
		Headers: map[string]string{
			"Authorization": "Bearer provider-key",
			"X-Scope":       "provider",
		},
	})

	languageModel, err := p.ChatModel(ModelGPT4oMini)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := languageModel.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Headers: map[string]string{
			"Authorization": "Bearer request-key",
			"X-Scope":       "request",
		},
	}); err != nil {
		t.Fatalf("language DoGenerate error = %v", err)
	}

	if captured.Get("Authorization") != "Bearer request-key" {
		t.Fatalf("Authorization = %q, want request key", captured.Get("Authorization"))
	}
	if captured.Get("X-Scope") != "request" {
		t.Fatalf("X-Scope = %q, want request", captured.Get("X-Scope"))
	}
}
