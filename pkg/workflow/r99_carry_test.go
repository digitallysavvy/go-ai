package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// fetch gives a 204 or 205 response a null body (node 24: response.ok is
// true and response.body is null), so TS `!response.ok || !response.body`
// throws "Failed to fetch chat: <status> " on the POST and on a reconnect.
// Go read the empty body as a stream that ended without a finish chunk.
func TestR99carryWorkflowChatTransportNullBodyStatus(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("x-workflow-run-id", "run")
			w.WriteHeader(status)
		}))
		want := "Failed to fetch chat: " + strconv.Itoa(status) + " "

		tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
		chunks, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "c"})
		for range chunks {
		}
		if err := <-errs; err == nil || err.Error() != want {
			t.Errorf("%d send error = %v, want %q", status, err, want)
		}

		chunks, errs = tr.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
		for range chunks {
		}
		if err := <-errs; err == nil || err.Error() != want {
			t.Errorf("%d reconnect error = %v, want %q", status, err, want)
		}
		srv.Close()
	}
}
