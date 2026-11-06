package workflow

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS reconnectToStreamIterator: a reconnect stream that delivers its finish
// chunk and then fails (here a malformed frame) is still caught and counted
// as a consecutive error; reaching maxConsecutiveErrors throws, although the
// finish chunk was seen. Go ended the loop at the finish chunk.
func TestR60WorkflowChatTransportErrorAfterFinishCountsAsReconnectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"start\"}\n\ndata: {\"type\":\"finish\"}\n\ndata: not json\n\n")
	}))
	defer srv.Close()

	t.Run("limit reached", func(t *testing.T) {
		var ended bool
		tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL, MaxConsecutiveErrors: 1,
			OnChatEnd: func(WorkflowChatTransportEndEvent) error { ended = true; return nil }})
		out, errs := tr.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
		chunks, err := collectChunks(out, errs, 3*time.Second)
		if err == nil || !strings.Contains(err.Error(), "Failed to reconnect after 1 consecutive errors. Last error: Error: invalid UI message chunk") {
			t.Fatalf("err = %v", err)
		}
		if len(chunks) != 2 || ended {
			t.Fatalf("chunks = %v, OnChatEnd called = %v; want start and finish, no OnChatEnd", chunks, ended)
		}
	})
	t.Run("below the limit", func(t *testing.T) {
		var end WorkflowChatTransportEndEvent
		tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL,
			OnChatEnd: func(e WorkflowChatTransportEndEvent) error { end = e; return nil }})
		out, errs := tr.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
		if _, err := collectChunks(out, errs, 3*time.Second); err != nil {
			t.Fatal(err)
		}
		if end.ChatID != "c" || end.ChunkIndex != 2 {
			t.Fatalf("OnChatEnd = %+v, want chat c at chunk 2", end)
		}
	})
}
