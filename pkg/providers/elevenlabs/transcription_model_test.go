package elevenlabs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func transcriptionTestServer(t *testing.T, capture *map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/speech-to-text" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if capture != nil {
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Fatalf("ParseMultipartForm: %v", err)
			}
			for k, v := range r.MultipartForm.Value {
				(*capture)[k] = v[0]
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"language_code":        "en",
			"language_probability": 0.99,
			"text":                 "Hello from the Go AI SDK.",
			"words": []map[string]interface{}{
				{"text": "Hello", "type": "word", "start": 0.0, "end": 0.5},
				{"text": "from", "type": "word", "start": 0.5, "end": 1.0},
			},
		})
	}))
}

func TestElevenLabsTranscription_RejectsRealtimeModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m, _ := p.TranscriptionModel(ModelScribeV2Realtime)
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported feature") {
		t.Fatalf("expected unsupported feature error, got %v", err)
	}
}

func TestElevenLabsTranscription_PassesModel(t *testing.T) {
	capture := map[string]string{}
	srv := transcriptionTestServer(t, &capture)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio-bytes"),
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if capture["model_id"] != "scribe_v1" {
		t.Fatalf("model_id = %q", capture["model_id"])
	}
	// No providerOptions: default diarize should be "true".
	if capture["diarize"] != "true" {
		t.Fatalf("diarize = %q", capture["diarize"])
	}
	if result.Text != "Hello from the Go AI SDK." {
		t.Fatalf("Text = %q", result.Text)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("segments = %#v", result.Segments)
	}
}

func TestElevenLabsTranscription_ProviderOptions(t *testing.T) {
	capture := map[string]string{}
	srv := transcriptionTestServer(t, &capture)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio-bytes"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"languageCode":          "en",
				"fileFormat":            "pcm_s16le_16",
				"tagAudioEvents":        false,
				"numSpeakers":           2,
				"timestampsGranularity": "character",
				"diarize":               true,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	want := map[string]string{
		"diarize":                "true",
		"file_format":            "pcm_s16le_16",
		"language_code":          "en",
		"model_id":               "scribe_v1",
		"num_speakers":           "2",
		"tag_audio_events":       "false",
		"timestamps_granularity": "character",
	}
	for k, v := range want {
		if capture[k] != v {
			t.Fatalf("field %s = %q, want %q (all: %#v)", k, capture[k], v, capture)
		}
	}
}

func TestElevenLabsTranscription_WarnsOnStreamingOptions(t *testing.T) {
	srv := transcriptionTestServer(t, nil)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("audio-bytes"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"elevenlabs": map[string]interface{}{
				"streaming": map[string]interface{}{"includeTimestamps": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.elevenlabs.streaming" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestElevenLabsTranscription_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "bad audio", "code": 400},
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.TranscriptionModel(ModelScribeV1)

	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("a"),
		MimeType: "audio/wav",
	})
	if err == nil || !strings.Contains(err.Error(), "bad audio") {
		t.Fatalf("unexpected error: %v", err)
	}
}
