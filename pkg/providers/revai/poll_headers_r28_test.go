package revai

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getFromApi polls with the provider headers, which carry the
// ai-sdk-revai User-Agent suffix; direct callers may pass audio as base64
// (unpadded / base64url accepted like atob).
func TestRevaiPollCarriesProviderUserAgentAndBase64Audio(t *testing.T) {
	var pollUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/speechtotext/v1/jobs":
			if r.Method == http.MethodPost {
				f, _, err := r.FormFile("media")
				if err != nil {
					t.Error(err)
				} else {
					buf := make([]byte, 8)
					n, _ := f.Read(buf)
					if string(buf[:n]) != string([]byte{0xfb, 0xff, 0xfe, 0x01}) {
						t.Errorf("uploaded %v", buf[:n])
					}
				}
				_, _ = w.Write([]byte(`{"id":"j1","status":"in_progress"}`))
			}
		case "/speechtotext/v1/jobs/j1":
			pollUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"id":"j1","status":"transcribed"}`))
		case "/speechtotext/v1/jobs/j1/transcript":
			_, _ = w.Write([]byte(`{"monologues":[]}`))
		}
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL}).TranscriptionModel("machine")
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		AudioBase64: base64.RawURLEncoding.EncodeToString([]byte{0xfb, 0xff, 0xfe, 0x01}), MimeType: "audio/mpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pollUA, "ai-sdk-revai/") {
		t.Errorf("poll User-Agent = %q", pollUA)
	}
}
