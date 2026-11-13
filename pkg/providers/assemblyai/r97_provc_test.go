package assemblyai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR97provcPollIsRawFetch: TS waitForCompletion polls with the fetch
// function itself (this.config.fetch ?? globalThis.fetch), not getFromApi.
// A failed poll fetch therefore rejects as is, not as the retryable
// "Cannot connect to API" APICallError that handleFetchError builds (core
// would retry the whole upload and submit on that), and the request carries
// combineHeaders(config headers, call headers) only: no Content-Type and no
// runtime user-agent tag.
func TestR97provcPollIsRawFetch(t *testing.T) {
	var mu sync.Mutex
	var pollHeaders http.Header
	failPoll := true
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"upload_url":"https://cdn.example.com/a"}`)
	})
	mux.HandleFunc("/transcript", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"tr1","status":"queued"}`)
	})
	mux.HandleFunc("/transcript/tr1", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pollHeaders = r.Header.Clone()
		fail := failPoll
		mu.Unlock()
		if fail {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		_, _ = io.WriteString(w, `{"id":"tr1","status":"completed","text":"hello"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "universal-3-5-pro")
	opts := &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav", Headers: map[string]string{"X-Call": "1"}}

	_, err := m.DoTranscribe(context.Background(), opts)
	if err == nil {
		t.Fatal("want the poll's transport error")
	}
	if strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %v, want the fetch rejection as is", err)
	}
	var retryable interface{ IsRetryable() bool }
	if errors.As(err, &retryable) && retryable.IsRetryable() {
		t.Fatalf("err = %v is retryable; TS does not retry a raw fetch rejection", err)
	}

	mu.Lock()
	failPoll = false
	mu.Unlock()
	res, err := m.DoTranscribe(context.Background(), opts)
	if err != nil || res.Text != "hello" {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := pollHeaders.Get("Content-Type"); got != "" {
		t.Errorf("poll Content-Type = %q, want none", got)
	}
	if got := pollHeaders.Get("Authorization"); got != "k" {
		t.Errorf("poll Authorization = %q, want k", got)
	}
	if got := pollHeaders.Get("X-Call"); got != "1" {
		t.Errorf("poll X-Call = %q, want the call header", got)
	}
	ua := pollHeaders.Get("User-Agent")
	if !strings.Contains(ua, "ai-sdk-assemblyai/") || strings.Contains(ua, providerutils.RuntimeEnvironmentUserAgent()) {
		t.Errorf("poll User-Agent = %q, want the provider tag without the runtime tag", ua)
	}
}
