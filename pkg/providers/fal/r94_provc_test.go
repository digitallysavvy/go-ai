package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getFromApi checks response.ok: an unfollowed 3xx on the transcription
// result poll fails with the fal API error.
func TestR94provcTranscriptionPollNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
			return
		}
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"text":"hello"}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k"})
	p.queueHost = strings.TrimRight(srv.URL, "/")
	tm, err := p.TranscriptionModel("whisper")
	if err != nil {
		t.Fatal(err)
	}
	res, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err == nil {
		t.Fatalf("result = %+v, want the 300 API error", res)
	}
}
