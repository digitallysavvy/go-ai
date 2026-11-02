package google

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TS streamGoogleInteractionEvents reads parseJsonEventStream output, which
// drops `data: [DONE]`: a connection that carries only [DONE] produced no
// event, counts as a failed attempt, and the stream errors after maxRetries.
// Go counted the [DONE] frame as an event, reset the failure counter and
// reconnected forever.
func TestR79prova_InteractionsDoneOnlyConnectionsCountAsEmpty(t *testing.T) {
	shortRetryDelay(t)
	srv := &interactionsRetryServer{handler: func(_ int, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}}
	ts := srv.serve(t)
	defer ts.Close()

	st := backgroundStream(t, ts.URL)
	defer func() { _ = st.Close() }()
	done := make(chan error, 1)
	go func() { _, err := drainRetryStream(st); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "closed without producing any events") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream kept reconnecting forever")
	}
	if n := srv.getCount(); n != 3 {
		t.Fatalf("GET requests = %d, want 3", n)
	}
}
