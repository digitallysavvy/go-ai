package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS #23679: the queue request_id is one encoded path segment.
func TestFalTranscriptionModel_EncodesRequestIDAsSinglePathSegment(t *testing.T) {
	const rawID = "abc/../../internal"
	var gotURI string
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"abc/../../internal"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hi","chunks":[{"timestamp":[0,1],"text":"hi"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p := New(Config{APIKey: "k"})
	p.queueHost = srv.URL
	m := NewTranscriptionModel(p, "wizper")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	want := "/fal-ai/wizper/requests/" + providerutils.EncodePathSegment(rawID)
	if gotURI != want {
		t.Fatalf("request URI = %q, want %q", gotURI, want)
	}
}
