package fal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS waits between "still in progress" polls with delay(pollIntervalMs) (no
// signal), so an abort surfaces from the next getFromApi as the abort reason.
func TestR95provcTranscriptionPollAbortReturnsCause(t *testing.T) {
	reason := errors.New("r95provc abort reason")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
			return
		}
		// Abort after the poll response arrived, during the wait.
		time.AfterFunc(100*time.Millisecond, func() { cancel(reason) })
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"detail":"Request is still in progress"}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k"})
	p.queueHost = strings.TrimRight(srv.URL, "/")
	tm, err := p.TranscriptionModel("whisper")
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if !errors.Is(err, reason) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
}
