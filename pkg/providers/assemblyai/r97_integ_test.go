package assemblyai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97integPollKeepsPrototypeKey: TS waitForCompletion reads the polled
// transcript with `await response.json()` and then schema.parse
// (assemblyai-transcription-model.ts:318), so a __proto__ key passes; the
// MediaSchemaParser check and the raw decode both keep it.
func TestR97integPollKeepsPrototypeKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"upload_url":"https://cdn.example.com/a"}`)
	})
	mux.HandleFunc("/transcript", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","status":"queued"}`)
	})
	mux.HandleFunc("/transcript/tr1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","status":"completed","text":"hello","__proto__":{"a":1},"constructor":{"prototype":{}}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "universal-3-5-pro")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello" {
		t.Fatalf("text = %q", res.Text)
	}
}
