package workflow

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// Ports workflow-chat-transport.test.ts: "positive initialStartIndex",
// "negative initialStartIndex" and "orphan UI chunk filter on negative
// startIndex resume".

type scriptedResponse struct {
	header map[string]string
	frames []string
}

type scriptedServer struct {
	mu   sync.Mutex
	urls []string
	srv  *httptest.Server
}

func newScriptedServer(t *testing.T, responses ...scriptedResponse) *scriptedServer {
	t.Helper()
	s := &scriptedServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		n := len(s.urls)
		s.urls = append(s.urls, r.URL.RequestURI())
		s.mu.Unlock()
		if n >= len(responses) {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for k, v := range responses[n].header {
			w.Header().Set(k, v)
		}
		for _, f := range responses[n].frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *scriptedServer) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.urls...)
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(nil) })
	return &buf
}

func chunkTypes(chunks []ai.UIMessageChunk) []string {
	types := make([]string, 0, len(chunks))
	for _, c := range chunks {
		types = append(types, stringField(c, "type"))
	}
	return types
}

func reconnect(t *testing.T, tr *WorkflowChatTransport, req ai.ChatTransportReconnectToStreamRequest) []ai.UIMessageChunk {
	t.Helper()
	out, errs := tr.ReconnectToStream(context.Background(), req)
	chunks, err := collectChunks(out, errs, 5*time.Second)
	if err != nil {
		t.Fatalf("ReconnectToStream: %v", err)
	}
	return chunks
}

func TestTSChatTransportStartIndexResolution(t *testing.T) {
	finish := `{"type":"finish"}`
	cases := []struct {
		name      string
		initial   int
		override  *int
		first     scriptedResponse
		wantURLs  []string // paths after /api/chat
		wantWarn  string
		wantNoLog bool
	}{
		{"positive start index adjusts retries", 100, nil, scriptedResponse{}, []string{"?startIndex=100", "?startIndex=100"}, "", true},
		{"negative start index resolves from tail header", -20, nil, scriptedResponse{header: map[string]string{"x-workflow-stream-tail-index": "499"}}, []string{"?startIndex=-20", "?startIndex=480"}, "", true},
		{"missing tail header falls back to 0", -10, nil, scriptedResponse{}, []string{"?startIndex=-10", "?startIndex=0"}, `Negative initialStartIndex is configured`, false},
		{"unparseable tail header falls back to 0", -5, nil, scriptedResponse{header: map[string]string{"x-workflow-stream-tail-index": "not-a-number"}}, []string{"?startIndex=-5", "?startIndex=0"}, `valid "x-workflow-stream-tail-index"`, false},
		{"parseInt-style tail header prefix", -5, nil, scriptedResponse{header: map[string]string{"x-workflow-stream-tail-index": "99 items"}}, []string{"?startIndex=-5", "?startIndex=95"}, "", true},
		{"tail header resolves below zero to 0", -20, nil, scriptedResponse{header: map[string]string{"x-workflow-stream-tail-index": "3"}}, []string{"?startIndex=-20", "?startIndex=0"}, "", true},
		{"per-call startIndex overrides the default", 0, intPtr(-7), scriptedResponse{header: map[string]string{"x-workflow-stream-tail-index": "20"}}, []string{"?startIndex=-7", "?startIndex=14"}, "", true},
		{"per-call 0 overrides a negative default", -7, intPtr(0), scriptedResponse{}, []string{"?startIndex=0", "?startIndex=0"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLog(t)
			srv := newScriptedServer(t, c.first, scriptedResponse{frames: []string{finish}})
			tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.srv.URL + "/api/chat", InitialStartIndex: c.initial})
			reconnect(t, tr, ai.ChatTransportReconnectToStreamRequest{ChatID: "test-chat", StartIndex: c.override})
			var want []string
			for _, q := range c.wantURLs {
				want = append(want, "/api/chat/test-chat/stream"+q)
			}
			if got := srv.requested(); !reflect.DeepEqual(got, want) {
				t.Fatalf("requests = %v, want %v", got, want)
			}
			if c.wantWarn != "" && !strings.Contains(logs.String(), c.wantWarn) {
				t.Fatalf("log %q lacks %q", logs.String(), c.wantWarn)
			}
			if c.wantNoLog && strings.Contains(logs.String(), "Negative initialStartIndex") {
				t.Fatalf("unexpected warning: %s", logs.String())
			}
		})
	}
}

func intPtr(n int) *int { return &n }

func TestTSChatTransportOrphanFilter(t *testing.T) {
	tail := map[string]string{"x-workflow-stream-tail-index": "50"}
	cases := []struct {
		name      string
		initial   int
		header    map[string]string
		frames    []string
		wantTypes []string
		wantWarns int
	}{
		{
			"drops orphan reasoning-delta and reasoning-end", -50, map[string]string{"x-workflow-stream-tail-index": "99"},
			[]string{
				`{"type":"reasoning-delta","id":"reasoning-0","delta":" space"}`,
				`{"type":"reasoning-delta","id":"reasoning-0","delta":"."}`,
				`{"type":"reasoning-end","id":"reasoning-0"}`,
				`{"type":"finish-step"}`, `{"type":"start-step"}`,
				`{"type":"reasoning-start","id":"reasoning-1"}`,
				`{"type":"reasoning-delta","id":"reasoning-1","delta":"hello"}`,
				`{"type":"reasoning-end","id":"reasoning-1"}`,
				`{"type":"finish"}`,
			},
			[]string{"finish-step", "start-step", "reasoning-start", "reasoning-delta", "reasoning-end", "finish"}, 1,
		},
		{
			"drops orphan text-delta and text-end", -10, tail,
			[]string{`{"type":"text-delta","id":"text-0","delta":"orphan"}`, `{"type":"text-end","id":"text-0"}`, `{"type":"finish"}`},
			[]string{"finish"}, 1,
		},
		{
			"drops orphan tool-input-delta and tool-output-available", -10, tail,
			[]string{`{"type":"tool-input-delta","toolCallId":"call_1","inputTextDelta":"x"}`, `{"type":"tool-output-available","toolCallId":"call_1","output":{}}`, `{"type":"finish"}`},
			[]string{"finish"}, 1,
		},
		{
			"passes through chunks whose start is in the window", -10, tail,
			[]string{
				`{"type":"tool-input-start","toolCallId":"call_1","toolName":"grep"}`,
				`{"type":"tool-input-available","toolCallId":"call_1","toolName":"grep","input":{}}`,
				`{"type":"tool-output-available","toolCallId":"call_1","output":{"ok":true}}`,
				`{"type":"finish"}`,
			},
			[]string{"tool-input-start", "tool-input-available", "tool-output-available", "finish"}, 0,
		},
		{
			"forgets started parts after reset-step", -10, tail,
			[]string{
				`{"type":"tool-input-start","toolCallId":"stale","toolName":"deleteFile"}`,
				`{"type":"reset-step"}`,
				`{"type":"tool-output-available","toolCallId":"stale","output":{}}`,
				`{"type":"tool-input-start","toolCallId":"retried","toolName":"deleteFile"}`,
				`{"type":"tool-input-available","toolCallId":"retried","toolName":"deleteFile","input":{}}`,
				`{"type":"finish"}`,
			},
			[]string{"tool-input-start", "reset-step", "tool-input-start", "tool-input-available", "finish"}, 1,
		},
		{
			"recovers a tool call via bare tool-input-available", -10, tail,
			[]string{
				`{"type":"tool-input-available","toolCallId":"call_1","toolName":"grep","input":{}}`,
				`{"type":"tool-output-available","toolCallId":"call_1","output":{"ok":true}}`,
				`{"type":"finish"}`,
			},
			[]string{"tool-input-available", "tool-output-available", "finish"}, 0,
		},
		{
			"inactive on positive initialStartIndex", 100, nil,
			[]string{`{"type":"reasoning-delta","id":"reasoning-0","delta":"x"}`, `{"type":"finish"}`},
			[]string{"reasoning-start", "reasoning-delta", "finish"}, 0,
		},
		{
			"inactive when startIndex is 0", 0, nil,
			[]string{`{"type":"reasoning-delta","id":"reasoning-0","delta":"x"}`, `{"type":"finish"}`},
			[]string{"reasoning-start", "reasoning-delta", "finish"}, 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLog(t)
			srv := newScriptedServer(t, scriptedResponse{header: c.header, frames: c.frames})
			tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.srv.URL + "/api/chat", InitialStartIndex: c.initial})
			chunks := reconnect(t, tr, ai.ChatTransportReconnectToStreamRequest{ChatID: "test-chat"})
			if got := chunkTypes(chunks); !reflect.DeepEqual(got, c.wantTypes) {
				t.Fatalf("types = %v, want %v", got, c.wantTypes)
			}
			if got := strings.Count(logs.String(), "Dropping orphan UI chunk"); got != c.wantWarns {
				t.Fatalf("orphan warnings = %d, want %d (%s)", got, c.wantWarns, logs.String())
			}
		})
	}
}

// A POST stream that delivers no chunks resumes with the constructor's
// initialStartIndex, as TS reconnectToStreamIterator does when called with
// initialChunkIndex 0.
func TestTSChatTransportPostWithoutChunksUsesInitialStartIndex(t *testing.T) {
	var mu sync.Mutex
	var urls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if r.Method == http.MethodPost {
			w.Header().Set("x-workflow-run-id", "run-1")
			return
		}
		w.Header().Set("x-workflow-stream-tail-index", "9")
		_, _ = w.Write([]byte(`data: {"type":"finish"}` + "\n\n"))
	}))
	defer srv.Close()
	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL + "/api/chat", InitialStartIndex: -3})
	out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat-1", Trigger: "submit-message"})
	if _, err := collectChunks(out, errs, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"POST /api/chat", "GET /api/chat/run-1/stream?startIndex=-3"}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("requests = %v, want %v", urls, want)
	}
}

// Ports workflow-chat-transport.stream-repair.test.ts. The TS tests assemble
// the stream with readUIMessageStream; here the assertion is on the chunks the
// transport hands to that consumer (what must stay, what must be dropped).
func TestTSChatTransportStreamRepair(t *testing.T) {
	c := func(s string) string { return s }
	interleaved := []string{
		c(`{"type":"start","messageId":"m1"}`), c(`{"type":"start-step"}`),
		c(`{"type":"text-start","id":"0"}`), c(`{"type":"text-delta","id":"0","delta":"Hello"}`),
		c(`{"type":"finish-step"}`),
		c(`{"type":"text-delta","id":"0","delta":" world"}`), c(`{"type":"text-end","id":"0"}`), c(`{"type":"finish"}`),
	}
	interleavedReasoning := []string{
		c(`{"type":"start","messageId":"m1"}`), c(`{"type":"start-step"}`),
		c(`{"type":"reasoning-start","id":"0"}`), c(`{"type":"reasoning-delta","id":"0","delta":"Think"}`),
		c(`{"type":"finish-step"}`),
		c(`{"type":"reasoning-delta","id":"0","delta":"ing..."}`), c(`{"type":"reasoning-end","id":"0"}`), c(`{"type":"finish"}`),
	}
	duplicated := []string{
		c(`{"type":"start","messageId":"m1"}`), c(`{"type":"start-step"}`),
		c(`{"type":"text-start","id":"0"}`), c(`{"type":"text-delta","id":"0","delta":"Hello world"}`), c(`{"type":"text-end","id":"0"}`),
		c(`{"type":"text-start","id":"0"}`), c(`{"type":"text-delta","id":"0","delta":"Hello world"}`), c(`{"type":"text-end","id":"0"}`),
		c(`{"type":"finish"}`),
	}
	wellFormed := []string{
		c(`{"type":"start","messageId":"m1"}`), c(`{"type":"start-step"}`),
		c(`{"type":"text-start","id":"0"}`), c(`{"type":"text-delta","id":"0","delta":"one"}`), c(`{"type":"text-end","id":"0"}`), c(`{"type":"finish-step"}`),
		c(`{"type":"start-step"}`),
		c(`{"type":"text-start","id":"0"}`), c(`{"type":"text-delta","id":"0","delta":"two"}`), c(`{"type":"text-end","id":"0"}`), c(`{"type":"finish-step"}`),
		c(`{"type":"finish"}`),
	}
	cases := []struct {
		name   string
		frames []string
		want   []string // chunk types the consumer sees
	}{
		{"interleaved text part stays one part", interleaved,
			[]string{"start", "start-step", "text-start", "text-delta", "finish-step", "text-delta", "text-end", "finish"}},
		{"interleaved reasoning part stays one part", interleavedReasoning,
			[]string{"start", "start-step", "reasoning-start", "reasoning-delta", "finish-step", "reasoning-delta", "reasoning-end", "finish"}},
		{"re-delivered duplicate tail is dropped", duplicated,
			[]string{"start", "start-step", "text-start", "text-delta", "text-end", "finish"}},
		{"well-formed multi-step stream is unchanged", wellFormed,
			[]string{"start", "start-step", "text-start", "text-delta", "text-end", "finish-step", "start-step", "text-start", "text-delta", "text-end", "finish-step", "finish"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("x-workflow-run-id", "wrun_test")
				for _, f := range tc.frames {
					_, _ = w.Write([]byte("data: " + f + "\n\n"))
				}
			}))
			defer srv.Close()
			tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
			out, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "chat_test", Trigger: "submit-message"})
			chunks, err := collectChunks(out, errs, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if got := chunkTypes(chunks); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("types = %v\nwant    %v", got, tc.want)
			}
		})
	}
}
