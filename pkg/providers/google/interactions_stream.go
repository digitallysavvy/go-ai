package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

type interactionsStream struct {
	currentEventData string // raw JSON of the event being processed (error data)
	ctx              context.Context
	client           *internalhttp.Client
	reader           io.ReadCloser
	parser           *streaming.SSEParser
	interactionID    string
	headers          map[string]string
	responseHeaders  map[string]string
	warnings         []types.Warning
	timeoutMs        int
	err              error
	buffer           []*provider.StreamChunk
	open             map[int]*interactionOpenBlock
	startEmitted     bool
	completed        bool
	finishStatus     string
	usage            *interactionsUsage
	serviceTier      string
	hasFunction      bool
	lastEventID      string
	resumable        bool
	finished         bool

	// attempt and receivedEvent mirror streamGoogleInteractionEvents'
	// consecutive-failure accounting (stream-google-interactions.ts):
	// attempt counts failed (re)connects, receivedEvent whether the current
	// connection produced any event.
	attempt       int
	receivedEvent bool

	// includeRawChunks emits a ChunkTypeRaw chunk with the parsed SSE payload
	// ahead of the chunks each event produces (TS includeRawChunks).
	includeRawChunks bool

	// emittedSources de-duplicates sources across the whole stream (TS
	// emittedSourceKeys): a citation that reappears in later annotation
	// deltas or tool results is surfaced once.
	emittedSources map[string]bool

	// generateID creates source ids (TS config.generateId()); nil falls back
	// to a random id.
	generateID func() string

	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

func (s *interactionsStream) newID() string {
	if s.generateID != nil {
		return s.generateID()
	}
	return streaming.GenerateID()
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *interactionsStream) RequestBody() interface{} { return s.requestBody }

type interactionOpenBlock struct {
	kind         string
	id           string
	blockType    string
	toolCallID   string
	toolName     string
	args         map[string]interface{}
	argsRaw      json.RawMessage
	argsString   string
	signature    string
	data         string
	mimeType     string
	uri          string
	result       interface{}
	callID       string
	isError      bool
	startEmitted bool
}

func newInteractionsStream(ctx context.Context, client *internalhttp.Client, interactionID string, headers map[string]string, warnings []types.Warning, responseHeaders map[string]string, timeoutMs int) (provider.TextStream, error) {
	s := &interactionsStream{
		ctx:             ctx,
		client:          client,
		interactionID:   interactionID,
		headers:         headers,
		responseHeaders: responseHeaders,
		warnings:        warnings,
		timeoutMs:       timeoutMs,
		open:            map[int]*interactionOpenBlock{},
		resumable:       true,
		serviceTier:     headerServiceTier(responseHeaders),
	}
	if err := s.openWithRetry(); err != nil {
		return nil, err
	}
	return s, nil
}

func newInteractionsEventStream(ctx context.Context, client *internalhttp.Client, reader io.ReadCloser, interactionID string, headers map[string]string, warnings []types.Warning, responseHeaders map[string]string, timeoutMs int) provider.TextStream {
	return &interactionsStream{
		ctx:             ctx,
		client:          client,
		reader:          reader,
		parser:          streaming.NewSSEParser(reader),
		interactionID:   interactionID,
		headers:         headers,
		responseHeaders: responseHeaders,
		warnings:        warnings,
		timeoutMs:       timeoutMs,
		open:            map[int]*interactionOpenBlock{},
		serviceTier:     headerServiceTier(responseHeaders),
	}
}

// headerServiceTier reads the applied service tier from the
// x-gemini-service-tier response header (TS headerServiceTier). The lookup is
// case-insensitive because header maps carry canonical keys.
func headerServiceTier(headers map[string]string) string {
	for k, v := range headers {
		if strings.EqualFold(k, "x-gemini-service-tier") {
			return v
		}
	}
	return ""
}

// parseInteractionsCreated parses an Interactions `created` timestamp; the
// zero time means absent or invalid (TS drops an unparsable date).
func parseInteractionsCreated(created string) time.Time {
	if created == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return time.Time{}
	}
	return t
}

func newSynthesizedInteractionsStream(model *InteractionsLanguageModel, response interactionsResponse, warnings []types.Warning, headers http.Header, includeRawChunks bool) provider.TextStream {
	content, _, hasFunctionCall := model.parseOutputs(response.Steps, normalizedInteractionID(response.ID))
	chunks := make([]*provider.StreamChunk, 0, len(content)+4)
	// TS always enqueues stream-start first, even without warnings.
	if warnings == nil {
		warnings = []types.Warning{}
	}
	chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: warnings})
	// TS replays response-metadata and (optionally) the raw response before
	// the content parts.
	chunks = append(chunks, &provider.StreamChunk{
		Type: provider.ChunkTypeResponseMetadata,
		ResponseMetadata: &provider.ResponseMetadata{
			ID:        normalizedInteractionID(response.ID),
			Timestamp: parseInteractionsCreated(response.Created),
			ModelID:   response.Model,
			Headers:   providerutils.ExtractHeaders(headers),
		},
	})
	if includeRawChunks {
		chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: response})
	}
	// TS nextBlockId: `${interactionId ?? 'agent'}:${counter++}`, counting
	// only the text and reasoning blocks.
	blockCounter := 0
	nextBlockID := func() string {
		id := fmt.Sprintf("%s:%d", firstNonEmpty(response.ID, "agent"), blockCounter)
		blockCounter++
		return id
	}
	for _, part := range content {
		switch p := part.(type) {
		case types.TextContent:
			id := nextBlockID()
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id})
			if p.Text != "" {
				chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: p.Text})
			}
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id, ProviderMetadata: p.ProviderMetadata})
		case types.ReasoningContent:
			id := nextBlockID()
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: id})
			if p.Text != "" {
				chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: id, Reasoning: p.Text})
			}
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: id, ProviderMetadata: p.ProviderMetadata})
		case types.GeneratedFileContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &p, ProviderMetadata: p.ProviderMetadata})
		case types.SourceContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &p, ProviderMetadata: p.ProviderMetadata})
		case types.ToolResultContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: p.ToolCallID, ToolName: p.ToolName, Result: p.Result}})
		case types.ToolCallContent:
			// TS streams the tool input (start, one delta with the whole
			// input, end) before the tool call itself.
			chunks = append(chunks,
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: p.ToolCallID, ToolName: p.ToolName, ProviderExecuted: p.ProviderExecuted}},
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ToolCall: &types.ToolCall{ID: p.ToolCallID, ToolName: p.ToolName}, Text: p.Input},
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: p.ToolCallID}},
				&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: p.ToolCallID, ToolName: p.ToolName, Arguments: p.Arguments, RawArguments: p.Input, ProviderExecuted: p.ProviderExecuted, ProviderMetadata: providerMetaMap(p.ThoughtSignature, normalizedInteractionID(response.ID)), ThoughtSignature: p.ThoughtSignature}, ProviderMetadata: p.ProviderMetadata},
			)
		}
	}
	// The synthesized finish carries no outputTokensByModality (TS only reads
	// it from the streamed interaction.completed usage).
	chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: mapInteractionsFinishReason(response.Status, hasFunctionCall), RawFinishReason: response.Status, Usage: usagePtr(convertInteractionsUsage(response.Usage)), ProviderMetadata: finishMetadata(response.ID, firstNonEmpty(response.ServiceTier, headerServiceTier(providerutils.ExtractHeaders(headers))), nil)})
	return &sliceTextStream{chunks: chunks}
}

// Next returns the next chunk in the stream.
//
// The two "buffer now has entries" exits below used to `return s.Next()`
// to re-enter the function and drain the buffer from the top. Go does not
// eliminate that tail call, so a long run of SSE events that each add to
// the buffer without the caller draining it in between (e.g. a resumed
// stream replaying many events before the next real chunk) could grow the
// goroutine stack without bound and crash the process with an
// unrecoverable stack overflow. `goto restart` replaces the recursive
// call: it re-enters at the same point (the buffer/err/startEmitted
// checks) without adding a stack frame. startEmitted is already true and
// err is nil at both call sites, so this is exactly equivalent to the
// prior recursive call.
func (s *interactionsStream) Next() (*provider.StreamChunk, error) {
restart:
	if len(s.buffer) > 0 {
		chunk := s.buffer[0]
		s.buffer = s.buffer[1:]
		return chunk, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	if !s.startEmitted {
		s.startEmitted = true
		// TS always enqueues stream-start first, even without warnings.
		warnings := s.warnings
		if warnings == nil {
			warnings = []types.Warning{}
		}
		return &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: warnings}, nil
	}
	for {
		var event *streaming.SSEEvent
		var err error
		if s.completed && s.resumable {
			// TS streamGoogleInteractionEvents ends the stream after the
			// terminal interaction.completed / error event.
			err = io.EOF
		} else {
			event, err = s.parser.Next()
		}
		if err == io.EOF {
			if s.resumable && !s.completed && s.interactionID != "" {
				if err := s.reconnectAfterEOF(); err != nil {
					s.err = err
					return nil, err
				}
				continue
			}
			if !s.finished {
				// TS flush: the finish part only; blocks that never saw a
				// step.stop stay open.
				s.appendFinish()
				if len(s.buffer) > 0 {
					goto restart
				}
			}
			if s.completed || !s.resumable || s.interactionID == "" {
				s.err = io.EOF
				return nil, io.EOF
			}
		}
		if err != nil {
			// TS reconnects with last_event_id after a mid-stream read
			// failure, up to maxRetries consecutive failures; an abort is
			// surfaced as is.
			if s.resumable && !s.completed && s.interactionID != "" && s.ctx.Err() == nil {
				s.attempt++
				if s.attempt < interactionsStreamMaxRetries {
					if rerr := s.sleepBeforeRetry(); rerr != nil {
						s.err = rerr
						return nil, rerr
					}
					if rerr := s.reopenWithRetry(); rerr != nil {
						s.err = rerr
						return nil, rerr
					}
					continue
				}
			}
			s.err = err
			return nil, err
		}
		s.receivedEvent = true
		if streaming.IsStreamDone(event) {
			continue
		}
		if s.includeRawChunks {
			var raw interface{}
			if json.Unmarshal([]byte(event.Data), &raw) != nil {
				raw = event.Data
			}
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw})
		}
		var payload interactionsEvent
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			// TS: an unparsable chunk marks the interaction failed, is
			// surfaced as an error part and reading continues.
			var chunkErr error
			if !json.Valid([]byte(event.Data)) {
				var probe interface{}
				chunkErr = providererrors.NewJSONParseError(event.Data, json.Unmarshal([]byte(event.Data), &probe))
			} else {
				chunkErr = providererrors.NewValidationErrorWithContext(event.Data, fmt.Sprintf("failed to parse interactions stream event: %v", err), err, &providererrors.ValidationContext{})
			}
			s.finishStatus = InteractionStatusFailed
			s.buffer = append(s.buffer, &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: chunkErr.Error(),
				Err:  chunkErr,
			})
			goto restart
		}
		// TS tracks the JSON event_id (not the SSE id) for last_event_id.
		if payload.EventID != "" {
			s.lastEventID = payload.EventID
		}
		s.currentEventData = event.Data
		s.processEvent(payload)
		if len(s.buffer) > 0 {
			goto restart
		}
	}
}

func (s *interactionsStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *interactionsStream) Close() error {
	if s.resumable && !s.completed && s.interactionID != "" {
		_ = (&InteractionsLanguageModel{cfg: InteractionsConfig{Client: s.client}}).cancelInteraction(context.Background(), s.interactionID, s.headers)
	}
	if s.reader == nil {
		return nil
	}
	return s.reader.Close()
}

func (s *interactionsStream) openGETStream() error {
	query := map[string]string{"stream": "true"}
	if s.lastEventID != "" {
		query["last_event_id"] = s.lastEventID
	}
	resp, err := s.client.DoStream(s.ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/interactions/" + url.PathEscape(s.interactionID),
		Query:   query,
		Headers: internalhttp.MergeHeaders(s.headers, map[string]string{"Accept": "text/event-stream"}),
	})
	if err != nil {
		return err
	}
	s.reader = resp.Body
	s.parser = streaming.NewSSEParser(resp.Body)
	return nil
}

// Reconnect policy of streamGoogleInteractionEvents (stream-google-interactions.ts):
// at most interactionsStreamMaxRetries consecutive failures, retryDelay*attempt
// between them.
const interactionsStreamMaxRetries = 3

// interactionsStreamRetryDelay is the base delay between reconnect attempts
// (a variable so tests can shorten it).
var interactionsStreamRetryDelay = 500 * time.Millisecond

// cancelOnAbort fires the best-effort POST /interactions/{id}/cancel TS sends
// when the caller aborts before the interaction completed.
func (s *interactionsStream) cancelOnAbort() {
	_ = (&InteractionsLanguageModel{cfg: InteractionsConfig{Client: s.client}}).cancelInteraction(context.Background(), s.interactionID, s.headers)
}

// sleepBeforeRetry waits retryDelay*attempt, returning ctx.Err() (after the
// cancel call) when the caller aborts meanwhile.
func (s *interactionsStream) sleepBeforeRetry() error {
	timer := time.NewTimer(interactionsStreamRetryDelay * time.Duration(s.attempt))
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		s.cancelOnAbort()
		return s.ctx.Err()
	case <-timer.C:
		return nil
	}
}

// reconnectAfterEOF handles a connection that ended before the terminal
// event: an attempt that produced no event at all counts as a failure (an
// empty/misbehaving server must not loop forever), otherwise the failure
// counter resets and the stream resumes from last_event_id.
func (s *interactionsStream) reconnectAfterEOF() error {
	if !s.receivedEvent {
		s.attempt++
		if s.attempt >= interactionsStreamMaxRetries {
			return fmt.Errorf("google.interactions: SSE stream closed without producing any events.") //nolint:staticcheck // matches TS SDK exact error text
		}
		if err := s.sleepBeforeRetry(); err != nil {
			return err
		}
	} else {
		s.attempt = 0
	}
	return s.reopenWithRetry()
}

// openWithRetry opens the GET stream, retrying failed connects like TS
// (attempt++, give up at maxRetries, retryDelay*attempt between tries).
func (s *interactionsStream) openWithRetry() error {
	for {
		if s.ctx.Err() != nil {
			s.cancelOnAbort()
			return s.ctx.Err()
		}
		err := s.openGETStream()
		if err == nil {
			s.receivedEvent = false
			return nil
		}
		if s.ctx.Err() != nil {
			s.cancelOnAbort()
			return err
		}
		s.attempt++
		if s.attempt >= interactionsStreamMaxRetries {
			return err
		}
		if err := s.sleepBeforeRetry(); err != nil {
			return err
		}
	}
}

// reopenWithRetry is openWithRetry for a reconnect: the previous reader is
// closed first.
func (s *interactionsStream) reopenWithRetry() error {
	if s.reader != nil {
		_ = s.reader.Close()
		s.reader = nil
	}
	return s.openWithRetry()
}

func (s *interactionsStream) processEvent(event interactionsEvent) {
	switch event.EventType {
	case "interaction.created":
		if event.Interaction != nil {
			if event.Interaction.ID != "" {
				s.interactionID = event.Interaction.ID
			}
			if event.Interaction.ServiceTier != "" {
				s.serviceTier = event.Interaction.ServiceTier
			}
			s.buffer = append(s.buffer, &provider.StreamChunk{
				Type: provider.ChunkTypeResponseMetadata,
				ResponseMetadata: &provider.ResponseMetadata{
					ID:        normalizedInteractionID(event.Interaction.ID),
					Timestamp: parseInteractionsCreated(event.Interaction.Created),
					ModelID:   event.Interaction.Model,
					Headers:   s.responseHeaders,
				},
			})
		}
	case "step.start":
		if event.Index == nil || event.Step == nil {
			return
		}
		idx := *event.Index
		block := event.Step
		id := fmt.Sprintf("%s:%d", firstNonEmpty(s.interactionID, "interaction"), idx)
		open := &interactionOpenBlock{kind: block.Type, id: id, blockType: block.Type, toolCallID: firstNonEmpty(block.ID, id), toolName: block.Name, signature: block.Signature, data: block.Data, mimeType: block.MimeType, uri: block.URI, callID: firstNonEmpty(block.CallID, id), result: block.Result, isError: block.IsError != nil && *block.IsError}
		s.open[idx] = open
		switch block.Type {
		case "thought":
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: id})
			for _, item := range block.Summary {
				if item.Type == "text" && item.Text != "" {
					s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: id, Reasoning: item.Text})
				}
			}
		case "model_output":
			// TS: step.start often carries only the type; content[0], when
			// present, promotes the block eagerly (text -> text-start plus
			// its annotation sources, image -> an image block emitted at
			// step.stop). Anything else stays pending until a delta says.
			open.kind = "pending_model_output"
			var inner []interactionsContentBlock
			if len(block.ContentRaw) > 0 {
				_ = json.Unmarshal(block.ContentRaw, &inner)
			}
			if len(inner) > 0 {
				switch initial := inner[0]; initial.Type {
				case "text":
					open.kind = "text"
					s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id})
					s.emitSources(annotationsToSources(initial.Annotations, s.newID))
				case "image":
					open.kind = "image"
					open.data, open.mimeType, open.uri = initial.Data, initial.MimeType, initial.URI
				}
			}
		case "function_call":
			// TS: the tool name defaults to 'unknown' and tool-input-start
			// is always enqueued at step.start.
			s.hasFunction = true
			open.argsString = ""
			if open.toolName == "" {
				open.toolName = "unknown"
			}
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName}})
			open.startEmitted = true
		default:
			// Step types outside the TS schema (including top-level
			// text/image/video) are unknown blocks: tracked, never emitted.
			if block.Type != "processing_call" && block.Type != "processing_result" {
				open.kind = "unknown"
			}
			// Built-in tool steps never count as client function calls (TS
			// hasFunctionCall is only set by function_call steps).
			if isBuiltinInteractionsToolCall(block.Type) {
				open.toolName = builtinToolName(*block)
				if args, ok := objectArguments(block.Arguments); ok {
					open.args, open.argsRaw = args, block.Arguments
				}
			} else if isBuiltinInteractionsToolResult(block.Type) {
				open.toolName = builtinResultToolName(*block)
			}
		}
	case "step.delta":
		if event.Index == nil || event.Delta == nil {
			return
		}
		open := s.open[*event.Index]
		if open == nil {
			return
		}
		delta := event.Delta
		textShaped := delta.Type == "text" || delta.Type == "text_annotation" || delta.Type == "text_annotation_delta"
		// A pending model_output block is promoted to text on the first
		// text-shaped delta.
		if open.kind == "pending_model_output" && textShaped {
			open.kind = "text"
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: open.id})
		}
		// Image and video deltas carry their whole payload: TS emits the
		// file part inline, whether or not a text block is open at the same
		// index (a model_output step can interleave text and files).
		if delta.Type == "image" && (open.kind == "pending_model_output" || open.kind == "text" || open.kind == "image") {
			s.emitFile(delta.Data, delta.URI, delta.MimeType, "image/png")
			if open.kind == "image" {
				open.data, open.uri = "", ""
			}
			return
		}
		if delta.Type == "video" && (open.kind == "pending_model_output" || open.kind == "text") {
			s.emitFile(delta.Data, delta.URI, delta.MimeType, "video/mp4")
			return
		}
		switch {
		case open.kind == "processing_call" || open.kind == "processing_result":
			if delta.Type != open.kind {
				break
			}
			if delta.Signature != "" {
				open.signature = delta.Signature
			}
			if open.kind == "processing_call" && delta.ID != "" {
				open.toolCallID = delta.ID
			}
			if open.kind == "processing_result" && delta.CallID != "" {
				open.callID = delta.CallID
			}
		case open.kind == "text" && delta.Type == "text":
			if delta.Text != "" {
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: open.id, Text: delta.Text})
			}
		case open.kind == "text" && (delta.Type == "text_annotation" || delta.Type == "text_annotation_delta"):
			s.emitSources(annotationsToSources(delta.Annotations, s.newID))
		case open.kind == "thought":
			if delta.Type == "thought_summary" && len(delta.ContentRaw) > 0 {
				var contentBlock interactionsContentBlock
				if err := json.Unmarshal(delta.ContentRaw, &contentBlock); err == nil && contentBlock.Type == "text" && contentBlock.Text != "" {
					s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: open.id, Reasoning: contentBlock.Text})
				}
			}
			if delta.Type == "thought_signature" && delta.Signature != "" {
				open.signature = delta.Signature
			}
		case open.kind == "function_call" && delta.Type == "arguments_delta":
			var fragment string
			if err := json.Unmarshal(delta.Arguments, &fragment); err == nil && fragment != "" {
				open.argsString += fragment
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName}, Text: fragment})
			}
			if delta.ID != "" {
				open.toolCallID = delta.ID
			}
			if delta.Signature != "" {
				open.signature = delta.Signature
			}
			s.hasFunction = true
		case isBuiltinInteractionsToolCall(open.blockType) && delta.Type == open.blockType:
			if delta.ID != "" {
				open.toolCallID = delta.ID
			}
			if args, ok := objectArguments(delta.Arguments); ok {
				open.args, open.argsRaw = args, delta.Arguments
			}
			if delta.Name != "" && open.blockType == "mcp_server_tool_call" {
				open.toolName = delta.Name
			}
		case isBuiltinInteractionsToolResult(open.blockType) && delta.Type == open.blockType:
			if delta.CallID != "" {
				open.callID = delta.CallID
			}
			if delta.Result != nil {
				open.result = delta.Result
			}
			if delta.IsError != nil {
				open.isError = *delta.IsError
			}
			if delta.Name != "" && open.blockType == "mcp_server_tool_result" {
				open.toolName = delta.Name
			}
		}
	case "step.stop":
		if event.Index == nil {
			return
		}
		open := s.open[*event.Index]
		if open == nil {
			return
		}
		s.stopBlock(open)
		delete(s.open, *event.Index)
	case "interaction.status_update", "interaction.in_progress", "interaction.requires_action":
		// TS: a status-less event keeps the interaction going, except that
		// requires_action implies its own status.
		switch {
		case event.Status != "":
			s.finishStatus = event.Status
		case event.EventType == "interaction.requires_action":
			s.finishStatus = InteractionStatusRequiresAction
		default:
			s.finishStatus = InteractionStatusInProgress
		}
	case "interaction.completed":
		s.completed = true
		if event.Interaction != nil {
			if event.Interaction.Status != "" {
				s.finishStatus = event.Interaction.Status
			}
			if event.Interaction.Usage != nil {
				s.usage = event.Interaction.Usage
			}
			if event.Interaction.ServiceTier != "" {
				s.serviceTier = event.Interaction.ServiceTier
			}
			if event.Interaction.ID != "" {
				s.interactionID = event.Interaction.ID
			}
		}
	case "error":
		s.finishStatus = InteractionStatusFailed
		// TS: createProviderStreamError({message: error?.message ?? 'Unknown
		// interaction error', type: 'error', code: error?.code, data: event}).
		message := "Unknown interaction error"
		var code interface{}
		if event.Error != nil {
			if event.Error.Message != nil {
				message = *event.Error.Message
			}
			if event.Error.Code != "" {
				code = event.Error.Code
			}
		}
		var data interface{}
		if json.Unmarshal([]byte(s.currentEventData), &data) != nil {
			data = nil
		}
		s.completed = true
		s.buffer = append(s.buffer, &provider.StreamChunk{
			Type:        provider.ChunkTypeError,
			Text:        message,
			AbortReason: message,
			Err:         providererrors.NewStreamProviderError(message, "google.interactions", "error", code, nil, nil, data),
		})
	}
}

func (s *interactionsStream) stopBlock(open *interactionOpenBlock) {
	switch open.kind {
	case "text":
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: open.id, ProviderMetadata: providerMetaRaw("", s.interactionID)})
	case "thought":
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: open.id, ProviderMetadata: providerMetaRaw(open.signature, s.interactionID)})
	case "image":
		s.emitFile(open.data, open.uri, open.mimeType, "image/png")
	case "processing_call":
		meta := processingMeta(open.signature, s.interactionID, open.toolCallID, "")
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeCustom, CustomContent: &types.CustomContent{Kind: "google.processing_call", ProviderMetadata: meta}, ProviderMetadata: meta})
	case "processing_result":
		meta := processingMeta(open.signature, s.interactionID, "", open.callID)
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeCustom, CustomContent: &types.CustomContent{Kind: "google.processing_result", ProviderMetadata: meta}, ProviderMetadata: meta})
	case "function_call":
		var args map[string]interface{}
		if open.argsString != "" {
			_ = json.Unmarshal([]byte(open.argsString), &args)
		}
		if args == nil {
			args = map[string]interface{}{}
		}
		argsJSON := open.argsString
		if argsJSON == "" {
			argsJSON = jsonStringify(args)
		}
		s.buffer = append(s.buffer,
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: open.toolCallID}},
			&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName, Arguments: args, RawArguments: argsJSON, ThoughtSignature: open.signature, ProviderMetadata: providerMetaMap(open.signature, s.interactionID)}, ProviderMetadata: providerMetaRaw(open.signature, s.interactionID)},
		)
	case "pending_model_output":
		// step ended before any delta revealed the content type — no output to emit
	default:
		if isBuiltinInteractionsToolCall(open.blockType) {
			argsJSON := jsonorder.Text(open.argsRaw, open.args)
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName, Arguments: defaultMap(open.args), RawArguments: argsJSON, ProviderExecuted: true}})
		} else if isBuiltinInteractionsToolResult(open.blockType) {
			block := interactionsContentBlock{Type: open.blockType, CallID: open.callID, Name: open.toolName, Result: open.result}
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: open.callID, ToolName: open.toolName, Result: open.result, ProviderExecuted: true}})
			s.emitSources(builtinToolResultSources(block, s.newID))
		}
	}
}

func (s *interactionsStream) appendFinish() {
	if s.finished {
		return
	}
	s.finished = true
	s.buffer = append(s.buffer, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     mapInteractionsFinishReason(s.finishStatus, s.hasFunction),
		RawFinishReason:  s.finishStatus,
		Usage:            usagePtr(convertInteractionsUsage(s.usage)),
		ProviderMetadata: finishMetadata(s.interactionID, s.serviceTier, s.usage),
	})
}

func usagePtr(u types.Usage) *types.Usage {
	return &u
}

func finishMetadata(interactionID, serviceTier string, usage *interactionsUsage) json.RawMessage {
	google := pruneMap(map[string]interface{}{
		"interactionId":          emptyToNil(interactionID),
		"serviceTier":            emptyToNil(serviceTier),
		"outputTokensByModality": outputTokensByModalityMap(usage),
	})
	if google == nil {
		google = map[string]interface{}{}
	}
	raw, _ := json.Marshal(map[string]interface{}{"google": google})
	return raw
}

func defaultMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

type sliceTextStream struct {
	chunks []*provider.StreamChunk
	err    error

	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *sliceTextStream) RequestBody() interface{} { return s.requestBody }

func (s *sliceTextStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) == 0 {
		s.err = io.EOF
		return nil, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func (s *sliceTextStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *sliceTextStream) Close() error { return nil }

// objectArguments reports whether raw is a JSON object (TS: arguments != null
// && typeof arguments === 'object') and decodes it.
func objectArguments(raw json.RawMessage) (map[string]interface{}, bool) {
	var args map[string]interface{}
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil || args == nil {
		return nil, false
	}
	return args, true
}

// emitSources queues source parts, skipping any already surfaced on this
// stream (TS sourceKey: url, else filename ?? title of a document).
func (s *interactionsStream) emitSources(parts []types.ContentPart) {
	for _, part := range parts {
		source, ok := part.(types.SourceContent)
		if !ok {
			continue
		}
		key := "url:" + source.URL
		if source.SourceType != "url" {
			key = "doc:" + firstNonEmpty(source.Filename, source.Title)
		}
		if s.emittedSources[key] {
			continue
		}
		if s.emittedSources == nil {
			s.emittedSources = map[string]bool{}
		}
		s.emittedSources[key] = true
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &source})
	}
}

// emitFile queues a generated file from inline data or a URI (TS: data wins
// over uri, neither emits nothing).
func (s *interactionsStream) emitFile(data, uri, mimeType, defaultMime string) {
	meta := providerMetaRaw("", s.interactionID)
	mediaType := firstNonEmpty(mimeType, defaultMime)
	switch {
	case data != "":
		decoded, _ := base64.StdEncoding.DecodeString(data)
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: mediaType, Data: decoded, ProviderMetadata: meta}, ProviderMetadata: meta})
	case uri != "":
		fileData := types.FileData{Type: types.FileDataTypeURL, URL: uri, MediaType: mediaType}
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: mediaType, FileData: fileData, URL: uri, ProviderMetadata: meta}, ProviderMetadata: meta})
	}
}
