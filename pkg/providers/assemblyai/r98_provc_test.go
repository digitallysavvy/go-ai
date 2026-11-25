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

// TS upload headers are {'Content-Type': 'application/octet-stream',
// ...combineHeaders(config headers, call headers)}: a Content-Type in the
// provider headers replaces the octet-stream default.
func TestR98provcUploadContentTypeFromProviderHeaders(t *testing.T) {
	var mu sync.Mutex
	var uploadCT string
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		uploadCT = r.Header.Get("Content-Type")
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
	for _, tc := range []struct {
		headers map[string]string
		want    string
	}{
		{nil, "application/octet-stream"},
		{map[string]string{"content-type": "audio/wav"}, "audio/wav"},
	} {
		m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL, Headers: tc.headers}), "universal-3-5-pro")
		if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := uploadCT
		mu.Unlock()
		if got != tc.want {
			t.Errorf("headers %v: upload Content-Type = %q, want %q", tc.headers, got, tc.want)
		}
	}
}
