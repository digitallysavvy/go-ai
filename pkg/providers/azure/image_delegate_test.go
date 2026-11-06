package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// azure.image() wraps OpenAIImageModel in TS: decoded b64 images (all of
// them), usage, providerOptions.openai, api-version query, max images.
func TestImageModelDelegatesToOpenAIImage(t *testing.T) {
	var path, query string
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		b := base64.StdEncoding.EncodeToString([]byte("img1"))
		c := base64.StdEncoding.EncodeToString([]byte("img2"))
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"` + b + `"},{"b64_json":"` + c + `"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`))
	}))
	defer srv.Close()

	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: srv.URL + "/openai", APIVersion: "2025-01-01-preview", UseDeploymentBasedURLs: true})
	m := NewImageModel(p, "gpt-image-1")
	if m.MaxImagesPerCall() != 10 {
		t.Fatalf("MaxImagesPerCall = %d, want 10", m.MaxImagesPerCall())
	}
	if NewImageModel(p, "my-deployment").MaxImagesPerCall() != 1 {
		t.Fatal("unknown deployment should be 1")
	}
	n := 2
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "cat", N: &n,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"background": "transparent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/openai/deployments/gpt-image-1/images/generations" || query != "api-version=2025-01-01-preview" {
		t.Fatalf("path=%q query=%q", path, query)
	}
	if body["background"] != "transparent" {
		t.Fatalf("provider options not forwarded: %v", body)
	}
	if _, ok := body["response_format"]; ok {
		t.Fatalf("gpt-image-* must not send response_format: %v", body)
	}
	if len(res.Images) != 2 || string(res.Images[1]) != "img2" || res.Usage.TotalTokens != 7 {
		t.Fatalf("result = %+v", res)
	}
}

// azure.speech() / azure.transcription() wrap the OpenAI models in TS:
// providerOptions.openai, api-version query, call headers, media type and
// response metadata all come from them.
func TestSpeechAndTranscriptionDelegateToOpenAI(t *testing.T) {
	var speechBody map[string]interface{}
	var speechQuery, transQuery, transBody, transHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/audio/speech"):
			speechQuery = r.URL.RawQuery
			_ = json.Unmarshal(raw, &speechBody)
			_, _ = w.Write([]byte("AUDIO"))
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			transQuery, transBody, transHeader = r.URL.RawQuery, string(raw), r.Header.Get("X-Call")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"hola","language":"spanish","duration":1.5}`))
		}
	}))
	defer srv.Close()

	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: srv.URL + "/openai", APIVersion: "2025-01-01-preview", UseDeploymentBasedURLs: true})

	sm, err := p.SpeechModel("tts-1")
	if err != nil {
		t.Fatal(err)
	}
	speed := 1.0
	sres, err := sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "hi", Speed: &speed,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"speed": 2.0, "instructions": "calm"}},
	})
	if err != nil || string(sres.Audio) != "AUDIO" {
		t.Fatalf("speech: %v %+v", err, sres)
	}
	if speechBody["speed"] != 2.0 || speechBody["instructions"] != "calm" || speechQuery != "api-version=2025-01-01-preview" {
		t.Fatalf("speech body=%v query=%q", speechBody, speechQuery)
	}

	tm, err := p.TranscriptionModel("whisper-1")
	if err != nil {
		t.Fatal(err)
	}
	tres, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav", Headers: map[string]string{"X-Call": "1"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"language": "es"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tres.Language != "es" || transQuery != "api-version=2025-01-01-preview" || transHeader != "1" ||
		!strings.Contains(transBody, `filename="audio.wav"`) || !strings.Contains(transBody, "verbose_json") {
		t.Fatalf("lang=%q query=%q header=%q body=%s", tres.Language, transQuery, transHeader, transBody)
	}
}
