package deepgram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS deepgram-transcription-model.ts doGenerate takes currentDate before the
// request; Go took it after the response.
func TestR89provcTranscriptionTimestampBeforeRequest(t *testing.T) {
	var handled time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = time.Now()
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`{"metadata":{"duration":1},"results":{"channels":[{"alternatives":[{"transcript":"hi","words":[]}]}]}}`))
	}))
	defer server.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "nova-3")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("x"), MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Response.Timestamp.Before(handled) {
		t.Fatalf("timestamp %v not before the request %v", res.Response.Timestamp, handled)
	}
}
