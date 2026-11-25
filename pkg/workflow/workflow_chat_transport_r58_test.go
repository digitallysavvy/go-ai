package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TS WorkflowChatTransport throws on a failed POST before it calls
// onChatSendMessage ("invoked after successfully sending messages"); the
// multiplexer's client called the hook with the error response first.
func TestR58MultiplexerFailedPostSkipsOnChatSendMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	called := false
	tr := NewWorkflowRunMultiplexer(WorkflowRunMultiplexerOptions{
		API: srv.URL,
		OnChatSendMessage: func(*http.Response, SendMessagesOptions) error {
			called = true
			return nil
		},
	})
	if _, err := tr.SendMessages(context.Background(), SendMessagesOptions{ChatID: "c"}); err == nil {
		t.Fatal("want an error for the failed POST")
	}
	if called {
		t.Fatal("OnChatSendMessage was called for a failed POST")
	}
}
