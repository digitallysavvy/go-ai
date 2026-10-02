package deepgram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func speechTestServer(t *testing.T, headers map[string]string, capture *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = r.URL.RawQuery
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "audio/mp3")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 100))
	}))
}

func TestDeepgramSpeechModel_ComposesUpstreamModelID(t *testing.T) {
	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	if _, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hello",
		Voice: "thalia",
	}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.Contains(query, "model=aura-2-thalia-en") {
		t.Fatalf("query = %s", query)
	}
}

func TestDeepgramSpeechModel_ComposesWithLanguage(t *testing.T) {
	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	if _, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:     "hola",
		Voice:    "celeste",
		Language: "es",
	}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.Contains(query, "model=aura-2-celeste-es") {
		t.Fatalf("query = %s", query)
	}
}

func TestDeepgramSpeechModel_RequiresVoiceForFamilyID(t *testing.T) {
	srv := speechTestServer(t, nil, nil)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err == nil || !strings.Contains(err.Error(), "requires a `voice`") {
		t.Fatalf("expected voice-required error, got %v", err)
	}
}

func TestDeepgramSpeechModel_FullVoiceModelIDPassesThrough(t *testing.T) {
	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2-helena-en")

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hello",
		Voice: "different-voice",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.Contains(query, "model=aura-2-helena-en") {
		t.Fatalf("query = %s", query)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Feature == "voice" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected voice warning, got %#v", result.Warnings)
	}
}

func TestDeepgramSpeechModel_SpeedPassthrough(t *testing.T) {
	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")
	speed := 1.5

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hello",
		Voice: "helena",
		Speed: &speed,
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.Contains(query, "speed=1.5") {
		t.Fatalf("query = %s", query)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", result.Warnings)
	}
}

func TestDeepgramSpeechModel_OutputFormatMapping(t *testing.T) {
	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "hello",
		Voice:        "helena",
		OutputFormat: "wav",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.Contains(query, "container=wav") || !strings.Contains(query, "encoding=linear16") {
		t.Fatalf("query = %s", query)
	}
}

func TestDeepgramSpeechModel_ProviderOptions(t *testing.T) {
	var query string
	srv := speechTestServer(t, nil, &query)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hello",
		Voice: "helena",
		ProviderOptions: map[string]interface{}{
			"deepgram": map[string]interface{}{
				"encoding":       "mp3",
				"bitRate":        48000,
				"container":      "wav",
				"callback":       "https://example.com/callback",
				"callbackMethod": "POST",
				"mipOptOut":      true,
				"tag":            "test-tag",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.Contains(query, "encoding=mp3") {
		t.Fatalf("query = %s", query)
	}
	if strings.Contains(query, "container=wav") {
		t.Fatalf("mp3 should not have container: %s", query)
	}
	if !strings.Contains(query, "bit_rate=48000") {
		t.Fatalf("query = %s", query)
	}
	if !strings.Contains(query, "mip_opt_out=true") {
		t.Fatalf("query = %s", query)
	}
	if !strings.Contains(query, "tag=test-tag") {
		t.Fatalf("query = %s", query)
	}
}

func TestDeepgramSpeechModel_ProviderMetadata(t *testing.T) {
	srv := speechTestServer(t, map[string]string{
		"dg-model-name":             "aura-2-helena-en",
		"dg-model-uuid":             "uuid-1",
		"dg-additional-model-uuids": "a,b",
		"dg-char-count":             "69",
		"dg-breaks-applied":         "0",
		"dg-pronunciations-applied": "2",
		"dg-pronunciation-warnings": "1 unknown word",
		"dg-request-id":             "req-1",
	}, nil)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hello",
		Voice: "helena",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	meta, ok := result.ProviderMetadata["deepgram"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected deepgram metadata, got %#v", result.ProviderMetadata)
	}
	if meta["modelName"] != "aura-2-helena-en" || meta["modelUuid"] != "uuid-1" {
		t.Fatalf("meta = %#v", meta)
	}
	if meta["charCount"] != 69 || meta["breaksApplied"] != 0 || meta["pronunciationsApplied"] != 2 {
		t.Fatalf("meta = %#v", meta)
	}
	if meta["requestId"] != "req-1" {
		t.Fatalf("meta = %#v", meta)
	}
	uuids, ok := meta["additionalModelUuids"].([]string)
	if !ok || len(uuids) != 2 {
		t.Fatalf("additionalModelUuids = %#v", meta["additionalModelUuids"])
	}
	// Ported from TS deepgram-speech-model.test.ts "should include usage
	// info" (TS 8c659885c5 / #21427): Dg-Char-Count becomes
	// result.usage.characters.
	if want := map[string]interface{}{"characters": 69}; !reflect.DeepEqual(result.Usage, want) {
		t.Fatalf("result.Usage = %#v, want %#v", result.Usage, want)
	}
}

// TestDeepgramSpeechModel_UsageAbsentWithoutCharCountHeader mirrors TS
// "should return empty provider metadata when Deepgram headers are absent":
// result.usage must be nil/undefined, not a zero-valued usage object.
func TestDeepgramSpeechModel_UsageAbsentWithoutCharCountHeader(t *testing.T) {
	srv := speechTestServer(t, nil, nil)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hello",
		Voice: "helena",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Usage != nil {
		t.Fatalf("result.Usage = %#v, want nil", result.Usage)
	}
}

func TestDeepgramSpeechModel_InstructionsWarning(t *testing.T) {
	srv := speechTestServer(t, nil, nil)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "hello",
		Voice:        "helena",
		Instructions: "Speak slowly",
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Feature == "instructions" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected instructions warning, got %#v", result.Warnings)
	}
}

func TestDeepgramSpeechModel_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"err_code":"INVALID_QUERY_PARAMETER","err_msg":"Invalid model","request_id":"r1"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.SpeechModel("aura-2")

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello", Voice: "helena"})
	if err == nil || !strings.Contains(err.Error(), "Invalid model") {
		t.Fatalf("unexpected error: %v", err)
	}
}
