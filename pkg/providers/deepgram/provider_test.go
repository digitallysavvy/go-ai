package deepgram

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
		seenAuth  string
		seenQuery string
		seenCT    string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listen" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		seenCT = r.Header.Get("Content-Type")
		seenQuery = r.URL.RawQuery

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"metadata": map[string]interface{}{"duration": 5.25},
			"results": map[string]interface{}{
				"channels": []map[string]interface{}{
					{
						"alternatives": []map[string]interface{}{
							{
								"transcript": "hello deepgram",
								"words": []map[string]interface{}{
									{"word": "hello", "start": 0.0, "end": 0.7},
									{"word": "deepgram", "start": 0.8, "end": 1.5},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "dg-key", BaseURL: srv.URL})
	mAny, err := p.TranscriptionModel("")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	m := mAny.(*TranscriptionModel)
	if m.ModelID() != "nova-2" {
		t.Fatalf("default model id = %q, want nova-2", m.ModelID())
	}

	got, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:      []byte("bytes"),
		MimeType:   "audio/wav",
		Language:   "en",
		Timestamps: true,
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}

	if seenAuth != "Token dg-key" {
		t.Fatalf("Authorization header = %q", seenAuth)
	}
	if seenCT != "audio/wav" {
		t.Fatalf("Content-Type = %q", seenCT)
	}
	if !strings.Contains(seenQuery, "model=nova-2") || !strings.Contains(seenQuery, "language=en") {
		t.Fatalf("query missing model/language: %s", seenQuery)
	}
	if !strings.Contains(seenQuery, "punctuate=true") || !strings.Contains(seenQuery, "utterances=true") {
		t.Fatalf("query missing timestamp flags: %s", seenQuery)
	}
	if got.Text != "hello deepgram" {
		t.Fatalf("Text = %q", got.Text)
	}
	if len(got.Timestamps) != 2 {
		t.Fatalf("timestamps len = %d, want 2", len(got.Timestamps))
	}
	if got.Usage.DurationSeconds != 5.25 {
		t.Fatalf("duration = %v", got.Usage.DurationSeconds)
	}
}

func TestDoTranscribe_APIErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "dg", BaseURL: srv.URL})
	mAny, _ := p.TranscriptionModel("nova-2")
	_, err := mAny.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("x"),
		MimeType: "audio/mpeg",
	})
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("unexpected error: %v", err)
	}
}
