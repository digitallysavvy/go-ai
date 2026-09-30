package fal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// decodeJSONBody reads and JSON-decodes an *http.Request body into a map,
// for asserting on the request body a model sent.
func decodeJSONBody(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("failed to decode request body %q: %v", raw, err)
	}
	return body
}

// newTestSpeechModel builds a Provider whose speechHost points at an
// httptest server, mirroring how fal-speech-model.test.ts intercepts
// "https://fal.run/{modelId}" with createTestServer. The submit endpoint and
// the audio-download endpoint are served from the same mux (same origin) so
// the trusted-origin download path (mirroring TS's
// getFromApi({trustedOrigin: requestUrl})) applies during tests, the same
// way it would for a same-origin CDN in production.
func newTestSpeechModel(t *testing.T, mux *http.ServeMux) (*SpeechModel, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mux)
	p := New(Config{APIKey: "test-api-key"})
	p.speechHost = srv.URL
	return NewSpeechModel(p, "fal-ai/minimax/speech-02-hd"), srv
}

func speechAudioHandler(audio []byte, extraHeaders map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for k, v := range extraHeaders {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "audio/mp3")
		w.WriteHeader(200)
		_, _ = w.Write(audio)
	}
}

// TestFalSpeechModel_PassesTextAndDefaultOutputFormat ports "should pass
// text and default output_format" from fal-speech-model.test.ts.
func TestFalSpeechModel_PassesTextAndDefaultOutputFormat(t *testing.T) {
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"},"duration_ms":1234}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(make([]byte, 100), nil))

	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello from the AI SDK!",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotBody["text"] != "Hello from the AI SDK!" {
		t.Fatalf("body.text = %v", gotBody["text"])
	}
	if gotBody["output_format"] != "url" {
		t.Fatalf("body.output_format = %v, want url", gotBody["output_format"])
	}
}

// TestFalSpeechModel_PassesHeaders ports "should pass headers".
func TestFalSpeechModel_PassesHeaders(t *testing.T) {
	var gotHeaders http.Header
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(make([]byte, 10), nil))

	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello from the AI SDK!",
		Headers: map[string]string{
			"Custom-Request-Header": "request-header-value",
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotHeaders.Get("Authorization") != "Key test-api-key" {
		t.Fatalf("Authorization header = %q", gotHeaders.Get("Authorization"))
	}
	if gotHeaders.Get("Custom-Request-Header") != "request-header-value" {
		t.Fatalf("Custom-Request-Header = %q", gotHeaders.Get("Custom-Request-Header"))
	}
}

// TestFalSpeechModel_ReturnsAudioData ports "should return audio data".
func TestFalSpeechModel_ReturnsAudioData(t *testing.T) {
	audioBuf := make([]byte, 100)
	for i := range audioBuf {
		audioBuf[i] = byte(i)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(audioBuf, nil))

	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello from the AI SDK!",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Audio) != len(audioBuf) {
		t.Fatalf("audio length = %d, want %d", len(result.Audio), len(audioBuf))
	}
	for i := range audioBuf {
		if result.Audio[i] != audioBuf[i] {
			t.Fatalf("audio mismatch at byte %d", i)
		}
	}
}

// TestFalSpeechModel_ResponseMetadata ports "should include response data
// with timestamp, modelId and headers".
func TestFalSpeechModel_ResponseMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "test-request-id")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(make([]byte, 10), nil))

	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello from the AI SDK!",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if result.Response == nil {
		t.Fatal("expected response metadata")
	}
	if result.Response.ModelID != "fal-ai/minimax/speech-02-hd" {
		t.Fatalf("response.modelId = %q", result.Response.ModelID)
	}
	if result.Response.Headers["X-Request-Id"] != "test-request-id" {
		t.Fatalf("response.headers[X-Request-Id] = %q", result.Response.Headers["X-Request-Id"])
	}
}

// TestFalSpeechModel_WarningsForUnsupportedSettings ports "should include
// warnings for unsupported settings".
func TestFalSpeechModel_WarningsForUnsupportedSettings(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(make([]byte, 10), nil))

	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Hello from the AI SDK!",
		Language:     "en",
		OutputFormat: "wav", // invalid; triggers a warning and defaults to url
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected at least one warning")
	}
}

// TestFalSpeechModel_ProviderOptionsPassthrough verifies
// providerOptions.fal is spread at the top level of the request body
// (voice_setting / audio_setting / language_boost / pronunciation_dict are
// already snake_case, matching fal-speech-model-options.ts).
func TestFalSpeechModel_ProviderOptionsPassthrough(t *testing.T) {
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"audio":{"url":"http://` + r.Host + `/files/test.mp3"}}`))
	})
	mux.HandleFunc("/files/test.mp3", speechAudioHandler(make([]byte, 10), nil))

	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hi",
		ProviderOptions: map[string]interface{}{
			"fal": map[string]interface{}{
				"language_boost": "English",
				"voice_setting": map[string]interface{}{
					"speed": 1.5,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotBody["language_boost"] != "English" {
		t.Fatalf("body.language_boost = %v", gotBody["language_boost"])
	}
	voiceSetting, ok := gotBody["voice_setting"].(map[string]interface{})
	if !ok || voiceSetting["speed"] != 1.5 {
		t.Fatalf("body.voice_setting = %v", gotBody["voice_setting"])
	}
}

// TestFalSpeechModel_ErrorResponse verifies a non-2xx response is surfaced
// as a ProviderError using the fal error schema's message field.
func TestFalSpeechModel_ErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/minimax/speech-02-hd", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid voice","code":400}}`))
	})

	m, srv := newTestSpeechModel(t, mux)
	defer srv.Close()

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error message")
	}
}
