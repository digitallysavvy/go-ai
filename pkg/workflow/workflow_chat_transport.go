package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/google/uuid"
)

type runEvent struct {
	Event string
	Data  string
}

type runState struct {
	mu       sync.Mutex
	events   []runEvent
	done     bool
	watchers map[chan runEvent]struct{}
}

// WorkflowChatTransport serves run-scoped SSE and resume handlers.
type WorkflowChatTransport struct {
	Runs sync.Map // map[string]*runState

	API                             string
	HTTPClient                      *http.Client
	MaxConsecutiveErrors            int
	InitialStartIndex               int
	OnChatSendMessage               func(*http.Response, SendMessagesOptions) error
	OnChatEnd                       func(ChatEndEvent) error
	PrepareSendMessagesRequest      func(SendMessagesOptions) (PreparedChatRequest, error)
	PrepareReconnectToStreamRequest func(ReconnectToStreamOptions) (PreparedChatRequest, error)
}

// WorkflowChatTransportOptions configures both server and client transport use.
type WorkflowChatTransportOptions struct {
	API                             string
	HTTPClient                      *http.Client
	MaxConsecutiveErrors            int
	InitialStartIndex               int
	OnChatSendMessage               func(*http.Response, SendMessagesOptions) error
	OnChatEnd                       func(ChatEndEvent) error
	PrepareSendMessagesRequest      func(SendMessagesOptions) (PreparedChatRequest, error)
	PrepareReconnectToStreamRequest func(ReconnectToStreamOptions) (PreparedChatRequest, error)
}

// SendMessagesOptions mirrors the TypeScript transport sendMessages call shape.
type SendMessagesOptions struct {
	ChatID    string
	MessageID string
	Trigger   string
	Messages  interface{}
	Body      map[string]interface{}
	Headers   map[string]string
}

// ReconnectToStreamOptions configures reconnect/resume requests.
type ReconnectToStreamOptions struct {
	RunID      string
	ChatID     string
	StartIndex int
	Headers    map[string]string
}

// ChatEndEvent is emitted after a client-side SSE stream finishes.
type ChatEndEvent struct {
	ChatID     string
	ChunkIndex int
	RunID      string
	Events     []*streaming.SSEEvent
}

// PreparedChatRequest customizes a WorkflowChatTransport client request.
type PreparedChatRequest struct {
	API     string
	Body    map[string]interface{}
	Headers map[string]string
}

// NewWorkflowChatTransport creates a transport with optional client settings.
func NewWorkflowChatTransport(opts ...WorkflowChatTransportOptions) *WorkflowChatTransport {
	t := &WorkflowChatTransport{}
	if len(opts) > 0 {
		o := opts[0]
		t.API = o.API
		t.HTTPClient = o.HTTPClient
		t.MaxConsecutiveErrors = o.MaxConsecutiveErrors
		t.InitialStartIndex = o.InitialStartIndex
		t.OnChatSendMessage = o.OnChatSendMessage
		t.OnChatEnd = o.OnChatEnd
		t.PrepareSendMessagesRequest = o.PrepareSendMessagesRequest
		t.PrepareReconnectToStreamRequest = o.PrepareReconnectToStreamRequest
	}
	if t.HTTPClient == nil {
		t.HTTPClient = http.DefaultClient
	}
	return t
}

func (t *WorkflowChatTransport) getOrCreate(runID string) *runState {
	if v, ok := t.Runs.Load(runID); ok {
		return v.(*runState)
	}
	rs := &runState{watchers: map[chan runEvent]struct{}{}}
	actual, _ := t.Runs.LoadOrStore(runID, rs)
	return actual.(*runState)
}

func (t *WorkflowChatTransport) appendEvent(runID, event, data string) {
	rs := t.getOrCreate(runID)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	re := runEvent{Event: event, Data: data}
	rs.events = append(rs.events, re)
	for ch := range rs.watchers {
		select {
		case ch <- re:
		default:
		}
	}
}

func (t *WorkflowChatTransport) finish(runID string) {
	rs := t.getOrCreate(runID)
	rs.mu.Lock()
	rs.done = true
	for ch := range rs.watchers {
		close(ch)
	}
	rs.watchers = map[chan runEvent]struct{}{}
	rs.mu.Unlock()
}

func (t *WorkflowChatTransport) client() *http.Client {
	if t != nil && t.HTTPClient != nil {
		return t.HTTPClient
	}
	return http.DefaultClient
}

func (t *WorkflowChatTransport) endpoint() string {
	if t != nil && t.API != "" {
		return t.API
	}
	return "/api/chat"
}

func mergeHeaders(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// SendMessages POSTs chat messages to the configured API and returns parsed SSE events.
func (t *WorkflowChatTransport) SendMessages(ctx context.Context, opts SendMessagesOptions) ([]*streaming.SSEEvent, error) {
	body := map[string]interface{}{}
	for k, v := range opts.Body {
		body[k] = v
	}
	if opts.Messages != nil {
		body["messages"] = opts.Messages
	}
	if opts.ChatID != "" {
		body["chatId"] = opts.ChatID
	}
	if opts.MessageID != "" {
		body["messageId"] = opts.MessageID
	}
	if opts.Trigger != "" {
		body["trigger"] = opts.Trigger
	}
	api := t.endpoint()
	headers := opts.Headers
	if t != nil && t.PrepareSendMessagesRequest != nil {
		prepared, err := t.PrepareSendMessagesRequest(opts)
		if err != nil {
			return nil, err
		}
		if prepared.API != "" {
			api = prepared.API
		}
		if prepared.Body != nil {
			body = prepared.Body
		}
		headers = mergeHeaders(headers, prepared.Headers)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if t.OnChatSendMessage != nil {
		if err := t.OnChatSendMessage(resp, opts); err != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil, err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("workflow chat transport: POST %s returned %d: %s", api, resp.StatusCode, string(payload))
	}
	events, err := streaming.ParseSSEStream(resp.Body)
	if err != nil {
		return nil, err
	}
	runID := resp.Header.Get("X-Workflow-Run-ID")
	if t.OnChatEnd != nil {
		if err := t.OnChatEnd(ChatEndEvent{ChatID: opts.ChatID, ChunkIndex: len(events), RunID: runID, Events: events}); err != nil {
			return events, err
		}
	}
	if runID != "" && !containsFinishEvent(events) {
		limit := 3
		if t != nil && t.MaxConsecutiveErrors > 0 {
			limit = t.MaxConsecutiveErrors
		}
		for attempt := 0; attempt < limit && !containsFinishEvent(events); attempt++ {
			replayed, err := t.ReconnectToStream(ctx, ReconnectToStreamOptions{
				RunID:      runID,
				ChatID:     opts.ChatID,
				StartIndex: len(events),
				Headers:    opts.Headers,
			})
			if err != nil {
				if attempt == limit-1 {
					return events, err
				}
				continue
			}
			events = append(events, replayed...)
		}
	}
	return events, nil
}

func containsFinishEvent(events []*streaming.SSEEvent) bool {
	for _, event := range events {
		if event == nil {
			continue
		}
		if event.Event == "finish" || event.Event == "done" || event.Data == "[DONE]" {
			return true
		}
	}
	return false
}

// ReconnectToStream GETs the configured API to resume a run-scoped SSE stream.
func (t *WorkflowChatTransport) ReconnectToStream(ctx context.Context, opts ReconnectToStreamOptions) ([]*streaming.SSEEvent, error) {
	if opts.RunID == "" {
		return nil, fmt.Errorf("workflow chat transport: run id is required")
	}
	startIndex := opts.StartIndex
	if startIndex == 0 && t != nil {
		startIndex = t.InitialStartIndex
	}
	api := t.endpoint()
	headers := opts.Headers
	if t != nil && t.PrepareReconnectToStreamRequest != nil {
		prepared, err := t.PrepareReconnectToStreamRequest(opts)
		if err != nil {
			return nil, err
		}
		if prepared.API != "" {
			api = prepared.API
		}
		headers = mergeHeaders(headers, prepared.Headers)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("runId", opts.RunID)
	if opts.ChatID != "" {
		q.Set("chatId", opts.ChatID)
	}
	q.Set("startIndex", strconv.Itoa(startIndex))
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("workflow chat transport: GET %s returned %d: %s", api, resp.StatusCode, string(payload))
	}
	events, err := streaming.ParseSSEStream(resp.Body)
	if err != nil {
		return nil, err
	}
	if t.OnChatEnd != nil {
		if err := t.OnChatEnd(ChatEndEvent{ChatID: opts.ChatID, ChunkIndex: len(events), RunID: opts.RunID, Events: events}); err != nil {
			return events, err
		}
	}
	return events, nil
}

// Resume attaches an SSE reader to an in-progress run.
func (t *WorkflowChatTransport) Resume(runID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, ok := t.Runs.Load(runID)
		if !ok {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		t.serveRun(w, r, runID, v.(*runState))
	})
}

func (t *WorkflowChatTransport) serveRun(w http.ResponseWriter, r *http.Request, runID string, rs *runState) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Workflow-Run-ID", runID)
	sse := streaming.NewSSEWriter(w)

	rs.mu.Lock()
	snapshot := append([]runEvent(nil), rs.events...)
	done := rs.done
	watch := make(chan runEvent, 16)
	if !done {
		rs.watchers[watch] = struct{}{}
	}
	rs.mu.Unlock()
	startIndex := parseStartIndex(r, len(snapshot))
	if startIndex > 0 && startIndex < len(snapshot) {
		snapshot = snapshot[startIndex:]
	} else if startIndex >= len(snapshot) {
		snapshot = nil
	}

	for _, e := range snapshot {
		_ = sse.WriteNamedEvent(e.Event, e.Data)
		flusher.Flush()
	}
	if done {
		_ = sse.WriteDone()
		flusher.Flush()
		return
	}
	for {
		select {
		case <-r.Context().Done():
			rs.mu.Lock()
			delete(rs.watchers, watch)
			rs.mu.Unlock()
			return
		case e, ok := <-watch:
			if !ok {
				_ = sse.WriteDone()
				flusher.Flush()
				return
			}
			_ = sse.WriteNamedEvent(e.Event, e.Data)
			flusher.Flush()
		}
	}
}

func parseStartIndex(r *http.Request, total int) int {
	raw := r.URL.Query().Get("startIndex")
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	// Negative values are tail-relative (TS transport behavior).
	if n < 0 {
		n = total + n
		if n < 0 {
			n = 0
		}
	}
	return n
}

// ServeHTTP starts a new run stream and emits simple lifecycle events.
func (t *WorkflowChatTransport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		runID := r.URL.Query().Get("runId")
		if runID != "" {
			t.Resume(runID).ServeHTTP(w, r)
			return
		}
	}
	runID := uuid.NewString()
	rs := t.getOrCreate(runID)
	go func() {
		t.appendEvent(runID, "start", fmt.Sprintf(`{"runId":%q}`, runID))
		time.Sleep(5 * time.Millisecond)
		payload, _ := json.Marshal(map[string]interface{}{"status": "running"})
		t.appendEvent(runID, "progress", string(payload))
		time.Sleep(5 * time.Millisecond)
		t.appendEvent(runID, "finish", `{"status":"done"}`)
		t.finish(runID)
	}()
	t.serveRun(w, r, runID, rs)
}
