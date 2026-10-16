package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TestSpeechModelProviderOptionsSpeedInstructionsOverrideTopLevel covers
// e13c32f: providerOptions.openai.speed/instructions win over the top-level
// SpeechGenerateOptions fields when both are set, and providerOptions alone
// (no top-level default) still forwards speed/instructions.
func TestSpeechModelProviderOptionsSpeedInstructionsOverrideTopLevel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewSpeechModel(p, "tts-1")

	topSpeed := 1.0
	body, warnings := m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:         "hello",
		Speed:        &topSpeed,
		Instructions: "top-level",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"speed":        1.75,
				"instructions": "provider-option wins",
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if body["speed"] != 1.75 {
		t.Fatalf("speed = %#v, want provider option override 1.75", body["speed"])
	}
	if body["instructions"] != "provider-option wins" {
		t.Fatalf("instructions = %#v, want provider option override", body["instructions"])
	}

	// providerOptions alone, no top-level fields set.
	body, warnings = m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text: "hello",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"speed":        0.5,
				"instructions": "only provider option",
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if body["speed"] != 0.5 || body["instructions"] != "only provider option" {
		t.Fatalf("body = %#v", body)
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
	if err == nil || !strings.Contains(err.Error(), "provider error (400)") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestSpeechProviderOptionsValidatedLikeTS(t *testing.T) {
	for name, opts := range map[string]map[string]interface{}{
		"speed too small":     {"speed": 0.1},
		"speed too big":       {"speed": 5.0},
		"speed not number":    {"speed": "fast"},
		"instructions number": {"instructions": 3.0},
	} {
		_, _, err := (&SpeechModel{modelID: "tts-1"}).buildRequestBodyChecked(&provider.SpeechGenerateOptions{
			Text: "hi", ProviderOptions: map[string]interface{}{"openai": opts},
		})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "providerOptions" || ia.Message != "invalid openai provider options" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	body, _, err := (&SpeechModel{modelID: "tts-1"}).buildRequestBodyChecked(&provider.SpeechGenerateOptions{
		Text: "hi", Instructions: "top", ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"speed": nil, "instructions": ""}},
	})
	if err != nil || body["instructions"] != "" {
		t.Fatalf("body=%v err=%v", body, err)
	}
}

// TS createBinaryResponseHandler / postToApi treat any 2xx as success.
func TestSpeechModelAccepts2xxNon200(t *testing.T) {
	serverURL, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("AUDIO"))
	}))
	defer closeServer()
	m := NewSpeechModel(New(Config{APIKey: "k", BaseURL: serverURL + "/v1"}), "tts-1")
	out, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("202 must be accepted: %v", err)
	}
	if string(out.Audio) != "AUDIO" {
		t.Fatalf("audio = %q", out.Audio)
	}
}
