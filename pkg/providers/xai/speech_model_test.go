package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestSpeechModel_Metadata(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewSpeechModel(prov, "")

	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q, want v4", model.SpecificationVersion())
	}
	if model.Provider() != "xai.speech" {
		t.Fatalf("Provider() = %q, want xai.speech", model.Provider())
	}
}

// TestSpeechModel_DoGenerate verifies the basic request shape (default
// voice/language, output_format) and that raw audio bytes are returned as-is.
func TestSpeechModel_DoGenerate(t *testing.T) {
	var capturedBody map[string]interface{}
	audioBytes := []byte("fake-mp3-bytes")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tts" {
			t.Errorf("path = %q, want /tts", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(audioBytes)
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewSpeechModel(prov, "")

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "Hello world"})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if string(result.Audio) != string(audioBytes) {
		t.Fatalf("Audio = %q, want %q", result.Audio, audioBytes)
	}

	if capturedBody["text"] != "Hello world" {
		t.Errorf("text = %v, want Hello world", capturedBody["text"])
	}
	if capturedBody["voice_id"] != "eve" {
		t.Errorf("voice_id = %v, want eve", capturedBody["voice_id"])
	}
	if capturedBody["language"] != "auto" {
		t.Errorf("language = %v, want auto", capturedBody["language"])
	}
	outputFormat, ok := capturedBody["output_format"].(map[string]interface{})
	if !ok || outputFormat["codec"] != "mp3" {
		t.Errorf("output_format = %#v, want codec=mp3", capturedBody["output_format"])
	}
}

// TestSpeechModel_DoGenerate_VoiceAndSpeed verifies that a custom voice and
// speed are forwarded to the request.
func TestSpeechModel_DoGenerate_VoiceAndSpeed(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewSpeechModel(prov, "")

	speed := 1.25
	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "Hi",
		Voice: "ash",
		Speed: &speed,
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	if capturedBody["voice_id"] != "ash" {
		t.Errorf("voice_id = %v, want ash", capturedBody["voice_id"])
	}
	if capturedBody["speed"] != 1.25 {
		t.Errorf("speed = %v, want 1.25", capturedBody["speed"])
	}
}

// TestSpeechModel_DoGenerate_TimestampsEnvelope verifies that requesting
// providerOptions.xai.withTimestamps sends with_timestamps in the request and
// decodes the resulting JSON envelope: base64 audio is decoded to raw bytes,
// and duration/contentType/audioTimestamps land in ProviderMetadata.xai
// alongside the trace id (row 1ffa1d2).
func TestSpeechModel_DoGenerate_TimestampsEnvelope(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("x-trace-id", "trace-abc")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"audio": "aGVsbG8=",
			"content_type": "audio/mpeg",
			"duration": 1.5,
			"audio_timestamps": {
				"graph_chars": ["h", "i"],
				"graph_times": [[0, 0.1], [0.1, 0.2]]
			}
		}`))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewSpeechModel(prov, "")

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "hi",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"withTimestamps": true},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if string(result.Audio) != "hello" {
		t.Fatalf("Audio = %q, want hello", result.Audio)
	}
	if capturedBody["with_timestamps"] != true {
		t.Errorf("with_timestamps = %v, want true", capturedBody["with_timestamps"])
	}

	xaiMeta, ok := result.ProviderMetadata["xai"].(map[string]interface{})
	if !ok {
		t.Fatalf("ProviderMetadata = %#v, want xai metadata", result.ProviderMetadata)
	}
	if xaiMeta["traceId"] != "trace-abc" {
		t.Errorf("traceId = %v, want trace-abc", xaiMeta["traceId"])
	}
	if xaiMeta["duration"] != 1.5 {
		t.Errorf("duration = %v, want 1.5", xaiMeta["duration"])
	}
	if xaiMeta["contentType"] != "audio/mpeg" {
		t.Errorf("contentType = %v, want audio/mpeg", xaiMeta["contentType"])
	}
	if _, ok := xaiMeta["audioTimestamps"]; !ok {
		t.Errorf("audioTimestamps missing from metadata: %#v", xaiMeta)
	}
}

// TestSpeechModel_DoGenerate_Replace verifies that providerOptions.xai.replace
// is forwarded verbatim to the request body.
func TestSpeechModel_DoGenerate_Replace(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewSpeechModel(prov, "")

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text: "AI is great",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"replace": map[string]interface{}{"AI": "A eye"}},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	replace, ok := capturedBody["replace"].(map[string]interface{})
	if !ok || replace["AI"] != "A eye" {
		t.Fatalf("replace = %#v, want {AI: A eye}", capturedBody["replace"])
	}
}

// TestSpeechModel_DoGenerate_ErrorShape verifies the plain
// `{"error": "some string"}` xAI speech error shape is parsed into a
// meaningful error message that also includes the trace id.
func TestSpeechModel_DoGenerate_ErrorShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-trace-id", "trace-123")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"speed must be between 0.7 and 1.5"}`))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewSpeechModel(prov, "")

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "speed must be between 0.7 and 1.5") {
		t.Errorf("error = %q, want to contain the speed message", err.Error())
	}
	if !strings.Contains(err.Error(), "trace-123") {
		t.Errorf("error = %q, want to contain the trace id", err.Error())
	}
}

// TestSpeechModel_DoGenerate_StructuredErrorShape verifies the nested
// `{"error": {"message": ...}}` xAI error shape is parsed correctly too.
func TestSpeechModel_DoGenerate_StructuredErrorShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid voice_id","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewSpeechModel(prov, "")

	_, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Invalid voice_id") {
		t.Errorf("error = %q, want to contain Invalid voice_id", err.Error())
	}
}
