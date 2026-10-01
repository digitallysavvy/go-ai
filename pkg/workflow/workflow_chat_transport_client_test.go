package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

func writeChunk(t *testing.T, sse *streaming.SSEWriter, chunk map[string]interface{}) {
	t.Helper()
	b, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := sse.WriteData(string(b)); err != nil {
		t.Fatalf("WriteData: %v", err)
	}
}

func collectChunks(out <-chan ai.UIMessageChunk, errs <-chan error, timeout time.Duration) ([]ai.UIMessageChunk, error) {
	var chunks []ai.UIMessageChunk
	deadline := time.After(timeout)
	for {
		select {
		case c, ok := <-out:
			if !ok {
				out = nil
			} else {
				chunks = append(chunks, c)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
			} else if err != nil {
				return chunks, err
			}
		case <-deadline:
			return chunks, fmt.Errorf("timed out waiting for channels to close")
		}
		if out == nil && errs == nil {
			return chunks, nil
		}
	}
}

// var _ ChatTransport implements are checked via the package-level assertion
// in workflow_chat_transport_client.go.

func TestWorkflowChatTransportSendMessagesBasic(t *testing.T) {
	var gotMessages bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		gotMessages = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-workflow-run-id", "run-1")
		sse := streaming.NewSSEWriter(w)
		writeChunk(t, sse, map[string]interface{}{"type": "text-start", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-delta", "id": "0", "delta": "hi"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-end", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "finish"})
	}))
	defer srv.Close()

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{
		ChatID: "chat-1", Trigger: "submit-message",
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
	})
	chunks, err := collectChunks(out, errs, 2*time.Second)
	if err != nil {
		t.Fatalf("SendMessages error: %v", err)
	}
	if !gotMessages {
		t.Fatal("server never saw the POST")
	}
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[len(chunks)-1]["type"] != "finish" {
		t.Fatalf("expected the last chunk to be finish, got %+v", chunks[len(chunks)-1])
	}
}

func TestWorkflowChatTransportMissingRunIDHeaderErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse := streaming.NewSSEWriter(w)
		writeChunk(t, sse, map[string]interface{}{"type": "finish"})
	}))
	defer srv.Close()

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat-1"})
	_, err := collectChunks(out, errs, 2*time.Second)
	if err == nil {
		t.Fatal("expected an error when the run id header is missing")
	}
}

// TestWorkflowChatTransportMalformedChunkTriggersReconnect covers
// workflow-chat-transport.ts:340-353: a JSON-invalid (or schema-invalid)
// frame is fatal for the current stream attempt, not silently skipped —
// TS's parseJsonEventStream throws, the enclosing try/catch logs and falls
// through exactly as if the connection had dropped, and the transport
// reconnects. A prior Go implementation `continue`d past the bad frame and
// kept reading from the same (now desynced) connection instead.
func TestWorkflowChatTransportMalformedChunkTriggersReconnect(t *testing.T) {
	var postCount, getCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse := streaming.NewSSEWriter(w)
		switch r.Method {
		case http.MethodPost:
			postCount++
			w.Header().Set("x-workflow-run-id", "run-malformed")
			w.WriteHeader(http.StatusOK)
			writeChunk(t, sse, map[string]interface{}{"type": "text-start", "id": "0"})
			// A malformed frame: not valid JSON at all. The pump must stop
			// here (not skip it and keep reading) so the transport falls
			// through to reconnect, matching TS.
			if err := sse.WriteData("{not valid json"); err != nil {
				t.Fatalf("WriteData: %v", err)
			}
			// If the pump wrongly kept reading past the malformed frame, it
			// would also consume this "finish" on the SAME connection and
			// the reconnect GET below would never happen.
			writeChunk(t, sse, map[string]interface{}{"type": "finish"})
		case http.MethodGet:
			getCount++
			if got := r.URL.Query().Get("startIndex"); got != "1" {
				t.Fatalf("expected reconnect startIndex=1 (only the text-start counted), got %s", got)
			}
			writeChunk(t, sse, map[string]interface{}{"type": "text-delta", "id": "0", "delta": "hi"})
			writeChunk(t, sse, map[string]interface{}{"type": "text-end", "id": "0"})
			writeChunk(t, sse, map[string]interface{}{"type": "finish"})
		}
	}))
	defer srv.Close()

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat-malformed"})
	chunks, err := collectChunks(out, errs, 2*time.Second)
	if err != nil {
		t.Fatalf("SendMessages error: %v", err)
	}
	if postCount != 1 || getCount != 1 {
		t.Fatalf("expected 1 POST and 1 reconnect GET (malformed frame must not be silently skipped), got post=%d get=%d", postCount, getCount)
	}
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks (text-start + reconnected delta/end/finish), got %d: %+v", len(chunks), chunks)
	}
}

func TestWorkflowChatTransportReconnectsWhenStreamEndsWithoutFinish(t *testing.T) {
	var postCount, getCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse := streaming.NewSSEWriter(w)
		switch r.Method {
		case http.MethodPost:
			postCount++
			w.Header().Set("x-workflow-run-id", "run-2")
			w.WriteHeader(http.StatusOK)
			writeChunk(t, sse, map[string]interface{}{"type": "text-start", "id": "0"})
			// Stream ends here without a finish chunk (simulated drop).
		case http.MethodGet:
			getCount++
			if r.URL.Path != "/run-2/stream" {
				t.Fatalf("unexpected reconnect path: %s", r.URL.Path)
			}
			if got := r.URL.Query().Get("startIndex"); got != "1" {
				t.Fatalf("expected reconnect startIndex=1, got %s", got)
			}
			writeChunk(t, sse, map[string]interface{}{"type": "text-delta", "id": "0", "delta": "hi"})
			writeChunk(t, sse, map[string]interface{}{"type": "text-end", "id": "0"})
			writeChunk(t, sse, map[string]interface{}{"type": "finish"})
		}
	}))
	defer srv.Close()

	var endEvent WorkflowChatTransportEndEvent
	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{
		API: srv.URL,
		OnChatEnd: func(e WorkflowChatTransportEndEvent) error {
			endEvent = e
			return nil
		},
	})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat-2"})
	chunks, err := collectChunks(out, errs, 2*time.Second)
	if err != nil {
		t.Fatalf("SendMessages error: %v", err)
	}
	if postCount != 1 || getCount != 1 {
		t.Fatalf("expected 1 POST and 1 reconnect GET, got post=%d get=%d", postCount, getCount)
	}
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks (1 from POST + 3 from reconnect), got %d: %+v", len(chunks), chunks)
	}
	if endEvent.ChatID != "chat-2" || endEvent.ChunkIndex != 4 {
		t.Fatalf("unexpected OnChatEnd event: %+v", endEvent)
	}
}

// TestWorkflowChatTransportFramingRepairSynthesizesMissingStart mirrors TS's
// normalizeUIMessageStreamParts tests: an orphaned text-delta (no preceding
// text-start) gets a synthesized text-start inserted ahead of it.
func TestWorkflowChatTransportFramingRepairSynthesizesMissingStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-workflow-run-id", "run-3")
		sse := streaming.NewSSEWriter(w)
		// No text-start: the server-side step retried and this is the tail
		// end of a part whose start was lost.
		writeChunk(t, sse, map[string]interface{}{"type": "text-delta", "id": "0", "delta": "hi"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-end", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "finish"})
	}))
	defer srv.Close()

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat-3"})
	chunks, err := collectChunks(out, errs, 2*time.Second)
	if err != nil {
		t.Fatalf("SendMessages error: %v", err)
	}
	if len(chunks) != 4 {
		t.Fatalf("expected a synthesized text-start plus the 3 original chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0]["type"] != "text-start" || chunks[0]["id"] != "0" {
		t.Fatalf("expected a synthesized text-start first, got %+v", chunks[0])
	}
}

// TestWorkflowChatTransportResetStepClearsFrameState mirrors TS's
// "forgets started parts after a reset-step chunk" test: a part id reused
// after reset-step is treated as a fresh part, not a duplicate.
func TestWorkflowChatTransportResetStepClearsFrameState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-workflow-run-id", "run-4")
		sse := streaming.NewSSEWriter(w)
		writeChunk(t, sse, map[string]interface{}{"type": "text-start", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-end", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "reset-step"})
		// Id "0" is reused after reset-step: this must NOT be dropped as a
		// duplicate start.
		writeChunk(t, sse, map[string]interface{}{"type": "text-start", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-delta", "id": "0", "delta": "hi"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-end", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "finish"})
	}))
	defer srv.Close()

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat-4"})
	chunks, err := collectChunks(out, errs, 2*time.Second)
	if err != nil {
		t.Fatalf("SendMessages error: %v", err)
	}
	if len(chunks) != 7 {
		t.Fatalf("expected all 7 chunks to pass through, got %d: %+v", len(chunks), chunks)
	}
}

// TestWorkflowChatTransportOrphanFilterDropsUnstartedReasoningDelta mirrors
// TS's "drops orphan reasoning-delta / reasoning-end when no prior
// reasoning-start" test: on a negative-startIndex resume, a delta/end whose
// start fell outside the resumed window is dropped instead of crashing the
// consumer.
func TestWorkflowChatTransportOrphanFilterDropsUnstartedReasoningDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse := streaming.NewSSEWriter(w)
		w.Header().Set("x-workflow-stream-tail-index", "9")
		// Resumed window starts mid-part: no reasoning-start observed here.
		writeChunk(t, sse, map[string]interface{}{"type": "reasoning-delta", "id": "r0", "delta": "..."})
		writeChunk(t, sse, map[string]interface{}{"type": "reasoning-end", "id": "r0"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-start", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-delta", "id": "0", "delta": "hi"})
		writeChunk(t, sse, map[string]interface{}{"type": "text-end", "id": "0"})
		writeChunk(t, sse, map[string]interface{}{"type": "finish"})
	}))
	defer srv.Close()

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL, InitialStartIndex: -6})
	out, errs := tr.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "run-5"})
	chunks, err := collectChunks(out, errs, 2*time.Second)
	if err != nil {
		t.Fatalf("ReconnectToStream error: %v", err)
	}
	for _, c := range chunks {
		if c["type"] == "reasoning-delta" || c["type"] == "reasoning-end" {
			t.Fatalf("expected orphaned reasoning chunks to be dropped, got %+v", chunks)
		}
	}
	if len(chunks) != 4 {
		t.Fatalf("expected the 3 text chunks + finish to pass through, got %d: %+v", len(chunks), chunks)
	}
}

func TestWorkflowChatTransportOnChatSendMessageCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-workflow-run-id", "run-6")
		sse := streaming.NewSSEWriter(w)
		writeChunk(t, sse, map[string]interface{}{"type": "finish"})
	}))
	defer srv.Close()

	var sawChatID string
	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{
		API: srv.URL,
		OnChatSendMessage: func(resp *http.Response, req ai.ChatTransportSendMessagesRequest) error {
			sawChatID = req.ChatID
			if resp.Header.Get("x-workflow-run-id") != "run-6" {
				t.Fatalf("expected access to the response in OnChatSendMessage")
			}
			return nil
		},
	})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat-6"})
	if _, err := collectChunks(out, errs, 2*time.Second); err != nil {
		t.Fatalf("SendMessages error: %v", err)
	}
	if sawChatID != "chat-6" {
		t.Fatalf("expected OnChatSendMessage to see chat-6, got %q", sawChatID)
	}
}

// TestWorkflowChatTransportPumpChunkStreamGoroutineLeak is a permanent
// regression test for R2-4: pumpChunkStream used to send repaired chunks
// with an unconditional blocking `out <- repaired`, never selecting on
// ctx.Done(). A consumer that stops draining the channel SendMessages
// returns -- without separately cancelling ctx -- left the goroutine running
// t.sendMessages permanently blocked. Adapted from the bug report's
// TestZZBugReviewWorkflowChatTransportPumpChunkStreamGoroutineLeak
// (state/parity/sep_23_2026/bug-review/R2.md): this asserts the *fixed*
// behavior (cancelling ctx unblocks the pump) rather than the original
// leak repro (which asserted a `runtime.Stack` dump still showed
// "pumpChunkStream" 150ms after the consumer stopped reading).
func TestWorkflowChatTransportPumpChunkStreamGoroutineLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-workflow-run-id", "run-leak")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i := 0; i < 50; i++ {
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"text-delta\",\"id\":\"t1\",\"delta\":\"chunk-%d\"}\n\n", i)
			flusher.Flush()
			time.Sleep(2 * time.Millisecond)
		}
	}))
	defer srv.Close()

	transport := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	out, _ := transport.SendMessages(ctx, ai.ChatTransportSendMessagesRequest{ChatID: "c1"})
	<-out // read one chunk, then stop draining.

	// Before the fix, no amount of waiting here would unblock the pump: it
	// had no ctx.Done() case on its `out <-` send at all. The fix makes
	// cancelling ctx -- the normal way a ChatTransport consumer gives up on
	// a stream -- unblock it. Confirm the pump goroutine is gone afterward
	// (rather than asserting the old test's "still blocked" negative) by
	// checking the stack no longer mentions pumpChunkStream, bounded by a
	// retry loop instead of one fixed sleep.
	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), "pumpChunkStream") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("pumpChunkStream goroutine is still blocked after cancelling ctx")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
