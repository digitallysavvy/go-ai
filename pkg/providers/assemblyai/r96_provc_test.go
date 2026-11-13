package assemblyai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provcUploadBase64AudioAsGiven: TS postToApi sends options.audio as
// the upload content, so a base64 string audio is uploaded as that string.
// Before, Go uploaded an empty body when only AudioBase64 was set.
func TestR96provcUploadBase64AudioAsGiven(t *testing.T) {
	var mu sync.Mutex
	var uploaded string
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		uploaded = string(b)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"upload_url":"https://cdn.example.com/a"}`)
	})
	mux.HandleFunc("/transcript", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","status":"queued"}`)
	})
	mux.HandleFunc("/transcript/tr1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","status":"completed","text":"hi"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "universal-3-5-pro")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "QUJD", MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if uploaded != "QUJD" {
		t.Fatalf("uploaded %q, want the base64 string as given", uploaded)
	}
}
