package assemblyai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestProviderUnsupportedModels(t *testing.T) {
	p := New(Config{APIKey: "k"})

	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("LanguageModel should return unsupported error")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel should return unsupported error")
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel should return unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel should return unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel should return unsupported error")
	}
}

func TestDoTranscribe_RequestShapeAndResponseMapping(t *testing.T) {
	t.Parallel()

	var (
		seenUploadAuth    string
		seenTranscriptRaw map[string]interface{}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			seenUploadAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"upload_url": "https://files.example/audio",
			})
			return
		case "/transcript":
			_ = json.NewDecoder(r.Body).Decode(&seenTranscriptRaw)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     "tx-1",
				"status": "queued",
			})
			return
		case "/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":             "tx-1",
				"status":         "completed",
				"text":           "hello world",
				"audio_duration": 2300,
				"words": []map[string]interface{}{
					{"text": "hello", "start": 0, "end": 1000},
					{"text": "world", "start": 1100, "end": 2300},
				},
			})
			return
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "asm-key", BaseURL: srv.URL})
	mAny, err := p.TranscriptionModel("slim")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	m := mAny.(*TranscriptionModel)
	if m.ModelID() != "slim" {
		t.Fatalf("ModelID = %q, want slim", m.ModelID())
	}

	got, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("abc"),
		Language: "en",
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}

	if seenUploadAuth != "asm-key" {
		t.Fatalf("Authorization header = %q, want asm-key", seenUploadAuth)
	}
	if seenTranscriptRaw["audio_url"] != "https://files.example/audio" {
		t.Fatalf("audio_url = %#v", seenTranscriptRaw["audio_url"])
	}
	if seenTranscriptRaw["language_code"] != "en" {
		t.Fatalf("language_code = %#v", seenTranscriptRaw["language_code"])
	}
	if seenTranscriptRaw["speech_model"] != "slim" {
		t.Fatalf("speech_model = %#v", seenTranscriptRaw["speech_model"])
	}
	if got.Text != "hello world" {
		t.Fatalf("Text = %q", got.Text)
	}
	if len(got.Timestamps) != 2 {
		t.Fatalf("timestamps len = %d, want 2", len(got.Timestamps))
	}
	if got.Usage.DurationSeconds != 2.3 {
		t.Fatalf("duration = %v, want 2.3", got.Usage.DurationSeconds)
	}
}

func TestDoTranscribe_PollError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"upload_url": "u"})
		case "/transcript":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-2", "status": "queued"})
		case "/transcript/tx-2":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "tx-2", "status": "error", "error": "bad audio"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	mAny, _ := p.TranscriptionModel("")
	_, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a")})
	if err == nil || !strings.Contains(err.Error(), "transcription failed: bad audio") {
		t.Fatalf("unexpected error: %v", err)
	}
}
