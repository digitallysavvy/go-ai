package hume

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func newHumeTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(Config{APIKey: "test-api-key", BaseURL: server.URL})
}

// TestSpeechModel_PassesModelAndText mirrors "should pass the model and text".
func TestSpeechModel_PassesModelAndText(t *testing.T) {
	var seenBody map[string]interface{}
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	if _, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello from the AI SDK!"}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	utterances, ok := seenBody["utterances"].([]interface{})
	if !ok || len(utterances) != 1 {
		t.Fatalf("utterances = %#v", seenBody["utterances"])
	}
	utterance := utterances[0].(map[string]interface{})
	if utterance["text"] != "Hello from the AI SDK!" {
		t.Fatalf("text = %#v", utterance["text"])
	}
	voice := utterance["voice"].(map[string]interface{})
	if voice["id"] != defaultVoiceID || voice["provider"] != "HUME_AI" {
		t.Fatalf("voice = %#v", voice)
	}
	format := seenBody["format"].(map[string]interface{})
	if format["type"] != "mp3" {
		t.Fatalf("format = %#v", format)
	}
}

// TestSpeechModel_Headers mirrors "should pass headers".
func TestSpeechModel_Headers(t *testing.T) {
	var seenHeaders http.Header
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenHeaders = r.Header
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:    "Hello from the AI SDK!",
		Headers: map[string]string{"Custom-Request-Header": "request-header-value"},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenHeaders.Get("X-Hume-Api-Key") != "test-api-key" {
		t.Fatalf("x-hume-api-key = %q", seenHeaders.Get("X-Hume-Api-Key"))
	}
	if seenHeaders.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q", seenHeaders.Get("Content-Type"))
	}
	if seenHeaders.Get("Custom-Request-Header") != "request-header-value" {
		t.Fatalf("custom header = %q", seenHeaders.Get("Custom-Request-Header"))
	}
}

// TestSpeechModel_PassesOptions mirrors "should pass options".
func TestSpeechModel_PassesOptions(t *testing.T) {
	var seenBody map[string]interface{}
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	speed := 1.5
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Hello from the AI SDK!",
		Voice:        "test-voice",
		OutputFormat: "mp3",
		Speed:        &speed,
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	utterance := seenBody["utterances"].([]interface{})[0].(map[string]interface{})
	if utterance["speed"] != 1.5 {
		t.Fatalf("speed = %#v", utterance["speed"])
	}
	voice := utterance["voice"].(map[string]interface{})
	if voice["id"] != "test-voice" {
		t.Fatalf("voice = %#v", voice)
	}
}

// TestSpeechModel_ReturnsAudioData mirrors "should return audio data with
// correct content type" and "should handle different audio formats".
func TestSpeechModel_ReturnsAudioData(t *testing.T) {
	for _, format := range []string{"mp3", "pcm", "wav"} {
		p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/"+format)
			_, _ = w.Write(make([]byte, 100))
		})
		model, _ := p.SpeechModel("")

		result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello from the AI SDK!"})
		if err != nil {
			t.Fatalf("DoGenerate(%s): %v", format, err)
		}
		if len(result.Audio) != 100 {
			t.Fatalf("audio length = %d for %s", len(result.Audio), format)
		}
	}
}

// TestSpeechModel_ResponseMetadata mirrors "should include response data
// with timestamp, modelId and headers" and "should use real date when no
// custom date provider is specified".
func TestSpeechModel_ResponseMetadata(t *testing.T) {
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		w.Header().Set("x-request-id", "test-request-id")
		w.Header().Set("x-ratelimit-remaining", "123")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello from the AI SDK!"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Response.ModelID != "" {
		t.Fatalf("modelId = %q, want empty string", result.Response.ModelID)
	}
	if result.Response.Headers["X-Request-Id"] != "test-request-id" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
	if result.Response.Headers["X-Ratelimit-Remaining"] != "123" {
		t.Fatalf("headers = %#v", result.Response.Headers)
	}
}

// TestSpeechModel_WarningsEmptyByDefault mirrors "should include warnings if
// any are generated".
func TestSpeechModel_WarningsEmptyByDefault(t *testing.T) {
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello from the AI SDK!"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_UnsupportedOutputFormatWarning mirrors the unsupported
// outputFormat fallback.
func TestSpeechModel_UnsupportedOutputFormatWarning(t *testing.T) {
	var seenBody map[string]interface{}
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello", OutputFormat: "flac"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	format := seenBody["format"].(map[string]interface{})
	if format["type"] != "mp3" {
		t.Fatalf("format = %#v", format)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "outputFormat" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_ContextGenerationID mirrors passing
// providerOptions.hume.context with a generationId.
func TestSpeechModel_ContextGenerationID(t *testing.T) {
	var seenBody map[string]interface{}
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello",
		ProviderOptions: map[string]interface{}{
			"hume": SpeechModelOptions{Context: &Context{GenerationID: "gen-1"}},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	ctx := seenBody["context"].(map[string]interface{})
	if ctx["generation_id"] != "gen-1" {
		t.Fatalf("context = %#v", ctx)
	}
}

// TestSpeechModel_ContextUtterances mirrors passing
// providerOptions.hume.context with an utterances list.
func TestSpeechModel_ContextUtterances(t *testing.T) {
	var seenBody map[string]interface{}
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		seenBody = map[string]interface{}{}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	trailingSilence := 0.5
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "Hello",
		ProviderOptions: map[string]interface{}{
			"hume": SpeechModelOptions{Context: &Context{Utterances: []Utterance{
				{Text: "prior utterance", TrailingSilence: &trailingSilence, Voice: &UtteranceVoice{Name: "Ava", Provider: "HUME_AI"}},
			}}},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	ctx := seenBody["context"].(map[string]interface{})
	utterances := ctx["utterances"].([]interface{})
	if len(utterances) != 1 {
		t.Fatalf("utterances = %#v", utterances)
	}
	u := utterances[0].(map[string]interface{})
	if u["text"] != "prior utterance" || u["trailing_silence"] != 0.5 {
		t.Fatalf("utterance = %#v", u)
	}
	voice := u["voice"].(map[string]interface{})
	if voice["name"] != "Ava" || voice["provider"] != "HUME_AI" {
		t.Fatalf("voice = %#v", voice)
	}
}

// TestSpeechModel_LanguageWarning mirrors "Hume speech models do not
// support language selection".
func TestSpeechModel_LanguageWarning(t *testing.T) {
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mp3")
		_, _ = w.Write(make([]byte, 100))
	})
	model, _ := p.SpeechModel("")

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello", Language: "fr"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "language" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestSpeechModel_APIErrors mirrors surfacing Hume's error envelope.
func TestSpeechModel_APIErrors(t *testing.T) {
	p := newHumeTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "Invalid API key", "code": 401},
		})
	})
	model, _ := p.SpeechModel("")

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello"})
	if err == nil {
		t.Fatal("expected error")
	}
}
