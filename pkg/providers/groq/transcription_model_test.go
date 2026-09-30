package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported (behaviorally) from ai/packages/groq/src/groq-transcription-model.test.ts

func TestGroqTranscription_ExtractsText(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"Hello world!","x_groq":{"id":"req_1"}}`))
	}))
	defer server.Close()

	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3-turbo")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("fake-audio"),
		MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Text != "Hello world!" {
		t.Fatalf("Text = %q", result.Text)
	}
	if gotPath != "/audio/transcriptions" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestGroqTranscription_PlainTextResponse(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		gotBody = r.FormValue("response_format")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("This is a plain text transcription."))
	}))
	defer server.Close()

	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3-turbo")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("fake-audio"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"groq": map[string]interface{}{"responseFormat": "text"},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Text != "This is a plain text transcription." {
		t.Fatalf("Text = %q", result.Text)
	}
	if gotBody != "text" {
		t.Fatalf("response_format sent = %q, want text", gotBody)
	}
}

func TestGroqTranscription_FallsBackToWordsWhenNoSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"task":"transcribe","language":"English","duration":2,"text":"Hello world",
			"segments":null,
			"words":[{"word":"Hello","start":0,"end":1},{"word":"world","start":1,"end":2}],
			"x_groq":{"id":"req_1"}
		}`))
	}))
	defer server.Close()

	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    []byte("fake-audio"),
		MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{
			"groq": map[string]interface{}{
				"language":               "en",
				"responseFormat":         "verbose_json",
				"timestampGranularities": []interface{}{"word"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("Segments = %+v, want 2 word-derived segments", result.Segments)
	}
	if result.Segments[0].Text != "Hello" || result.Segments[0].Start != 0 || result.Segments[0].End != 1 {
		t.Fatalf("Segments[0] = %+v", result.Segments[0])
	}
	if result.Language != "English" {
		t.Fatalf("Language = %q", result.Language)
	}
	if result.DurationInSeconds == nil || *result.DurationInSeconds != 2 {
		t.Fatalf("DurationInSeconds = %v", result.DurationInSeconds)
	}
}

func TestGroqTranscription_PrefersSegmentsOverWords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"text":"Hello world",
			"segments":[{"text":"Hello world","start":0,"end":2,"id":0,"seek":0,"tokens":[],"temperature":0,"avg_logprob":0,"compression_ratio":0,"no_speech_prob":0}],
			"words":[{"word":"Hello","start":0,"end":1}],
			"x_groq":{"id":"req_1"}
		}`))
	}))
	defer server.Close()

	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("fake-audio"), MimeType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if len(result.Segments) != 1 || result.Segments[0].Text != "Hello world" {
		t.Fatalf("Segments = %+v", result.Segments)
	}
}
