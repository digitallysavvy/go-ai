package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS throws `Failed to fetch chat: ${response.status} ${await
// response.text()}`: the whole body, UTF-8 decoded (an invalid byte is
// U+FFFD). Go cut the body at 4096 bytes and kept raw bytes.
func TestR98carryWorkflowChatTransportFailedFetchBody(t *testing.T) {
	body := strings.Repeat("x", 5000) + "\xff<end>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	want := "Failed to fetch chat: 502 " + strings.Repeat("x", 5000) + "�<end>"

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	chunks, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "c"})
	for range chunks {
	}
	if err := <-errs; err == nil || err.Error() != want {
		t.Fatalf("send error = %v, want the whole decoded body", err)
	}

	chunks, errs = tr.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
	for range chunks {
	}
	if err := <-errs; err == nil || err.Error() != want {
		t.Fatalf("reconnect error = %v, want the whole decoded body", err)
	}
}
