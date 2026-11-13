package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `segments?.map(...) ?? words?.map(...)`: an empty segments array wins
// over words, only null/absent segments fall back to words.
func TestGroqTranscriptionEmptySegmentsWinOverWords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"Hello","segments":[],"words":[{"word":"Hello","start":0,"end":1}],"x_groq":{"id":"r"}}`))
	}))
	defer server.Close()
	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Segments) != 0 {
		t.Fatalf("Segments = %+v, want none", result.Segments)
	}
}
