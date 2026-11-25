package assemblyai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS assemblyai-transcription-model.ts doGenerate takes currentDate before
// the upload; Go took it after the submit.
func TestR89provcTranscriptionTimestampBeforeUpload(t *testing.T) {
	var mu sync.Mutex
	var uploaded time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/upload"):
			uploaded = time.Now()
			time.Sleep(10 * time.Millisecond)
			_, _ = w.Write([]byte(`{"upload_url":"https://cdn.assemblyai.com/u"}`))
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"t1","status":"queued"}`))
		default:
			_, _ = w.Write([]byte(`{"id":"t1","status":"completed","text":"hi","words":[],"audio_duration":1}`))
		}
	}))
	defer server.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "best")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("x"), MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Response.Timestamp.Before(uploaded) {
		t.Fatalf("timestamp %v not before the upload %v", res.Response.Timestamp, uploaded)
	}
}
