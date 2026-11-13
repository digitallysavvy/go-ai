package deepgram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provcBase64AudioSentAsGiven: TS postToApi sends options.audio as
// the request content, so a base64 string audio goes out as that string.
// Before, Go sent an empty body when only AudioBase64 was set.
func TestR96provcBase64AudioSentAsGiven(t *testing.T) {
	var mu sync.Mutex
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"duration":1},"results":{"channels":[{"alternatives":[{"transcript":"hi","words":[]}]}]}}`))
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "nova-3")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "QUJD", MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if sent != "QUJD" {
		t.Fatalf("sent %q, want the base64 string as given", sent)
	}
}
