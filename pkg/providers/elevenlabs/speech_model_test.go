package elevenlabs

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
)

func TestElevenLabsSpeechModelMetadataAndGenerate(t *testing.T) {
	var seenPath string
	var seenQuery string
	var seenBody map[string]interface{}

	p := newElevenLabsProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		seenQuery = r.URL.Query().Get("output_format")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("AUDIO")),
		}, nil
	})
	m := NewSpeechModel(p, "eleven_multilingual_v2")
	if m.SpecificationVersion() != "v4" || m.Provider() != "elevenlabs" || m.ModelID() != "eleven_multilingual_v2" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}

	out, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v1/text-to-speech/21m00Tcm4TlvDq8ikWAM" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenQuery != "mp3_44100_128" {
		t.Fatalf("output_format = %q", seenQuery)
	}
	if seenBody["model_id"] != "eleven_multilingual_v2" || seenBody["text"] != "hello" {
		t.Fatalf("body mismatch: %#v", seenBody)
	}
	if _, ok := seenBody["voice_settings"]; ok {
		t.Fatalf("voice_settings should be omitted by default: %#v", seenBody)
	}
	if string(out.Audio) != "AUDIO" {
		t.Fatalf("result mismatch: %#v", out)
	}
}

func TestElevenLabsSpeechModelRequestArgsParity(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewSpeechModel(p, "eleven_multilingual_v2")

	body, query, warnings, voice := m.buildRequestArgs(&provider.SpeechGenerateOptions{Text: "hello"})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if voice != "21m00Tcm4TlvDq8ikWAM" {
		t.Fatalf("voice = %q", voice)
	}
	if query["output_format"] != "mp3_44100_128" {
		t.Fatalf("query = %#v", query)
	}
	if body["text"] != "hello" || body["model_id"] != "eleven_multilingual_v2" {
		t.Fatalf("body mismatch: %#v", body)
	}
	if _, ok := body["voice_settings"]; ok {
		t.Fatalf("voice_settings should be omitted by default: %#v", body)
	}

	speed := 1.2
	body, query, warnings, voice = m.buildRequestArgs(&provider.SpeechGenerateOptions{
		Text:         "hola",
		Voice:        "voice-1",
		OutputFormat: "pcm",
		Speed:        &speed,
		Language:     "es",
		Instructions: "speak warmly",
	})
	if voice != "voice-1" {
		t.Fatalf("voice = %q", voice)
	}
	if query["output_format"] != "pcm_44100" {
		t.Fatalf("query = %#v", query)
	}
	if body["language_code"] != "es" {
		t.Fatalf("language_code = %#v", body["language_code"])
	}
	voiceSettings, ok := body["voice_settings"].(map[string]interface{})
	if !ok || voiceSettings["speed"] != 1.2 {
		t.Fatalf("voice_settings mismatch: %#v", body["voice_settings"])
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "instructions" || warnings[0].Details != "ElevenLabs speech models do not support instructions. Instructions parameter was ignored." {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestElevenLabsSpeechModelProviderOptionsParity(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewSpeechModel(p, "eleven_multilingual_v2")

	stability := 0.4
	similarity := 0.6
	style := 0.8
	boost := true
	seed := 99.0
	normalizeLanguage := false
	enableLogging := true

	body, query, warnings, _ := m.buildRequestArgs(&provider.SpeechGenerateOptions{
		Text:     "hello",
		Language: "fr",
		ProviderOptions: map[string]interface{}{
			"elevenlabs": SpeechModelOptions{
				LanguageCode: "de",
				VoiceSettings: &SpeechVoiceSettings{
					Stability:       &stability,
					SimilarityBoost: &similarity,
					Style:           &style,
					UseSpeakerBoost: &boost,
				},
				PronunciationDictionaryLocators: []PronunciationDictionaryLocator{{
					PronunciationDictionaryID: "dict-1",
					VersionID:                 "v1",
				}},
				Seed:                           &seed,
				PreviousText:                   "before",
				NextText:                       "after",
				PreviousRequestIDs:             []string{"prev"},
				NextRequestIDs:                 []string{"next"},
				ApplyTextNormalization:         "on",
				ApplyLanguageTextNormalization: &normalizeLanguage,
				EnableLogging:                  &enableLogging,
			},
		},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if body["language_code"] != "fr" {
		t.Fatalf("top-level language should win, body = %#v", body)
	}
	voiceSettings := body["voice_settings"].(map[string]interface{})
	if voiceSettings["stability"] != 0.4 || voiceSettings["similarity_boost"] != 0.6 || voiceSettings["style"] != 0.8 || voiceSettings["use_speaker_boost"] != true {
		t.Fatalf("voice_settings mismatch: %#v", voiceSettings)
	}
	if body["seed"] != 99.0 || body["previous_text"] != "before" || body["next_text"] != "after" || body["apply_text_normalization"] != "on" || body["apply_language_text_normalization"] != false {
		t.Fatalf("body mismatch: %#v", body)
	}
	if query["enable_logging"] != "true" {
		t.Fatalf("query = %#v", query)
	}
	locators := body["pronunciation_dictionary_locators"].([]map[string]interface{})
	if len(locators) != 1 || locators[0]["pronunciation_dictionary_id"] != "dict-1" || locators[0]["version_id"] != "v1" {
		t.Fatalf("locators mismatch: %#v", locators)
	}
}

func TestElevenLabsSpeechModelErrorPaths(t *testing.T) {
	pErr := newElevenLabsProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewSpeechModel(pErr, "m").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"}); err == nil {
		t.Fatal("expected transport error")
	}

	pStatus := newElevenLabsProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"error":"unauthorized"}`)),
		}, nil
	})
	if _, err := NewSpeechModel(pStatus, "m").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"}); err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected status error, got %v", err)
	}
}

type elevenLabsRoundTripper func(*http.Request) (*http.Response, error)

func (f elevenLabsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newElevenLabsProviderWithTransport(t *testing.T, rt elevenLabsRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://elevenlabs.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://elevenlabs.example",
		Headers: map[string]string{
			"xi-api-key":   "k",
			"Content-Type": "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
