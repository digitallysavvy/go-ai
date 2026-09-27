package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// WorkflowChatTransport is the Go port of TS's
// `packages/workflow/src/workflow-chat-transport.ts`: a client-side
// ai.ChatTransport that POSTs a chat's message history to a workflow chat
// endpoint, reads the response as a stream of ai.UIMessageChunk, and
// automatically reconnects (a GET to "<api>/<runId>/stream?startIndex=N")
// when the initial response ends without a "finish" chunk — e.g. a network
// drop or a platform function timeout mid-stream.
//
// It also repairs UI message stream part framing (normalizeUIMessageStreamParts,
// 4a9f4d5) and, on a negative-startIndex resume, drops orphaned deltas/ends
// whose "*-start" fell outside the resumed window (148babc), matching the TS
// transport's defenses against a durable, possibly-replayed shared stream.
type WorkflowChatTransport struct {
	api                             string
	httpClient                      *http.Client
	onChatSendMessage               func(*http.Response, ai.ChatTransportSendMessagesRequest) error
	onChatEnd                       func(WorkflowChatTransportEndEvent) error
	maxConsecutiveErrors            int
	initialStartIndex               int
	prepareSendMessagesRequest      func(ai.ChatTransportSendMessagesRequest) (PreparedChatRequest, error)
	prepareReconnectToStreamRequest func(WorkflowChatTransportReconnectContext) (PreparedChatRequest, error)
}

// WorkflowChatTransport implements ai.ChatTransport, matching TS's
// `class WorkflowChatTransport<UI_MESSAGE> implements ChatTransport<UI_MESSAGE>`.
var _ ai.ChatTransport = (*WorkflowChatTransport)(nil)

// WorkflowChatTransportOptions configures a WorkflowChatTransport.
type WorkflowChatTransportOptions struct {
	// API is the chat endpoint. Defaults to "/api/chat".
	API string
	// HTTPClient is used for both the initial POST and any reconnect GETs.
	// Defaults to http.DefaultClient.
	HTTPClient *http.Client
	// OnChatSendMessage is invoked after the initial POST completes, useful
	// for inspecting response headers or tracking chat history.
	OnChatSendMessage func(resp *http.Response, req ai.ChatTransportSendMessagesRequest) error
	// OnChatEnd is invoked once, after a "finish" chunk is observed.
	OnChatEnd func(WorkflowChatTransportEndEvent) error
	// MaxConsecutiveErrors bounds reconnect attempts. Defaults to 3.
	MaxConsecutiveErrors int
	// InitialStartIndex is the default startIndex used by ReconnectToStream
	// when it is called directly (not as part of a SendMessages recovery).
	// Negative values are tail-relative (e.g. -10 reads the last 10 chunks),
	// useful for resuming a chat UI after a page refresh without replaying
	// the whole conversation. Defaults to 0 (replay from the beginning).
	InitialStartIndex int
	// PrepareSendMessagesRequest customizes the API endpoint, body, and
	// headers used for the initial POST.
	PrepareSendMessagesRequest func(ai.ChatTransportSendMessagesRequest) (PreparedChatRequest, error)
	// PrepareReconnectToStreamRequest customizes the API endpoint and headers
	// used for reconnect GETs.
	PrepareReconnectToStreamRequest func(WorkflowChatTransportReconnectContext) (PreparedChatRequest, error)
}

// WorkflowChatTransportEndEvent is passed to WorkflowChatTransportOptions.OnChatEnd,
// mirroring TS's `onChatEnd?: ({chatId, chunkIndex}) => void`.
type WorkflowChatTransportEndEvent struct {
	ChatID     string
	ChunkIndex int
}

// WorkflowChatTransportReconnectContext is passed to
// PrepareReconnectToStreamRequest. API is the default reconnect URL before
// preparation ("<api>/<runId-or-chatId>/stream").
type WorkflowChatTransportReconnectContext struct {
	ChatID string
	API    string
}

// NewWorkflowChatTransport creates a client-side WorkflowChatTransport.
func NewWorkflowChatTransport(opts WorkflowChatTransportOptions) *WorkflowChatTransport {
	api := opts.API
	if api == "" {
		api = "/api/chat"
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	maxErrs := opts.MaxConsecutiveErrors
	if maxErrs <= 0 {
		maxErrs = 3
	}
	return &WorkflowChatTransport{
		api:                             api,
		httpClient:                      client,
		onChatSendMessage:               opts.OnChatSendMessage,
		onChatEnd:                       opts.OnChatEnd,
		maxConsecutiveErrors:            maxErrs,
		initialStartIndex:               opts.InitialStartIndex,
		prepareSendMessagesRequest:      opts.PrepareSendMessagesRequest,
		prepareReconnectToStreamRequest: opts.PrepareReconnectToStreamRequest,
	}
}

// SendMessages implements ai.ChatTransport.SendMessages: it POSTs the message
// history to the configured endpoint and streams the response as UI message
// chunks, transparently reconnecting if the response ends without "finish".
func (t *WorkflowChatTransport) SendMessages(ctx context.Context, req ai.ChatTransportSendMessagesRequest) (<-chan ai.UIMessageChunk, <-chan error) {
	out := make(chan ai.UIMessageChunk)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		t.sendMessages(ctx, req, out, errs)
	}()
	return out, errs
}

// ReconnectToStream implements ai.ChatTransport.ReconnectToStream: it resumes
// an in-progress or previously-interrupted stream for req.ChatID, treating
// ChatID as the workflow run id (the same simplification TS's transport
// makes when reconnectToStream is called directly rather than via
// SendMessages's own recovery path).
func (t *WorkflowChatTransport) ReconnectToStream(ctx context.Context, req ai.ChatTransportReconnectToStreamRequest) (<-chan ai.UIMessageChunk, <-chan error) {
	out := make(chan ai.UIMessageChunk)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		normalizer := newUIStreamNormalizer()
		t.reconnectLoop(ctx, req.ChatID, req.ChatID, 0, true, normalizer, out, errs)
	}()
	return out, errs
}

func (t *WorkflowChatTransport) sendMessages(ctx context.Context, req ai.ChatTransportSendMessagesRequest, out chan<- ai.UIMessageChunk, errs chan<- error) {
	body := map[string]interface{}{"messages": req.Messages}
	if req.ChatID != "" {
		body["chatId"] = req.ChatID
	}
	if req.MessageID != "" {
		body["messageId"] = req.MessageID
	}
	if req.Trigger != "" {
		body["trigger"] = req.Trigger
	}
	api := t.api
	headers := map[string]string{}
	if t.prepareSendMessagesRequest != nil {
		prepared, err := t.prepareSendMessagesRequest(req)
		if err != nil {
			errs <- err
			return
		}
		if prepared.API != "" {
			api = prepared.API
		}
		if prepared.Body != nil {
			body = prepared.Body
		}
		for k, v := range prepared.Headers {
			headers[k] = v
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		errs <- err
		return
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(raw))
	if err != nil {
		errs <- err
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		errs <- err
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Body == nil {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		errs <- fmt.Errorf("workflow chat transport: failed to fetch chat: %d %s", resp.StatusCode, strings.TrimSpace(string(payload)))
		return
	}

	workflowRunID := resp.Header.Get("x-workflow-run-id")
	if workflowRunID == "" {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		errs <- fmt.Errorf(`workflow chat transport: workflow run ID not found in "x-workflow-run-id" response header`)
		return
	}

	if t.onChatSendMessage != nil {
		if err := t.onChatSendMessage(resp, req); err != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			errs <- err
			return
		}
	}

	normalizer := newUIStreamNormalizer()
	chunkIndex, gotFinish := t.pumpChunkStream(resp.Body, normalizer, nil, out)
	resp.Body.Close()

	if gotFinish {
		t.finishChat(req.ChatID, chunkIndex, errs)
		return
	}
	// The initial POST stream ended without a finish chunk (dropped
	// connection or platform function timeout) — reconnect to resume.
	t.reconnectLoop(ctx, req.ChatID, workflowRunID, chunkIndex, false, normalizer, out, errs)
}

// reconnectLoop mirrors TS's reconnectToStreamIterator: it GETs the run's
// stream, resuming from the running chunkIndex (or an explicit startIndex on
// the first request), retrying up to maxConsecutiveErrors times on read
// failures, until a "finish" chunk is observed.
func (t *WorkflowChatTransport) reconnectLoop(ctx context.Context, chatID, runID string, initialChunkIndex int, useConfiguredStartIndex bool, normalizer *uiStreamNormalizer, out chan<- ai.UIMessageChunk, errs chan<- error) {
	chunkIndex := initialChunkIndex
	explicitStartIndex := t.initialStartIndex
	useExplicitStartIndex := useConfiguredStartIndex && explicitStartIndex != 0

	base := fmt.Sprintf("%s/%s/stream", strings.TrimRight(t.api, "/"), url.PathEscape(runID))
	reconnectCtx := WorkflowChatTransportReconnectContext{ChatID: chatID, API: base}
	headers := map[string]string{}
	if t.prepareReconnectToStreamRequest != nil {
		prepared, err := t.prepareReconnectToStreamRequest(reconnectCtx)
		if err != nil {
			errs <- err
			return
		}
		if prepared.API != "" {
			base = prepared.API
		}
		for k, v := range prepared.Headers {
			headers[k] = v
		}
	}

	var orphans *orphanFilter
	if useExplicitStartIndex && explicitStartIndex < 0 {
		orphans = newOrphanFilter()
	}

	gotFinish := false
	consecutiveErrors := 0
	replayFromStart := false

	for !gotFinish {
		startIndex := chunkIndex
		switch {
		case useExplicitStartIndex:
			startIndex = explicitStartIndex
		case replayFromStart:
			startIndex = 0
		}

		reqURL := base + "?startIndex=" + strconv.Itoa(startIndex)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			errs <- err
			return
		}
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}
		resp, err := t.httpClient.Do(httpReq)
		if err != nil {
			errs <- err
			return
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Body == nil {
			payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			errs <- fmt.Errorf("workflow chat transport: failed to fetch chat: %d %s", resp.StatusCode, strings.TrimSpace(string(payload)))
			return
		}

		if useExplicitStartIndex && explicitStartIndex > 0 {
			chunkIndex = explicitStartIndex
		} else if useExplicitStartIndex && explicitStartIndex < 0 {
			if tailHeader := resp.Header.Get("x-workflow-stream-tail-index"); tailHeader != "" {
				if tailIndex, err := strconv.Atoi(tailHeader); err == nil {
					chunkIndex = tailIndex + 1 + explicitStartIndex
					if chunkIndex < 0 {
						chunkIndex = 0
					}
				} else {
					log.Printf("[WorkflowChatTransport] Negative initialStartIndex is configured (%d) but the reconnection endpoint returned an unparseable \"x-workflow-stream-tail-index\" header. Retries will replay the stream from the beginning.", explicitStartIndex)
					replayFromStart = true
				}
			} else {
				log.Printf("[WorkflowChatTransport] Negative initialStartIndex is configured (%d) but the reconnection endpoint did not return an \"x-workflow-stream-tail-index\" header. Retries will replay the stream from the beginning.", explicitStartIndex)
				replayFromStart = true
			}
		}
		useExplicitStartIndex = false

		read, finished := t.pumpChunkStream(resp.Body, normalizer, orphans, out)
		resp.Body.Close()
		chunkIndex += read
		if finished {
			gotFinish = true
			consecutiveErrors = 0
			break
		}
		consecutiveErrors++
		if consecutiveErrors >= t.maxConsecutiveErrors {
			errs <- fmt.Errorf("workflow chat transport: failed to reconnect after %d consecutive errors", t.maxConsecutiveErrors)
			return
		}
	}

	t.finishChat(chatID, chunkIndex, errs)
}

func (t *WorkflowChatTransport) finishChat(chatID string, chunkIndex int, errs chan<- error) {
	if t.onChatEnd == nil {
		return
	}
	if err := t.onChatEnd(WorkflowChatTransportEndEvent{ChatID: chatID, ChunkIndex: chunkIndex}); err != nil {
		errs <- err
	}
}

// pumpChunkStream reads one SSE response body as a stream of UI message
// chunks, applying framing repair (and the orphan filter, when non-nil)
// before sending each chunk downstream. It returns the number of raw chunks
// read from the wire and whether a "finish" chunk was observed. A read error
// mid-stream (rather than a clean EOF) simply stops the pump — the caller
// treats an incomplete stream the same as a dropped connection and
// reconnects, matching TS's `catch { console.error(...) }` fallthrough.
func (t *WorkflowChatTransport) pumpChunkStream(body io.Reader, normalizer *uiStreamNormalizer, orphans *orphanFilter, out chan<- ai.UIMessageChunk) (read int, gotFinish bool) {
	parser := streaming.NewSSEParser(body)
	for {
		event, err := parser.Next()
		if err != nil {
			return read, gotFinish
		}
		data := strings.TrimSpace(event.Data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk ai.UIMessageChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// TS's parseJsonEventStream runs safeParseJSON with schema
			// validation and, on failure, `throw chunk.error` — caught by the
			// enclosing try/catch, which logs and falls through exactly as if
			// the stream had ended early (workflow-chat-transport.ts:340-353).
			// A malformed frame is therefore fatal for this attempt, not
			// silently skipped: stop the pump so the caller's reconnect path
			// runs, matching TS instead of dropping one frame and continuing
			// on the same (possibly desynced) connection.
			return read, gotFinish
		}
		read++
		// The orphan filter runs on the raw chunk, before framing repair —
		// matching TS, where reconnectToStreamIterator's orphan filter
		// operates inside the raw parseJsonEventStream loop and
		// normalizeUIMessageStreamParts wraps that already-filtered
		// iterator. Filtering after repair would let a synthesized "*-start"
		// mask the very orphan it was meant to catch.
		if orphans == nil || !orphans.shouldDrop(chunk) {
			for _, repaired := range normalizer.normalize(chunk) {
				out <- repaired
			}
		}
		if stringField(chunk, "type") == "finish" {
			gotFinish = true
		}
	}
}

func stringField(chunk ai.UIMessageChunk, key string) string {
	if chunk == nil {
		return ""
	}
	s, _ := chunk[key].(string)
	return s
}

// ---------------------------------------------------------------------------
// UI message stream framing repair (WORKFLOW-TRANSPORT, 4a9f4d5)
// ---------------------------------------------------------------------------

// partFrameState tracks, for one part family (text or reasoning), which part
// ids are open or have ended since the latest step boundary. Mirrors TS's
// PartFrameState (normalize-ui-message-stream.ts).
type partFrameState struct {
	open  map[string]bool
	ended map[string]bool
}

func newPartFrameState() *partFrameState {
	return &partFrameState{open: map[string]bool{}, ended: map[string]bool{}}
}

// uiStreamNormalizer repairs the framing of a UI message chunk stream so it
// is always well-formed for the AI SDK's UI message stream reducer, mirroring
// TS's normalizeUIMessageStreamParts. It keeps open parts active across
// finish-step, synthesizes a missing "*-start" for an orphaned delta/end, and
// drops a re-delivered start/delta/end for a part already open or ended
// since the latest step boundary (reconnect/replay overlap). Tool parts are
// deliberately left untouched, matching the TS scope note: tool-call ids are
// unique and the consumer does not reset its tool-call map on finish-step, so
// the step-boundary id-reuse orphaning that makes text/reasoning fragile does
// not apply to them.
type uiStreamNormalizer struct {
	text      *partFrameState
	reasoning *partFrameState
}

func newUIStreamNormalizer() *uiStreamNormalizer {
	return &uiStreamNormalizer{text: newPartFrameState(), reasoning: newPartFrameState()}
}

// normalize returns the chunks the consumer should see for one incoming
// chunk: usually the chunk itself, sometimes a synthesized start followed by
// the chunk, and sometimes nothing (a dropped duplicate).
func (n *uiStreamNormalizer) normalize(chunk ai.UIMessageChunk) []ai.UIMessageChunk {
	switch stringField(chunk, "type") {
	case "reset-step":
		// A retried model-call step starts a new frame. Forget parts from
		// the invalidated attempt so reused ids are framed normally.
		n.text = newPartFrameState()
		n.reasoning = newPartFrameState()
		return []ai.UIMessageChunk{chunk}
	case "finish-step":
		// Open parts are closed only by explicit end chunks — a finish-step
		// can come from another interleaved execution while a part is still
		// open. Ended ids may be reused by the next step.
		n.text.ended = map[string]bool{}
		n.reasoning.ended = map[string]bool{}
		return []ai.UIMessageChunk{chunk}
	case "text-start":
		return repairPart("start", stringField(chunk, "id"), chunk, n.text, "text-start")
	case "text-delta":
		return repairPart("delta", stringField(chunk, "id"), chunk, n.text, "text-start")
	case "text-end":
		return repairPart("end", stringField(chunk, "id"), chunk, n.text, "text-start")
	case "reasoning-start":
		return repairPart("start", stringField(chunk, "id"), chunk, n.reasoning, "reasoning-start")
	case "reasoning-delta":
		return repairPart("delta", stringField(chunk, "id"), chunk, n.reasoning, "reasoning-start")
	case "reasoning-end":
		return repairPart("end", stringField(chunk, "id"), chunk, n.reasoning, "reasoning-start")
	default:
		return []ai.UIMessageChunk{chunk}
	}
}

func repairPart(kind, id string, chunk ai.UIMessageChunk, state *partFrameState, startType string) []ai.UIMessageChunk {
	if kind == "start" {
		// Drop a duplicate/replayed start for a part that is still open or
		// has already ended since the latest step boundary.
		if state.open[id] || state.ended[id] {
			return nil
		}
		state.open[id] = true
		return []ai.UIMessageChunk{chunk}
	}
	// delta / end: drop a re-delivered chunk for an already-ended part.
	if state.ended[id] {
		return nil
	}
	var out []ai.UIMessageChunk
	// Synthesize the missing start for an orphaned delta/end.
	if !state.open[id] {
		state.open[id] = true
		out = append(out, ai.UIMessageChunk{"type": startType, "id": id})
	}
	if kind == "end" {
		delete(state.open, id)
		state.ended[id] = true
	}
	out = append(out, chunk)
	return out
}

// ---------------------------------------------------------------------------
// Orphan chunk dropping on a mid-part resume (WORKFLOW-TRANSPORT, 148babc)
// ---------------------------------------------------------------------------

// orphanFilter tracks `*-start` chunks the client has accepted so a resume
// with a negative startIndex can drop deltas/ends and tool output/approval
// chunks that refer to a part whose start chunk was emitted before the
// resume cursor, mirroring TS's createOrphanFilter. It is a best-effort
// safety net — it preserves only the parts the resumed window includes a
// "*-start" for.
type orphanFilter struct {
	seenStartedIDs         map[string]bool
	seenStartedToolCallIDs map[string]bool
	warnedOnce             bool
}

func newOrphanFilter() *orphanFilter {
	return &orphanFilter{seenStartedIDs: map[string]bool{}, seenStartedToolCallIDs: map[string]bool{}}
}

func (f *orphanFilter) shouldDrop(chunk ai.UIMessageChunk) bool {
	switch stringField(chunk, "type") {
	case "reset-step":
		f.seenStartedIDs = map[string]bool{}
		f.seenStartedToolCallIDs = map[string]bool{}
		return false
	case "text-start", "reasoning-start":
		f.seenStartedIDs[stringField(chunk, "id")] = true
		return false
	case "tool-input-start", "tool-input-available", "tool-input-error":
		// tool-input-available/tool-input-error are self-contained: the UI
		// stream reducer creates the tool part from them directly (a
		// non-streamed tool call is emitted as a bare tool-input-available),
		// so they must never be dropped. They also carry the full input, so
		// they recover a tool call whose tool-input-start fell outside the
		// resumed window.
		f.seenStartedToolCallIDs[stringField(chunk, "toolCallId")] = true
		return false
	case "text-delta", "text-end", "reasoning-delta", "reasoning-end":
		id := stringField(chunk, "id")
		if f.seenStartedIDs[id] {
			return false
		}
		f.warn(stringField(chunk, "type"), id)
		return true
	case "tool-input-delta", "tool-approval-request", "tool-output-available", "tool-output-error", "tool-output-denied":
		id := stringField(chunk, "toolCallId")
		if f.seenStartedToolCallIDs[id] {
			return false
		}
		f.warn(stringField(chunk, "type"), id)
		return true
	default:
		return false
	}
}

func (f *orphanFilter) warn(orphanKind, orphanRef string) {
	if f.warnedOnce {
		return
	}
	f.warnedOnce = true
	log.Printf("[WorkflowChatTransport] Dropping orphan UI chunk (%s for id %q) on resume — "+
		"the resume position landed mid-part. The dropped chunk(s) reference a part whose "+
		"start chunk wasn't in the resumed window. To preserve the full message, configure "+
		"your stream endpoint to rewind to a step boundary before returning the readable.",
		orphanKind, orphanRef)
}
