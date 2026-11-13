package assemblyai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provcTranscriptOutOfRangeNumber: a number beyond the double range
// under an undeclared key reads as Infinity (JSON.parse) and the completed
// transcript still converts (the raw map re-serializes it as null).
func TestR96provcTranscriptOutOfRangeNumber(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"upload_url":"https://cdn.example.com/a"}`)
	})
	mux.HandleFunc("/transcript", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","status":"queued"}`)
	})
	mux.HandleFunc("/transcript/tr1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","status":"completed","text":"hello","audio_duration":2,"extra":1e400}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "universal-3-5-pro")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil || res.Text != "hello" || res.Usage.DurationSeconds != 2 {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
}
