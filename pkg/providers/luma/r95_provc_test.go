package luma

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS pollForImageUrl waits with delay(pollIntervalMillis) (no signal), so an
// abort surfaces from the next getFromApi as the abort reason.
func TestR95provcPollAbortReturnsCause(t *testing.T) {
	reason := errors.New("r95provc abort reason")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			// Abort after the poll response arrived, during the wait.
			time.AfterFunc(100*time.Millisecond, func() { cancel(reason) })
		}
		_, _ = w.Write([]byte(`{"id":"g1","state":"queued"}`))
	}))
	defer srv.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelPhoton1)
	_, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "x"})
	if !errors.Is(err, reason) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
}
