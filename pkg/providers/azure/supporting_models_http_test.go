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
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenURI = r.URL.RequestURI()
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
	if len(one.Embedding) != 2 || one.Response.Headers["X-Req"][0] != "r1" {
		t.Fatalf("result mismatch: %#v", one)
	}

	many, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if len(many.Embeddings) != 2 || many.Responses[0].Headers["X-Req"][0] != "r1" {
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
	if seenBody["stream"] != false || seenBody["model"] != "dep" {
		t.Fatalf("stream flag mismatch: %#v", seenBody)
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
	if string(speech.Audio) != "AUDIO" || speech.MimeType != "audio/mpeg" {
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

func TestAzureUseDeploymentBasedURLs(t *testing.T) {
	var seenURI string
	p := New(Config{
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
	p := New(Config{APIKey: "k", BaseURL: "https://azure.example", DeploymentID: "dep", APIVersion: "2024-10-21"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL:    "https://azure.example",
		Headers:    map[string]string{"api-key": "k"},
		HTTPClient: &http.Client{Transport: rt},
	})
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
