package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestSpeechModelBuildRequestBodyDefaultsAndOptions(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewSpeechModel(p, "tts-1")

	defaultBody := m.buildRequestBody(&provider.SpeechGenerateOptions{Text: "hello"})
	if defaultBody["voice"] != "alloy" || defaultBody["model"] != "tts-1" || defaultBody["input"] != "hello" {
		t.Fatalf("default body mismatch: %#v", defaultBody)
	}
	if _, ok := defaultBody["speed"]; ok {
		t.Fatalf("speed should be omitted when nil: %#v", defaultBody)
	}

	speed := 1.25
	customBody := m.buildRequestBody(&provider.SpeechGenerateOptions{Text: "hello", Voice: "nova", Speed: &speed})
	if customBody["voice"] != "nova" || customBody["speed"] != 1.25 {
		t.Fatalf("custom body mismatch: %#v", customBody)
	}
}

func TestSpeechModelDoGenerateSuccessAndError(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	serverURL, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		if r.URL.Query().Get("fail") == "1" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("bad request"))
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("AUDIO"))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL + "/v1"})
	m := NewSpeechModel(p, "tts-1")

	out, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v1/audio/speech" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["voice"] != "alloy" {
		t.Fatalf("expected default voice in request body: %#v", seenBody)
	}
	if string(out.Audio) != "AUDIO" || out.MimeType != "audio/mpeg" || out.Usage.CharacterCount != 5 {
		t.Fatalf("speech result mismatch: %#v", out)
	}

	// Force error response via dedicated server endpoint.
	errServerURL, closeErr := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer closeErr()

	mErr := NewSpeechModel(New(Config{APIKey: "k", BaseURL: errServerURL + "/v1"}), "tts-1")
	_, err = mErr.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "returned status 400") {
		t.Fatalf("expected status error, got %v", err)
	}
}
