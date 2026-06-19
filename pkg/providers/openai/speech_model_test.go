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

	defaultBody, warnings := m.buildRequestBody(&provider.SpeechGenerateOptions{Text: "hello"})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if defaultBody["voice"] != "alloy" || defaultBody["model"] != "tts-1" || defaultBody["input"] != "hello" || defaultBody["response_format"] != "mp3" {
		t.Fatalf("default body mismatch: %#v", defaultBody)
	}
	if _, ok := defaultBody["speed"]; ok {
		t.Fatalf("speed should be omitted when nil: %#v", defaultBody)
	}

	speed := 1.25
	customBody, warnings := m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:         "hello",
		Voice:        "nova",
		OutputFormat: "wav",
		Speed:        &speed,
		Instructions: "speak warmly",
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if customBody["voice"] != "nova" || customBody["speed"] != 1.25 || customBody["response_format"] != "wav" || customBody["instructions"] != "speak warmly" {
		t.Fatalf("custom body mismatch: %#v", customBody)
	}

	warnBody, warnings := m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:         "hello",
		OutputFormat: "ogg",
		Language:     "es",
	})
	if warnBody["response_format"] != "mp3" {
		t.Fatalf("unsupported output format should keep mp3 fallback: %#v", warnBody)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if warnings[0].Type != "unsupported" || warnings[0].Feature != "outputFormat" || warnings[0].Details != "Unsupported output format: ogg. Using mp3 instead." {
		t.Fatalf("outputFormat warning mismatch: %#v", warnings[0])
	}
	if warnings[1].Type != "unsupported" || warnings[1].Feature != "language" || warnings[1].Details != `OpenAI speech models do not support language selection. Language parameter "es" was ignored.` {
		t.Fatalf("language warning mismatch: %#v", warnings[1])
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
	if seenBody["response_format"] != "mp3" {
		t.Fatalf("expected default response_format in request body: %#v", seenBody)
	}
	if string(out.Audio) != "AUDIO" {
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
