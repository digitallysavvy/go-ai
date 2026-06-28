package azure

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAzureEmbeddingModelDoEmbedAndDoEmbedManyHTTP(t *testing.T) {
	var seenURI string
	var seenBody map[string]interface{}
	var seenUserAgent string
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenURI = r.URL.RequestURI()
		seenUserAgent = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		if _, ok := seenBody["input"].([]interface{}); ok {
			return azureJSONResponse(200, `{"object":"list","data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[3,4]}],"usage":{"prompt_tokens":7,"total_tokens":9}}`, map[string]string{"X-Req": "r1"}), nil
		}
		return azureJSONResponse(200, `{"object":"list","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":4}}`, map[string]string{"X-Req": "r1"}), nil
	})

	m := NewEmbeddingModel(p, "dep")
	one, err := m.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "yes"}})
	if err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if !strings.HasPrefix(seenURI, "/v1/embeddings?api-version=2024-10-21") || seenBody["input"] != "hello" || seenBody["model"] != "dep" {
		t.Fatalf("request mismatch uri=%q body=%#v", seenURI, seenBody)
	}
	if len(one.Embedding) != 2 || one.Response.Headers["X-Req"] != "r1" {
		t.Fatalf("result mismatch: %#v", one)
	}
	if seenUserAgent != "go-ai/azure/0.5.0" {
		t.Fatalf("User-Agent = %q", seenUserAgent)
	}

	many, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if len(many.Embeddings) != 2 || many.Responses[0].Headers["X-Req"] != "r1" {
		t.Fatalf("many result mismatch: %#v", many)
	}
}

func TestAzureLanguageModelDoGenerateHTTP(t *testing.T) {
	var seenURI string
	var seenBody map[string]interface{}
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenURI = r.URL.RequestURI()
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return azureJSONResponse(200, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok","tool_calls":[]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, nil), nil
	})

	m := NewLanguageModel(p, "dep")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if !strings.HasPrefix(seenURI, "/v1/chat/completions?api-version=2024-10-21") {
		t.Fatalf("path = %q", seenURI)
	}
	if seenBody["model"] != "dep" {
		t.Fatalf("model mismatch: %#v", seenBody)
	}
	if _, ok := seenBody["stream"]; ok {
		t.Fatalf("stream = %#v, want omitted for non-streaming request", seenBody["stream"])
	}
	if res.Text != "ok" {
		t.Fatalf("result text = %q", res.Text)
	}
}

func TestAzureImageAndSpeechDoGenerateHTTP(t *testing.T) {
	pImage := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.RequestURI(), "/v1/images/generations?api-version=2024-10-21") {
			t.Fatalf("image path = %q", r.URL.RequestURI())
		}
		return azureJSONResponse(200, `{"data":[{"url":"https://example.com/image.png"}]}`, nil), nil
	})
	im := NewImageModel(pImage, "img")
	img, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
	if err != nil {
		t.Fatalf("image DoGenerate error = %v", err)
	}
	if img.URL == "" {
		t.Fatalf("expected URL image result: %#v", img)
	}

	pSpeech := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.RequestURI(), "/v1/audio/speech?api-version=2024-10-21") {
			t.Fatalf("speech path = %q", r.URL.RequestURI())
		}
		return azureTextResponse(200, "AUDIO"), nil
	})
	sm := NewSpeechModel(pSpeech, "tts")
	speech, err := sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("speech DoGenerate error = %v", err)
	}
	if string(speech.Audio) != "AUDIO" {
		t.Fatalf("speech result mismatch: %#v", speech)
	}
}

func TestAzureTranscriptionDoTranscribeHTTPAndSerialization(t *testing.T) {
	var seenBody string
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.RequestURI(), "/v1/audio/transcriptions?api-version=2024-10-21") {
			t.Fatalf("transcription path = %q", r.URL.RequestURI())
		}
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		return azureJSONResponse(200, `{"text":"hello","duration":1.5,"segments":[{"text":"hello","start":0.0,"end":1.5}]}`, nil), nil
	})

	tm := NewTranscriptionModel(p, "whisper")
	res, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:      []byte("audio"),
		MimeType:   "audio/mpeg",
		Language:   "en",
		Timestamps: true,
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if !strings.Contains(seenBody, `name="response_format"`) || !strings.Contains(seenBody, "verbose_json") {
		t.Fatalf("missing verbose response format in multipart body")
	}
	if !strings.Contains(seenBody, `name="model"`) || !strings.Contains(seenBody, "whisper") {
		t.Fatalf("missing model in multipart body")
	}
	if res.Text != "hello" || len(res.Timestamps) != 1 {
		t.Fatalf("transcription result mismatch: %#v", res)
	}

	modelAny, err := p.ChatModel("whisper")
	if err != nil {
		t.Fatalf("ChatModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "azure.chat" || restored.ModelID() != "whisper" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestAzureADTokenProviderAuthenticatesEveryRequestSurface(t *testing.T) {
	var tokenCalls int
	var seenPaths []string
	p := mustNewProvider(t, Config{
		BaseURL:      "https://azure.example",
		DeploymentID: "dep",
		APIVersion:   "2024-10-21",
		ADTokenProvider: func(ctx context.Context) (string, error) {
			if ctx == nil {
				t.Fatal("token provider received nil context")
			}
			tokenCalls++
			return "entra-token", nil
		},
		HTTPClient: &http.Client{Transport: azureRoundTripper(func(r *http.Request) (*http.Response, error) {
			seenPaths = append(seenPaths, r.URL.Path)
			if got := r.Header.Get("Authorization"); got != "Bearer entra-token" {
				t.Fatalf("Authorization = %q", got)
			}
			if got := r.Header.Get("api-key"); got != "" {
				t.Fatalf("api-key should be omitted when ADTokenProvider is set, got %q", got)
			}
			switch r.URL.Path {
			case "/v1/chat/completions":
				return azureJSONResponse(200, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, nil), nil
			case "/v1/embeddings":
				return azureJSONResponse(200, `{"object":"list","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":4}}`, nil), nil
			case "/v1/images/generations":
				return azureJSONResponse(200, `{"data":[{"url":"https://example.com/image.png"}]}`, nil), nil
			case "/v1/audio/speech":
				return azureTextResponse(200, "AUDIO"), nil
			case "/v1/audio/transcriptions":
				return azureJSONResponse(200, `{"text":"hello"}`, nil), nil
			default:
				t.Fatalf("unexpected path %q", r.URL.Path)
				return nil, nil
			}
		})},
	})

	if _, err := NewLanguageModel(p, "chat").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("language DoGenerate error = %v", err)
	}
	if _, err := NewEmbeddingModel(p, "embed").DoEmbed(context.Background(), "hello", nil); err != nil {
		t.Fatalf("embedding DoEmbed error = %v", err)
	}
	if _, err := NewImageModel(p, "image").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"}); err != nil {
		t.Fatalf("image DoGenerate error = %v", err)
	}
	if _, err := NewSpeechModel(p, "tts").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"}); err != nil {
		t.Fatalf("speech DoGenerate error = %v", err)
	}
	if _, err := NewTranscriptionModel(p, "whisper").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("audio"), MimeType: "audio/mpeg"}); err != nil {
		t.Fatalf("transcription DoTranscribe error = %v", err)
	}
	if tokenCalls != 5 {
		t.Fatalf("token provider calls = %d, want 5; paths=%v", tokenCalls, seenPaths)
	}
}

func TestAzureADTokenProviderDoesNotOverrideExplicitAuthorizationHeader(t *testing.T) {
	var tokenCalls int
	p := mustNewProvider(t, Config{
		BaseURL:      "https://azure.example",
		DeploymentID: "dep",
		APIVersion:   "2024-10-21",
		ADTokenProvider: func(ctx context.Context) (string, error) {
			tokenCalls++
			return "entra-token", nil
		},
		HTTPClient: &http.Client{Transport: azureRoundTripper(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("Authorization"); got != "Bearer caller-token" {
				t.Fatalf("Authorization = %q", got)
			}
			if got := r.Header.Get("api-key"); got != "" {
				t.Fatalf("api-key should be omitted when ADTokenProvider is set, got %q", got)
			}
			return azureJSONResponse(200, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, nil), nil
		})},
	})
	_, err := NewLanguageModel(p, "chat").DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:  types.Prompt{Text: "hi"},
		Headers: map[string]string{"Authorization": "Bearer caller-token"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if tokenCalls != 0 {
		t.Fatalf("token provider calls = %d, want 0", tokenCalls)
	}
}

func TestAzureADTokenProviderPreservesCallerAPIKeyHeader(t *testing.T) {
	var tokenCalls int
	p := mustNewProvider(t, Config{
		BaseURL:      "https://azure.example",
		DeploymentID: "dep",
		APIVersion:   "2024-10-21",
		ADTokenProvider: func(ctx context.Context) (string, error) {
			tokenCalls++
			return "entra-token", nil
		},
		HTTPClient: &http.Client{Transport: azureRoundTripper(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("Authorization"); got != "Bearer entra-token" {
				t.Fatalf("Authorization = %q", got)
			}
			if got := r.Header.Get("api-key"); got != "caller-api-key" {
				t.Fatalf("api-key = %q", got)
			}
			return azureJSONResponse(200, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, nil), nil
		})},
	})
	_, err := NewLanguageModel(p, "chat").DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:  types.Prompt{Text: "hi"},
		Headers: map[string]string{"api-key": "caller-api-key"},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token provider calls = %d, want 1", tokenCalls)
	}
}

func TestAzureUseDeploymentBasedURLs(t *testing.T) {
	var seenURI string
	p := mustNewProvider(t, Config{
		APIKey:                 "k",
		BaseURL:                "https://azure.example/openai",
		DeploymentID:           "dep",
		APIVersion:             "v1",
		UseDeploymentBasedURLs: true,
	})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://azure.example/openai",
		Headers: map[string]string{"api-key": "k"},
		HTTPClient: &http.Client{Transport: azureRoundTripper(func(r *http.Request) (*http.Response, error) {
			seenURI = r.URL.RequestURI()
			return azureJSONResponse(200, `{"text":"hello"}`, nil), nil
		})},
	})

	_, err := NewTranscriptionModel(p, "whisper-1").DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio"),
		MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if !strings.HasPrefix(seenURI, "/openai/deployments/whisper-1/audio/transcriptions?api-version=v1") {
		t.Fatalf("deployment-based path = %q", seenURI)
	}
}

func TestAzureModelErrorPaths(t *testing.T) {
	p := newAzureProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewImageModel(p, "img").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"}); err == nil {
		t.Fatal("expected image error")
	}
	if _, err := NewSpeechModel(p, "tts").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"}); err == nil {
		t.Fatal("expected speech error")
	}
	if _, err := NewTranscriptionModel(p, "whisper").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"}); err == nil {
		t.Fatal("expected transcription error")
	}
}

type azureRoundTripper func(*http.Request) (*http.Response, error)

func (f azureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newAzureProviderWithTransport(t *testing.T, rt azureRoundTripper) *Provider {
	t.Helper()
	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: "https://azure.example", DeploymentID: "dep", APIVersion: "2024-10-21"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL:    "https://azure.example",
		Headers:    p.staticAuthHeaders(),
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}

func mustNewProvider(t *testing.T, cfg Config) *Provider {
	t.Helper()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New azure provider: %v", err)
	}
	return p
}

func azureJSONResponse(status int, body string, headers map[string]string) *http.Response {
	h := http.Header{"Content-Type": []string{"application/json"}}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func azureTextResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
