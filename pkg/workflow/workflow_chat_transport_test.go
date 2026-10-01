package workflow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

func TestWorkflowRunMultiplexerSSEAndResume(t *testing.T) {
	tr := &WorkflowRunMultiplexer{}
	srv := httptest.NewServer(tr)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()
	runID := resp.Header.Get("X-Workflow-Run-ID")
	if runID == "" {
		t.Fatalf("missing run ID header")
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, "event: start") || !strings.Contains(text, "event: finish") {
		t.Fatalf("unexpected SSE body: %s", text)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resume", nil)
	tr.Resume(runID).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("resume code = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "event: start") {
		t.Fatalf("resume body missing replayed events: %s", rr.Body.String())
	}
}

func TestWorkflowRunMultiplexerResumeNotFound(t *testing.T) {
	tr := &WorkflowRunMultiplexer{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resume", nil)
	tr.Resume("missing").ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestWorkflowRunMultiplexerResumeStartIndex(t *testing.T) {
	tr := &WorkflowRunMultiplexer{}
	runID := "run-start-index"
	tr.appendEvent(runID, "start", `{"n":1}`)
	tr.appendEvent(runID, "progress", `{"n":2}`)
	tr.appendEvent(runID, "finish", `{"n":3}`)
	tr.finish(runID)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resume?startIndex=1", nil)
	tr.Resume(runID).ServeHTTP(rr, req)
	body := rr.Body.String()
	if strings.Contains(body, `event: start`) {
		t.Fatalf("expected start to be skipped by startIndex, got %s", body)
	}
	if !strings.Contains(body, `event: progress`) || !strings.Contains(body, `event: finish`) {
		t.Fatalf("expected progress+finish events, got %s", body)
	}
}

func TestWorkflowRunMultiplexerResumeNegativeStartIndex(t *testing.T) {
	tr := &WorkflowRunMultiplexer{}
	runID := "run-tail"
	tr.appendEvent(runID, "e1", `1`)
	tr.appendEvent(runID, "e2", `2`)
	tr.appendEvent(runID, "e3", `3`)
	tr.finish(runID)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resume?startIndex=-1", nil)
	tr.Resume(runID).ServeHTTP(rr, req)
	body := rr.Body.String()
	if strings.Contains(body, `event: e1`) || strings.Contains(body, `event: e2`) {
		t.Fatalf("expected only tail event, got %s", body)
	}
	if !strings.Contains(body, `event: e3`) {
		t.Fatalf("expected tail event e3, got %s", body)
	}
}

func TestWorkflowRunMultiplexerClientSendAndReconnect(t *testing.T) {
	var seenPost bool
	var seenReconnect bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Workflow-Run-ID", "run-client")
		sse := streaming.NewSSEWriter(w)
		switch r.Method {
		case http.MethodPost:
			if r.Header.Get("X-Test") != "send" {
				t.Fatalf("missing prepared send header")
			}
			seenPost = true
			_ = sse.WriteNamedEvent("start", `{"runId":"run-client"}`)
		case http.MethodGet:
			if r.Header.Get("X-Test") != "reconnect" {
				t.Fatalf("missing prepared reconnect header")
			}
			if r.URL.Query().Get("runId") != "run-client" || r.URL.Query().Get("startIndex") != "2" {
				t.Fatalf("unexpected reconnect query: %s", r.URL.RawQuery)
			}
			seenReconnect = true
			_ = sse.WriteNamedEvent("finish", `{"status":"done"}`)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
		_ = sse.WriteDone()
	}))
	defer srv.Close()

	var sendHook bool
	var endHook bool
	var prepareSend bool
	var prepareReconnect bool
	tr := NewWorkflowRunMultiplexer(WorkflowRunMultiplexerOptions{
		API: srv.URL,
		OnChatSendMessage: func(resp *http.Response, opts SendMessagesOptions) error {
			sendHook = resp.Header.Get("X-Workflow-Run-ID") == "run-client" && opts.ChatID == "chat-1"
			return nil
		},
		OnChatEnd: func(e ChatEndEvent) error {
			endHook = e.ChatID == "chat-1" && e.ChunkIndex > 0 && e.RunID == "run-client" && len(e.Events) > 0
			return nil
		},
		PrepareSendMessagesRequest: func(opts SendMessagesOptions) (PreparedChatRequest, error) {
			prepareSend = opts.ChatID == "chat-1"
			return PreparedChatRequest{Headers: map[string]string{"X-Test": "send"}}, nil
		},
		PrepareReconnectToStreamRequest: func(opts ReconnectToStreamOptions) (PreparedChatRequest, error) {
			prepareReconnect = opts.RunID == "run-client"
			return PreparedChatRequest{Headers: map[string]string{"X-Test": "reconnect"}}, nil
		},
	})
	events, err := tr.SendMessages(context.Background(), SendMessagesOptions{ChatID: "chat-1", Messages: []map[string]string{{"role": "user", "content": "hi"}}})
	if err != nil {
		t.Fatalf("SendMessages error: %v", err)
	}
	if !seenPost || !sendHook || !endHook || !prepareSend || len(events) == 0 || events[0].Event != "start" {
		t.Fatalf("unexpected send state: seenPost=%v sendHook=%v endHook=%v prepare=%v events=%+v", seenPost, sendHook, endHook, prepareSend, events)
	}
	events, err = tr.ReconnectToStream(context.Background(), ReconnectToStreamOptions{RunID: "run-client", ChatID: "chat-1", StartIndex: 2})
	if err != nil {
		t.Fatalf("ReconnectToStream error: %v", err)
	}
	if !seenReconnect || !prepareReconnect || len(events) == 0 || events[0].Event != "finish" {
		t.Fatalf("unexpected reconnect state: seen=%v prepare=%v events=%+v", seenReconnect, prepareReconnect, events)
	}
}

// TestWorkflowRunMultiplexerAppendEventDropIsObservable is a permanent
// regression test for R2-6: appendEvent's non-blocking fan-out to a
// watcher's fixed-size buffer used to drop an event with a bare
// `select { case ch <- re: default: }` and no trace of the drop anywhere.
// TS has no equivalent server-side multiplexer (see the type doc on
// WorkflowRunMultiplexer) to mirror a buffering/backpressure/drop policy
// from, so this keeps the existing non-blocking fan-out (required so one
// slow watcher can never stall the run or every other watcher) but makes a
// drop observable: counted, logged once, and never lost from the replayable
// history recorded in rs.events.
func TestWorkflowRunMultiplexerAppendEventDropIsObservable(t *testing.T) {
	tr := &WorkflowRunMultiplexer{}
	runID := "run-drop-1"
	watch := make(chan runEvent, 2) // tiny, intentionally never drained
	rs := &runState{watchers: map[chan runEvent]struct{}{watch: {}}}
	tr.Runs.Store(runID, rs)

	const total = 5
	for i := 0; i < total; i++ {
		tr.appendEvent(runID, "progress", fmt.Sprintf(`{"i":%d}`, i))
	}

	rs.mu.Lock()
	dropped := rs.droppedEvents
	warned := rs.warnedDrop
	recorded := len(rs.events)
	rs.mu.Unlock()

	if recorded != total {
		t.Fatalf("rs.events recorded %d events, want %d: a watcher's buffer filling must never drop events from the replayable history", recorded, total)
	}
	if dropped == 0 {
		t.Fatal("expected appendEvent to count at least one drop once the watcher's buffer filled")
	}
	if !warned {
		t.Fatal("expected appendEvent to have logged the drop (warnedDrop)")
	}
}
