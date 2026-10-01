package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGatewaySpeechModelDoGenerateWireFormat(t *testing.T) {
	var seenBody map[string]interface{}
	var seenModelID string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenModelID = r.Header.Get("ai-model-id")
		if r.Header.Get("ai-speech-model-specification-version") != "4" {
			t.Fatalf("speech spec header = %q", r.Header.Get("ai-speech-model-specification-version"))
		}
		if r.URL.Path != "/v4/ai/speech-model" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"audio": base64.StdEncoding.EncodeToString([]byte("audio")),
			"warnings": []map[string]interface{}{
				{"type": "other", "message": "w"},
			},
			"providerMetadata": map[string]interface{}{"gateway": map[string]interface{}{"id": "g1"}},
		})
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	speed := 1.25
	result, err := NewSpeechModel(p, "openai/tts").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "hello",
		Voice:           "alloy",
		OutputFormat:    "wav",
		Instructions:    "warm",
		Speed:           &speed,
		Language:        "en",
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"style": "narration"}},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenModelID != "openai/tts" || seenBody["text"] != "hello" || seenBody["outputFormat"] != "wav" || seenBody["instructions"] != "warm" {
		t.Fatalf("request mismatch model=%q body=%#v", seenModelID, seenBody)
	}
	if string(result.Audio) != "audio" || len(result.Warnings) != 1 {
		t.Fatalf("result mismatch: %#v", result)
	}
}

func TestGatewayTranscriptionModelDoTranscribeWireFormat(t *testing.T) {
	var seenBody map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ai-transcription-model-specification-version") != "4" {
			t.Fatalf("transcription spec header = %q", r.Header.Get("ai-transcription-model-specification-version"))
		}
		if r.Header.Get("ai-model-id") != "openai/whisper" {
			t.Fatalf("ai-model-id = %q", r.Header.Get("ai-model-id"))
		}
		if r.URL.Path != "/v4/ai/transcription-model" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("X-Transcription", "ok")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"text": "hello",
			"segments": []map[string]interface{}{
				{"text": "hello", "startSecond": 0, "endSecond": 1.5},
			},
			"language":          "en",
			"durationInSeconds": 1.5,
		})
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	result, err := NewTranscriptionModel(p, "openai/whisper").DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:           []byte("abc"),
		MimeType:        "audio/wav",
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"temperature": 0}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if seenBody["audio"] != base64.StdEncoding.EncodeToString([]byte("abc")) || seenBody["mediaType"] != "audio/wav" {
		t.Fatalf("request body = %#v", seenBody)
	}
	if result.Text != "hello" || result.Language != "en" || result.DurationInSeconds == nil || *result.DurationInSeconds != 1.5 || len(result.Segments) != 1 {
		t.Fatalf("result mismatch: %#v", result)
	}
	if result.Response == nil || result.Response.ModelID != "openai/whisper" || result.Response.Headers["X-Transcription"] != "ok" || result.Response.Body == nil {
		t.Fatalf("response metadata mismatch: %#v", result.Response)
	}
	if result.Warnings == nil || len(result.Warnings) != 0 {
		t.Fatalf("warnings should be an explicit empty slice to match TS, got %#v", result.Warnings)
	}
}
