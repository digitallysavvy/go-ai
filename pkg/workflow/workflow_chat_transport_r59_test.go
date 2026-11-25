package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// r59OpenStreamServer answers every request with a 200 SSE response that
// stays open until the test ends (a run still streaming).
func r59OpenStreamServer(t *testing.T, runID string) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if runID != "" {
			w.Header().Set("x-workflow-run-id", runID)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"start\"}\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv
}

// TS WorkflowChatTransport throws as soon as the run ID header is missing or
// onChatSendMessage throws; it does not read the rest of the response first.
// Go drained the body, so the error waited for the run's stream to end.
func TestR59WorkflowChatTransportFailsWithoutDrainingTheStream(t *testing.T) {
	hookErr := errors.New("hook failed")
	cases := map[string]struct {
		runID string
		hook  func(*http.Response, ai.ChatTransportSendMessagesRequest) error
		want  string
	}{
		"missing run ID header": {want: `Workflow run ID not found in "x-workflow-run-id" response header`},
		"OnChatSendMessage error": {runID: "run-1", hook: func(*http.Response, ai.ChatTransportSendMessagesRequest) error {
			return hookErr
		}, want: hookErr.Error()},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := r59OpenStreamServer(t, tc.runID)
			tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL, OnChatSendMessage: tc.hook})
			out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat"})
			_, err := collectChunks(out, errs, 2*time.Second)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q without waiting for the stream to end", err, tc.want)
			}
		})
	}
}

// The multiplexer returns an OnChatSendMessage error without reading the rest
// of the run's stream.
func TestR59MultiplexerHookErrorDoesNotDrainTheStream(t *testing.T) {
	srv := r59OpenStreamServer(t, "run-1")
	hookErr := errors.New("hook failed")
	tr := NewWorkflowRunMultiplexer(WorkflowRunMultiplexerOptions{API: srv.URL, OnChatSendMessage: func(*http.Response, SendMessagesOptions) error {
		return hookErr
	}})
	done := make(chan error, 1)
	go func() {
		_, err := tr.SendMessages(context.Background(), SendMessagesOptions{ChatID: "chat"})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, hookErr) {
			t.Fatalf("err = %v, want the hook error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SendMessages waited for the stream to end")
	}
}

// A failed POST or reconnect reports at most 4 KiB of the error body, like
// WorkflowChatTransport, instead of reading an unbounded body into the error.
func TestR59MultiplexerErrorBodyIsBounded(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()
	tr := NewWorkflowRunMultiplexer(WorkflowRunMultiplexerOptions{API: srv.URL})
	_, err := tr.SendMessages(context.Background(), SendMessagesOptions{ChatID: "chat"})
	if err == nil || len(err.Error()) > 4096+len(srv.URL)+100 {
		t.Fatalf("POST error length = %d, want the body capped at 4 KiB", len(err.Error()))
	}
	_, err = tr.ReconnectToStream(context.Background(), ReconnectToStreamOptions{RunID: "run-1"})
	if err == nil || len(err.Error()) > 4096+len(srv.URL)+100 {
		t.Fatalf("GET error length = %d, want the body capped at 4 KiB", len(err.Error()))
	}
}
