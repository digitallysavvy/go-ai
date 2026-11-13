package revai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS doGenerate waits with delay(pollingInterval) (no signal), so an abort
// surfaces from the next getFromApi as the abort reason.
func TestR95provcPollAbortReturnsCause(t *testing.T) {
	oldTimeout, oldInterval := pollTimeout, pollInterval
	pollTimeout, pollInterval = time.Minute, 2*time.Second
	t.Cleanup(func() { pollTimeout, pollInterval = oldTimeout, oldInterval })
	reason := errors.New("r95provc abort reason")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Abort after the poll response arrived, during the wait.
			time.AfterFunc(100*time.Millisecond, func() { cancel(reason) })
		}
		_, _ = w.Write([]byte(`{"id":"j1","status":"in_progress"}`))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("machine")
	_, err := m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	if !errors.Is(err, reason) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
}
