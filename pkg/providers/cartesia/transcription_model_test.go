package cartesia

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

var cartesiaTestAudio = []byte{0, 1, 2, 3, 4}

// cartesiaTranscriptionFixture mirrors
// cartesia/src/__fixtures__/cartesia-transcription.json.
var cartesiaTranscriptionFixture = map[string]interface{}{
	"text":     "Hello from the Vercel AI SDK.",
	"language": "en",
	"duration": 2.479,
	"words": []map[string]interface{}{
		{"word": "Hello", "start": 0.199, "end": 0.479},
		{"word": "from", "start": 0.5, "end": 0.639},
		{"word": "the", "start": 0.66, "end": 0.759},
		{"word": "Vercel", "start": 0.759, "end": 1.12},
		{"word": "AI", "start": 1.2, "end": 1.519},
		{"word": "SDK.", "start": 1.58, "end": 2.479},
	},
}

func newCartesiaJSONServer(t *testing.T, body interface{}, capture func(r *http.Request)) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return New(Config{APIKey: "test-api-key", BaseURL: server.URL})
}

// TestTranscriptionModel_PassesModel mirrors "should pass the model".
func TestTranscriptionModel_PassesModel(t *testing.T) {
	var seenModel string
	p := newCartesiaJSONServer(t, cartesiaTranscriptionFixture, func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		seenModel = r.FormValue("model")
	})
	model, _ := p.TranscriptionModel(ModelInkWhisper)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: cartesiaTestAudio, MimeType: "audio/wav"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenModel != "ink-whisper" {
		t.Fatalf("model = %q", seenModel)
	}
}

// TestTranscriptionModel_Headers mirrors "should pass headers".
func TestTranscriptionModel_Headers(t *testing.T) {
	var seenHeaders http.Header
	p := newCartesiaJSONServer(t, cartesiaTranscriptionFixture, func(r *http.Request) {
		seenHeaders = r.Header
	})
	model, _ := p.TranscriptionModel(ModelInkWhisper)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: cartesiaTestAudio, MimeType: "audio/wav",
		Headers: map[string]string{"Custom-Request-Header": "request-header-value"},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenHeaders.Get("Authorization") != "Bearer test-api-key" {
		t.Fatalf("authorization = %q", seenHeaders.Get("Authorization"))
	}
	if seenHeaders.Get("Custom-Request-Header") != "request-header-value" {
		t.Fatalf("custom header = %q", seenHeaders.Get("Custom-Request-Header"))
	}
}

// TestTranscriptionModel_ExtractsTranscription mirrors "should extract the
// transcription text" and "should extract segments, language and duration".
func TestTranscriptionModel_ExtractsTranscription(t *testing.T) {
	p := newCartesiaJSONServer(t, cartesiaTranscriptionFixture, nil)
	model, _ := p.TranscriptionModel(ModelInkWhisper)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: cartesiaTestAudio, MimeType: "audio/wav"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Text != "Hello from the Vercel AI SDK." {
		t.Fatalf("text = %q", result.Text)
	}
	if result.Language != "en" {
		t.Fatalf("language = %q", result.Language)
	}
	if result.DurationInSeconds == nil || *result.DurationInSeconds != 2.479 {
		t.Fatalf("duration = %#v", result.DurationInSeconds)
	}
	if len(result.Segments) == 0 || result.Segments[0].Text != "Hello" || result.Segments[0].Start != 0.199 || result.Segments[0].End != 0.479 {
		t.Fatalf("segments[0] = %#v", result.Segments[0])
	}
}

// TestTranscriptionModel_LanguageAndTimestampGranularities mirrors "should
// pass language and timestamp granularities".
func TestTranscriptionModel_LanguageAndTimestampGranularities(t *testing.T) {
	var seenLanguage, seenGranularity string
	p := newCartesiaJSONServer(t, cartesiaTranscriptionFixture, func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		seenLanguage = r.FormValue("language")
		seenGranularity = r.FormValue("timestamp_granularities[]")
	})
	model, _ := p.TranscriptionModel(ModelInkWhisper)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: cartesiaTestAudio, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"cartesia": TranscriptionModelOptions{Language: "en", TimestampGranularities: []string{"word"}},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if seenLanguage != "en" || seenGranularity != "word" {
		t.Fatalf("language=%q granularity=%q", seenLanguage, seenGranularity)
	}
}

// TestTranscriptionModel_StreamingOptionWarning mirrors the batch-mode
// warning for providerOptions.cartesia.streaming.
func TestTranscriptionModel_StreamingOptionWarning(t *testing.T) {
	p := newCartesiaJSONServer(t, cartesiaTranscriptionFixture, nil)
	model, _ := p.TranscriptionModel(ModelInkWhisper)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: cartesiaTestAudio, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"cartesia": TranscriptionModelOptions{Streaming: &StreamingOptions{TurnDetection: boolPtr(false)}},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.cartesia.streaming" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

// TestTranscriptionModel_ResponseMetadata mirrors "should include response
// data with timestamp and modelId".
func TestTranscriptionModel_ResponseMetadata(t *testing.T) {
	p := newCartesiaJSONServer(t, cartesiaTranscriptionFixture, nil)
	model, _ := p.TranscriptionModel(ModelInkWhisper)

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: cartesiaTestAudio, MimeType: "audio/wav"})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if result.Response.ModelID != ModelInkWhisper {
		t.Fatalf("modelId = %q", result.Response.ModelID)
	}
}

// TestTranscriptionModel_RejectsStreamingModel mirrors "rejects unsupported
// model operations" for ink-2 doGenerate.
func TestTranscriptionModel_RejectsStreamingModel(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model, _ := p.TranscriptionModel(ModelInk2)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{}, MimeType: "audio/wav"})
	if err == nil {
		t.Fatal("expected error for ink-2 non-streaming transcription")
	}
	if !errors.Is(err, providererrors.ErrUnsupportedFeature) {
		t.Fatalf("error = %v, want ErrUnsupportedFeature", err)
	}
}

// TestTranscriptionModel_APIErrors mirrors surfacing API errors via
// cartesiaFailedResponseHandler.
func TestTranscriptionModel_APIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error_code": "unauthorized",
			"title":      "Unauthorized",
			"message":    "Invalid API key",
			"request_id": "req-1",
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, _ := p.TranscriptionModel(ModelInkWhisper)

	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: cartesiaTestAudio, MimeType: "audio/wav"})
	if err == nil {
		t.Fatal("expected error")
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want *ProviderError", err)
	}
	if providerErr.Message != "Unauthorized: Invalid API key" {
		t.Fatalf("message = %q", providerErr.Message)
	}
}
