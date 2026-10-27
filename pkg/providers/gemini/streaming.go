package gemini

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// stream implements provider.TextStream for Gemini SSE responses.
// It emits block-boundary chunks (text-start/delta/end, reasoning-start/delta/end)
// and tool-input sequences (tool-input-start/delta/end + tool-call) to match the TS SDK.
// Both the google and googlevertex providers use this type via Config injection.
type stream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
	cfg    Config

	// Pre-converted chunks ready to emit. Next() drains this slice before
	// reading the next SSE event, enabling one SSE event → many chunks.
	chunkBuffer []*provider.StreamChunk

	// includeRawChunks emits a ChunkTypeRaw chunk with the parsed SSE payload
	// ahead of the chunks it produces (TS options.includeRawChunks).
	includeRawChunks bool

	// Block boundary state, tracked across SSE events.
	// Mirrors currentTextBlockId / currentReasoningBlockId in the TS SDK.
	currentTextBlockID      string
	currentReasoningBlockID string
	blockCounter            int

	// hasToolCalls tracks whether any user-invoked tool calls were seen,
	// so that a STOP finish reason can be mapped to tool-calls.
	hasToolCalls bool

	// Code execution state. TS parses these parts for both Google and Vertex.
	lastCodeExecID string

	// lastServerToolCallID associates a server-executed built-in tool's
	// toolResponse part with its preceding toolCall part (TS
	// lastServerToolCallId), mirroring lastCodeExecID above.
	lastServerToolCallID string

	// Metadata accumulated across SSE events, emitted on the finish chunk.
	lastGroundingMetadata json.RawMessage
	// requestGroundingMetadata is the current request's grounding snapshot;
	// groundingPerRequest collects the snapshots of the requests that were
	// continued (TS groundingMetadataPerRequest).
	requestGroundingMetadata json.RawMessage
	groundingPerRequest      []json.RawMessage
	emittedSourceURLs        map[string]bool
	lastUrlContextMetadata   json.RawMessage
	lastSafetyRatings        json.RawMessage
	lastFinishMessage        string
	lastPromptFeedback       json.RawMessage
	lastUsageMetadata        *UsageMetadata
	// lastServiceTier is the last top-level chunk serviceTier (fallback only).
	lastServiceTier string

	// activeStreamingToolCalls is a LIFO stack of in-progress streamed
	// function calls, mirroring TS `activeStreamingToolCalls`. A continuation
	// chunk that carries `partialArgs` but no `name` always applies to the
	// top of the stack (TS: `activeStreamingToolCalls[length - 1]`).
	activeStreamingToolCalls []*activeStreamingToolCall
	drainedOnDone            bool
	// deferredErr ends the stream with an error once chunkBuffer has drained
	// (TS: an exception in the transform handler errors the stream after the
	// chunks it already enqueued).
	deferredErr error

	// tnm maps provider tool names (e.g. "code_execution") to caller-chosen
	// custom names, mirroring TS createToolNameMapping.
	tnm toolNameMapping

	// httpHeaders/modelID feed the single response-metadata chunk emitted on
	// the first SSE event (previously done by an external
	// providerutils.WithResponseMetadata wrapper; now internal so the Gemini
	// responseId, TS "hasEmittedResponseMetadata", can be included too).
	httpHeaders             http.Header
	modelID                 string
	emittedResponseMetadata bool
	emittedResponseID       bool

	// finishReason/hasFinished hold the terminal finish reason. TS only
	// enqueues the 'finish' chunk once, from the stream's flush handler, not
	// per SSE event — so we track state here and emit at end-of-stream.
	finishReason    types.FinishReason
	rawFinishReason string
	hasFinished     bool

	// confirmedPromptBlockReason freezes content processing once a genuine
	// (non-"unspecified") promptFeedback.blockReason is seen: later chunks
	// still contribute usage/metadata but no more content (TS: "A confirmed
	// prompt block is terminal for generated content, but later chunks can
	// still contribute usage and provider metadata.").
	confirmedPromptBlockReason string

	// Gemini 4 long decode. continueFn opens the follow-up stream for a
	// CONTINUATION response (nil disables continuation). pendingContinuation
	// is the token to resume with once the current stream ends.
	// lastContinuationToken is surfaced on finish only when the response
	// stopped early. continuedUsage banks the usage of completed requests.
	continueFn              func(token string) (io.ReadCloser, error)
	pendingContinuation     string
	lastContinuationToken   string
	continuedUsage          *UsageMetadata
	initialPromptTokenCount *int
	continuationErr         bool

	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *stream) RequestBody() interface{} { return s.requestBody }

// activeStreamingToolCall tracks one in-progress streamed function call,
// mirroring the anonymous entries TS pushes onto `activeStreamingToolCalls`.
type activeStreamingToolCall struct {
	toolCallID       string
	toolName         string
	accumulator      *GoogleJSONAccumulator
	providerMetadata json.RawMessage

	// thoughtSignature is a Go-SDK convenience mirror of the last non-empty
	// thoughtSignature seen for this call, promoted onto types.ToolCall's
	// dedicated field (TS carries it only inside providerMetadata).
	thoughtSignature string
}

// newStream creates a stream with the given reader and provider configuration.
// httpHeaders/modelID are used for the single response-metadata chunk; both
// may be zero-valued (e.g. in tests constructing the stream directly).
func newStream(reader io.ReadCloser, cfg Config, tnm toolNameMapping, httpHeaders http.Header, modelID string) *stream {
	return &stream{
		reader:       reader,
		parser:       streaming.NewSSEParser(reader),
		cfg:          cfg,
		tnm:          tnm,
		httpHeaders:  httpHeaders,
		modelID:      modelID,
		finishReason: types.FinishReasonInterrupted,
	}
}

// Close implements io.Closer.
func (s *stream) Close() error { return s.reader.Close() }

// Err returns any non-EOF error from the stream.
func (s *stream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Next returns the next chunk in the stream.
// It drains the pre-converted chunk buffer before reading the next SSE event.
//
// Implemented as an explicit loop rather than self-recursion: an SSE event
// that produces no bufferable chunk (e.g. a usageMetadata-only keep-alive
// with no candidates) used to `return s.Next()`, and Go does not eliminate
// that tail call, so a long run of such events grew the goroutine stack
// without bound and could crash the process with an unrecoverable stack
// overflow. Every former `return s.Next()` is now a loop iteration instead.
func (s *stream) Next() (*provider.StreamChunk, error) {
	for {
		if s.err != nil {
			return nil, s.err
		}

		if len(s.chunkBuffer) > 0 {
			chunk := s.chunkBuffer[0]
			s.chunkBuffer = s.chunkBuffer[1:]
			return chunk, nil
		}

		if s.deferredErr != nil {
			s.err = s.deferredErr
			continue
		}

		event, err := s.parser.Next()
		if err != nil {
			// The underlying reader can end without an explicit "[DONE]" SSE
			// event (e.g. in tests, or providers that just close the connection).
			// TS always calls its stream's flush() exactly once regardless of how
			// the stream ends, so finalize here too.
			if err == io.EOF && s.continueStream() {
				continue
			}
			if err == io.EOF && s.finalizeOnce() {
				continue
			}
			s.err = err
			return nil, err
		}
		if streaming.IsStreamDone(event) {
			if s.continueStream() {
				continue
			}
			if s.finalizeOnce() {
				continue
			}
			s.err = io.EOF
			return nil, io.EOF
		}

		if s.includeRawChunks {
			var raw interface{}
			if json.Unmarshal([]byte(event.Data), &raw) != nil {
				raw = event.Data
			}
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw})
		}

		// TS validates each parsed frame against chunkSchema; a mismatch is a
		// TypeValidationError `error` part and the stream keeps reading.
		if message := chunkValidationMessage(event.Data); message != "" {
			s.markParseError()
			errChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: message,
				Err:  providererrors.NewValidationErrorWithContext(compactJSONString(event.Data), validationIssueText(message), nil, &providererrors.ValidationContext{}),
			}
			if len(s.chunkBuffer) > 0 {
				s.chunkBuffer = append(s.chunkBuffer, errChunk)
				continue
			}
			return errChunk, nil
		}

		var chunkData Response
		if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
			// TS google-language-model.ts: `if (!chunk.success) { controller
			// .enqueue({type:'error', error: chunk.error}); return; }` — a
			// malformed chunk (schema/JSON failure) is surfaced as an error
			// chunk and the transform CONTINUES reading, it does not terminate
			// the stream. Previously Go returned this as a terminal Next()
			// error, ending the stream outright (P1-1c part 2).
			s.markParseError()
			var chunkErr error
			if !json.Valid([]byte(event.Data)) {
				var probe interface{}
				chunkErr = providererrors.NewJSONParseError(event.Data, json.Unmarshal([]byte(event.Data), &probe))
			} else {
				chunkErr = providererrors.NewValidationErrorWithContext(compactJSONString(event.Data), fmt.Sprintf("failed to parse stream chunk: %v", err), err, &providererrors.ValidationContext{})
			}
			errChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: chunkErr.Error(),
				Err:  chunkErr,
			}
			if len(s.chunkBuffer) > 0 {
				// A raw chunk precedes the error.
				s.chunkBuffer = append(s.chunkBuffer, errChunk)
				continue
			}
			return errChunk, nil
		}

		s.processSSEEvent(chunkData)
	}
}

// markParseError records TS `finishReason = error` for an unparsable chunk,
// unless the response already reported a finish reason (TS #23814).
func (s *stream) markParseError() {
	if !s.hasFinished {
		s.finishReason, s.rawFinishReason, s.hasFinished = types.FinishReasonError, "", true
	}
}

// continueStream opens the follow-up stream of an automatically continued
// long decode response once the current stream has ended. Returns true when a
// new stream was opened (the caller keeps reading). If the continuation
// request fails, an error chunk is buffered, continuation stops, and the
// caller finalizes: the finish chunk then carries the last continuation token
// so the caller can resume. Mirrors TS continueGoogleStream.
func (s *stream) continueStream() bool {
	if s.pendingContinuation == "" || s.continueFn == nil || s.continuationErr {
		return false
	}
	token := s.pendingContinuation
	s.pendingContinuation = ""
	next, err := s.continueFn(token)
	if err != nil {
		s.continuationErr = true
		message := fmt.Sprintf("failed to continue response: %v", err)
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: message,
			Err:  providererrors.NewStreamProviderError(message, s.cfg.ProviderName, "", nil, nil, nil, nil),
		})
		return false
	}
	s.groundingPerRequest = append(s.groundingPerRequest, rawOrNull(s.requestGroundingMetadata))
	s.requestGroundingMetadata = nil
	_ = s.reader.Close()
	s.reader = next
	s.parser = streaming.NewSSEParser(next)
	// The new response starts without a completion signal (TS resets in
	// stream order via continuation-start, #23814): if it is empty or cut off,
	// the stream reports interrupted rather than the prior response's reason.
	s.finishReason, s.rawFinishReason, s.hasFinished = types.FinishReasonInterrupted, "", false
	return true
}

// finalizeOnce closes any open blocks/tool inputs and appends the single
// 'finish' chunk, exactly once. Returns true the first time it runs (in
// which case the caller should drain s.chunkBuffer via s.Next()), false on
// any subsequent call.
func (s *stream) finalizeOnce() bool {
	if s.drainedOnDone {
		return false
	}
	s.closeOpenBlocks()
	s.chunkBuffer = append(s.chunkBuffer, s.buildFinishChunk())
	s.drainedOnDone = true
	return true
}

// processSSEEvent converts one SSE event into structured chunks appended to
// s.chunkBuffer. Mirrors the TS SDK TransformStream transform handler; the
// 'finish' chunk itself is only enqueued once, from flush (see Next/EOF).
func (s *stream) processSSEEvent(chunkData Response) {
	// An in-stream error frame (TS: value.error != null): surface it as a
	// provider stream error and finish with reason "error".
	if chunkData.Error != nil {
		s.finishReason = types.FinishReasonError
		s.rawFinishReason = ""
		s.hasFinished = true
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: chunkData.Error.Message,
			Err:  s.newStreamError(chunkData.Error),
		})
		return
	}

	// TS emits response-metadata on the first chunk that carries a
	// responseId. Go also emits one on the first chunk without an id (it
	// carries the model id and headers), so a responseId that only shows up
	// in a later chunk gets its own response-metadata chunk.
	if !s.emittedResponseMetadata || (!s.emittedResponseID && chunkData.ResponseID != "") {
		s.emittedResponseMetadata = true
		s.emittedResponseID = chunkData.ResponseID != ""
		headers := providerutils.ExtractHeaders(s.httpHeaders)
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeResponseMetadata,
			ResponseMetadata: &provider.ResponseMetadata{
				ID:        chunkData.ResponseID,
				Headers:   headers,
				ModelID:   s.modelID,
				Timestamp: time.Now(),
			},
		})
	}

	if chunkData.UsageMetadata != nil {
		s.lastUsageMetadata = chunkData.UsageMetadata
	}
	if chunkData.ServiceTier != "" {
		s.lastServiceTier = chunkData.ServiceTier
	}

	if chunkData.PromptFeedback != nil && s.confirmedPromptBlockReason == "" {
		s.lastPromptFeedback = chunkData.PromptFeedback
		if reason := promptFeedbackBlockReason(chunkData.PromptFeedback); isConfirmedPromptBlockReason(reason) {
			s.confirmedPromptBlockReason = reason
			s.finishReason = types.FinishReasonContentFilter
			s.rawFinishReason = reason
			s.hasFinished = true
		}
	}

	if len(chunkData.Candidates) > 0 && chunkData.Candidates[0].GroundingMetadata != nil {
		s.requestGroundingMetadata = chunkData.Candidates[0].GroundingMetadata
	}

	// A confirmed prompt block is terminal for generated content, but later
	// chunks can still contribute usage and provider metadata (handled above).
	if s.confirmedPromptBlockReason != "" || len(chunkData.Candidates) == 0 {
		return
	}
	candidate := chunkData.Candidates[0]

	if candidate.GroundingMetadata != nil {
		s.lastGroundingMetadata = candidate.GroundingMetadata
	}
	if candidate.UrlContextMetadata != nil {
		s.lastUrlContextMetadata = candidate.UrlContextMetadata
	}
	if candidate.SafetyRatings != nil {
		s.lastSafetyRatings = candidate.SafetyRatings
	}
	if candidate.FinishMessage != "" {
		s.lastFinishMessage = candidate.FinishMessage
	}
	if candidate.ContinuationToken != "" {
		s.lastContinuationToken = candidate.ContinuationToken
		if candidate.FinishReason == "CONTINUATION" {
			s.pendingContinuation = candidate.ContinuationToken
		}
	}

	// Grounding chunks become url sources, each URL emitted once (TS
	// emittedSourceUrls; document sources are not streamed).
	for _, src := range extractSources(candidate.GroundingMetadata, s.generateID) {
		if src.SourceType != "url" || s.emittedSourceURLs[src.URL] {
			continue
		}
		if s.emittedSourceURLs == nil {
			s.emittedSourceURLs = map[string]bool{}
		}
		s.emittedSourceURLs[src.URL] = true
		src := src
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &src})
	}

	// Process parts: non-function-call parts first (text/thought/code/inline),
	// then function call parts. Matches TS: main loop + getToolCallsFromParts.
	parts := candidate.Content.Parts
	for _, part := range parts {
		if part.FunctionCall == nil {
			s.processNonFuncPart(part)
		}
	}
	for _, part := range parts {
		if part.FunctionCall != nil {
			s.processFuncCallPart(part)
		}
	}

	if candidate.FinishReason != "" {
		var fr types.FinishReason
		switch candidate.FinishReason {
		case "STOP":
			if s.hasToolCalls {
				fr = types.FinishReasonToolCalls
			} else {
				fr = types.FinishReasonStop
			}
		case "MAX_TOKENS", "CONTINUATION":
			fr = types.FinishReasonLength
		case "IMAGE_SAFETY", "RECITATION", "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			fr = types.FinishReasonContentFilter
		case "MALFORMED_FUNCTION_CALL":
			fr = types.FinishReasonError
		default:
			fr = types.FinishReasonOther
		}
		s.finishReason = fr
		s.rawFinishReason = candidate.FinishReason
		s.hasFinished = true
	}

	if candidate.FinishReason == "CONTINUATION" {
		// Usage metadata is per request. The next request reports its own
		// usage, so bank the usage of this one.
		if s.continuedUsage == nil && s.lastUsageMetadata != nil {
			v := s.lastUsageMetadata.PromptTokenCount
			s.initialPromptTokenCount = &v
		}
		s.continuedUsage = addUsageMetadata(s.continuedUsage, s.lastUsageMetadata)
		s.lastUsageMetadata = nil
	}
}

// newStreamError mirrors TS createProviderStreamError for a Google in-stream
// error: type is the Google status, code/statusCode the numeric code, and the
// error is retryable for 408/409/429/5xx.
func (s *stream) newStreamError(e *StreamError) *providererrors.StreamProviderError {
	var code interface{}
	var statusCode *int
	retryable := false
	if e.Code != nil {
		c := int(*e.Code)
		code = *e.Code
		statusCode = &c
		retryable = c == 408 || c == 409 || c == 429 || c >= 500
	}
	var data interface{}
	if raw, err := json.Marshal(e); err == nil {
		_ = json.Unmarshal(raw, &data)
	}
	return providererrors.NewStreamProviderError(e.Message, s.cfg.ProviderName, e.Status, code, statusCode, &retryable, data)
}

// buildFinishChunk builds the single 'finish' chunk emitted at end-of-stream
// (TS: flush(controller)), after any open blocks/tool inputs are closed.
func (s *stream) buildFinishChunk() *provider.StreamChunk {
	fr := s.finishReason
	if !s.hasFinished {
		// A clean end without the provider's completion signal is
		// interrupted (TS #23814).
		fr = types.FinishReasonInterrupted
	}
	s.lastUsageMetadata = addUsageMetadata(s.continuedUsage, s.lastUsageMetadata)
	s.continuedUsage = nil
	usage := convertUsage(s.lastUsageMetadata)
	return &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     fr,
		RawFinishReason:  s.rawFinishReason,
		Usage:            &usage,
		ProviderMetadata: s.buildFinishMeta(),
	}
}

// generateID returns a fresh tool-call ID, using the configured generator
// when set (TS: `config.generateId()`), falling back to the shared streaming
// ID generator otherwise.
func (s *stream) generateID() string {
	if s.cfg.GenerateID != nil {
		return s.cfg.GenerateID()
	}
	return streaming.GenerateID()
}

// finishActiveStreamingToolCall pops the most recently opened streaming tool
// call and emits its closing delta (if any), tool-input-end, and tool-call
// chunks. Mirrors TS `finishActiveStreamingToolCall`.
func (s *stream) finishActiveStreamingToolCall() {
	n := len(s.activeStreamingToolCalls)
	if n == 0 {
		return
	}
	active := s.activeStreamingToolCalls[n-1]
	s.activeStreamingToolCalls = s.activeStreamingToolCalls[:n-1]

	finalJSON, closingDelta := active.accumulator.Finalize()

	if closingDelta != "" {
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type:             provider.ChunkTypeToolInputDelta,
			ID:               active.toolCallID,
			Text:             closingDelta,
			ProviderMetadata: active.providerMetadata,
		})
	}

	s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
		Type:             provider.ChunkTypeToolInputEnd,
		ToolCall:         &types.ToolCall{ID: active.toolCallID},
		ProviderMetadata: active.providerMetadata,
	})

	var args map[string]interface{}
	if tree, err := decodeOrderedJSON([]byte(finalJSON)); err == nil {
		if m, ok := toPlainJSON(tree).(map[string]interface{}); ok {
			args = m
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}

	s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
		Type: provider.ChunkTypeToolCall,
		ToolCall: &types.ToolCall{
			ID:               active.toolCallID,
			ToolName:         active.toolName,
			Arguments:        args,
			RawArguments:     finalJSON,
			ThoughtSignature: active.thoughtSignature,
		},
		ProviderMetadata: active.providerMetadata,
	})

	s.hasToolCalls = true
}

// closeOpenBlocks emits end chunks for any open text or reasoning block.
func (s *stream) closeOpenBlocks() {
	if s.currentTextBlockID != "" {
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeTextEnd,
			ID:   s.currentTextBlockID,
		})
		s.currentTextBlockID = ""
	}
	if s.currentReasoningBlockID != "" {
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeReasoningEnd,
			ID:   s.currentReasoningBlockID,
		})
		s.currentReasoningBlockID = ""
	}
}

// buildFinishMeta assembles the ProviderMetadata JSON for the finish chunk.
// Always fully populated (null for absent fields), matching TS
// GoogleProviderMetadata, and written under every configured metadata key.
func (s *stream) buildFinishMeta() json.RawMessage {
	meta := map[string]json.RawMessage{
		"promptFeedback":     rawOrNull(s.lastPromptFeedback),
		"groundingMetadata":  rawOrNull(s.lastGroundingMetadata),
		"urlContextMetadata": rawOrNull(s.lastUrlContextMetadata),
		"safetyRatings":      rawOrNull(s.lastSafetyRatings),
	}
	if len(s.groundingPerRequest) > 0 {
		per := append(append([]json.RawMessage{}, s.groundingPerRequest...), rawOrNull(s.requestGroundingMetadata))
		if gp, err := json.Marshal(per); err == nil {
			meta["groundingMetadataPerRequest"] = gp
		}
	}
	if s.lastFinishMessage != "" {
		fm, _ := json.Marshal(s.lastFinishMessage)
		meta["finishMessage"] = fm
	} else {
		meta["finishMessage"] = json.RawMessage("null")
	}
	if s.lastUsageMetadata != nil {
		if um, err := json.Marshal(s.lastUsageMetadata); err == nil {
			meta["usageMetadata"] = um
		}
	} else {
		meta["usageMetadata"] = json.RawMessage("null")
	}
	// serviceTier is always emitted (null when absent) to match TS SDK behavior.
	// TS: usage?.serviceTier of the final (merged) usage metadata; the
	// top-level chunk serviceTier is only a Go fallback for older responses.
	serviceTier := s.lastServiceTier
	if s.lastUsageMetadata != nil && s.lastUsageMetadata.ServiceTier != "" {
		serviceTier = s.lastUsageMetadata.ServiceTier
	}
	if serviceTier != "" {
		if st, err := json.Marshal(serviceTier); err == nil {
			meta["serviceTier"] = st
		}
	} else {
		meta["serviceTier"] = json.RawMessage("null")
	}
	// Only surface the token when the response stopped early (budget used up,
	// or continuing failed), not for the checkpoint tokens of a stream that
	// finished normally.
	if s.lastContinuationToken != "" && (s.rawFinishReason == "MAX_TOKENS" || s.rawFinishReason == "CONTINUATION") {
		ct, _ := json.Marshal(s.lastContinuationToken)
		meta["continuationToken"] = ct
	}
	if s.initialPromptTokenCount != nil {
		ip, _ := json.Marshal(*s.initialPromptTokenCount)
		meta["initialPromptTokenCount"] = ip
	}
	provMeta, _ := json.Marshal(s.cfg.wrapProviderMetadata(meta))
	return provMeta
}

// processNonFuncPart converts a non-function-call part into chunks.
// Handles code execution, inlineData, and text/reasoning block management.
func (s *stream) processNonFuncPart(part Part) {
	// Code execution. TS parses these for both Google and Vertex.
	{
		if part.ExecutableCode != nil && part.ExecutableCode.Code != "" {
			// TS: generateId() -- a fixed counter would repeat the same id in
			// every step of a multi-step run.
			toolCallID := s.generateID()
			s.lastCodeExecID = toolCallID
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               toolCallID,
					ToolName:         s.tnm.toCustomToolName("code_execution"),
					Arguments:        map[string]interface{}{"code": part.ExecutableCode.Code, "language": part.ExecutableCode.Language},
					RawArguments:     executableCodeInput(part.ExecutableCode.Language, part.ExecutableCode.Code),
					ProviderExecuted: true,
				},
			})
			return
		}
		if part.CodeExecutionResult != nil && s.lastCodeExecID != "" {
			// Do not clear lastCodeExecID: TS associates a result only with the
			// most recently seen executable code part, but does not reset the
			// pointer after emitting the result.
			toolCallID := s.lastCodeExecID
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID: toolCallID,
					ToolName:   s.tnm.toCustomToolName("code_execution"),
					Result: map[string]interface{}{
						"outcome": part.CodeExecutionResult.Outcome,
						"output":  part.CodeExecutionResult.Output,
					},
				},
			})
			return
		}
	}

	// Server-executed built-in tool call/result (distinct from a
	// (user-invoked) functionCall part). TS: `'toolCall' in part` /
	// `'toolResponse' in part`.
	if part.ToolCall != nil {
		toolCallID := part.ToolCall.ID
		if toolCallID == "" {
			toolCallID = s.generateID()
		}
		s.lastServerToolCallID = toolCallID
		args := part.ToolCall.Args
		if args == nil {
			args = map[string]interface{}{}
		}
		meta := map[string]interface{}{
			"serverToolCallId": toolCallID,
			"serverToolType":   part.ToolCall.ToolType,
		}
		if part.ThoughtSignature != "" {
			meta["thoughtSignature"] = part.ThoughtSignature
		}
		metaJSON, _ := json.Marshal(s.cfg.wrapProviderMetadata(meta))
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               toolCallID,
				ToolName:         "server:" + part.ToolCall.ToolType,
				Arguments:        args,
				RawArguments:     jsonorder.Text(json.RawMessage(part.toolCallArgsRaw), args),
				ProviderExecuted: true,
				Dynamic:          true,
			},
			ProviderMetadata: metaJSON,
		})
		return
	}
	if part.ToolResponse != nil {
		toolCallID := s.lastServerToolCallID
		if toolCallID == "" {
			toolCallID = part.ToolResponse.ID
		}
		if toolCallID == "" {
			toolCallID = s.generateID()
		}
		resultValue := part.ToolResponse.Response
		if resultValue == nil {
			resultValue = map[string]interface{}{}
		}
		meta := map[string]interface{}{
			"serverToolCallId": toolCallID,
			"serverToolType":   part.ToolResponse.ToolType,
		}
		if part.ThoughtSignature != "" {
			meta["thoughtSignature"] = part.ThoughtSignature
		}
		metaJSON, _ := json.Marshal(s.cfg.wrapProviderMetadata(meta))
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: toolCallID,
				ToolName:   "server:" + part.ToolResponse.ToolType,
				Result:     resultValue,
			},
			ProviderMetadata: metaJSON,
		})
		s.lastServerToolCallID = ""
		return
	}

	// InlineData → close open blocks, emit reasoning-file or file chunk.
	// Matches TS: 'inlineData' in part → type: hasThought ? 'reasoning-file' : 'file'.
	if part.InlineData != nil {
		s.closeOpenBlocks()
		data := decodeInlineData(part.InlineData.Data)
		// TS: providerMetadata carries the part's own thoughtSignature.
		var fileMeta json.RawMessage
		if part.ThoughtSignature != "" {
			fileMeta, _ = json.Marshal(s.cfg.wrapProviderMetadata(map[string]interface{}{
				"thoughtSignature": part.ThoughtSignature,
			}))
		}
		if part.Thought {
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeReasoningFile,
				ReasoningFileContent: &types.ReasoningFileContent{
					MediaType:        part.InlineData.MimeType,
					Data:             data,
					ProviderMetadata: fileMeta,
				},
				ProviderMetadata: fileMeta,
			})
		} else {
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeFile,
				GeneratedFileContent: &types.GeneratedFileContent{
					MediaType:        part.InlineData.MimeType,
					Data:             data,
					ProviderMetadata: fileMeta,
				},
				ProviderMetadata: fileMeta,
			})
		}
		return
	}

	// Build provider metadata for thoughtSignature, if present.
	var sigMeta json.RawMessage
	if part.ThoughtSignature != "" {
		sigMeta, _ = json.Marshal(s.cfg.wrapProviderMetadata(map[string]interface{}{
			"thoughtSignature": part.ThoughtSignature,
		}))
	}

	// Empty text + thoughtSignature on an open text block → text-delta with metadata only.
	if part.Text == "" && sigMeta != nil && s.currentTextBlockID != "" {
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type:             provider.ChunkTypeText,
			ID:               s.currentTextBlockID,
			ProviderMetadata: sigMeta,
		})
		return
	}

	if part.Text == "" {
		return
	}

	if part.Thought {
		// Reasoning text: close any open text block, open or continue reasoning block.
		if s.currentTextBlockID != "" {
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeTextEnd,
				ID:   s.currentTextBlockID,
			})
			s.currentTextBlockID = ""
		}
		if s.currentReasoningBlockID == "" {
			s.currentReasoningBlockID = fmt.Sprintf("%d", s.blockCounter)
			s.blockCounter++
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningStart,
				ID:               s.currentReasoningBlockID,
				ProviderMetadata: sigMeta,
			})
		}
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoning,
			ID:               s.currentReasoningBlockID,
			Reasoning:        part.Text,
			ProviderMetadata: sigMeta,
		})
	} else {
		// Regular text: close any open reasoning block, open or continue text block.
		if s.currentReasoningBlockID != "" {
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeReasoningEnd,
				ID:   s.currentReasoningBlockID,
			})
			s.currentReasoningBlockID = ""
		}
		if s.currentTextBlockID == "" {
			s.currentTextBlockID = fmt.Sprintf("%d", s.blockCounter)
			s.blockCounter++
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type:             provider.ChunkTypeTextStart,
				ID:               s.currentTextBlockID,
				ProviderMetadata: sigMeta,
			})
		}
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type:             provider.ChunkTypeText,
			ID:               s.currentTextBlockID,
			Text:             part.Text,
			ProviderMetadata: sigMeta,
		})
	}
}

// processFuncCallPart classifies and handles one functionCall part, mirroring
// TS google-language-model.ts's per-chunk state machine exactly:
//
//   - isStreamingChunk: partialArgs present, or a named call explicitly
//     signals more chunks are coming (willContinue == true with no args yet).
//   - isTerminalChunk: an empty `{}` functionCall that only signals "the
//     active streaming call is done" (no name/args/partialArgs/willContinue).
//   - isCompleteCall: a single chunk carries the full name + args already.
//   - isNoArgsCompleteCall: a single chunk names a zero-argument tool call.
//
// Each streamed function call gets its own GoogleJSONAccumulator (per-chunk
// independent function-call model), replacing the old whole-value diffing
// that assumed one growing JSON blob per tool-call ID.
func (s *stream) processFuncCallPart(part Part) {
	fc := part.FunctionCall
	if fc == nil {
		return
	}

	var sigMeta json.RawMessage
	if part.ThoughtSignature != "" {
		sigMeta, _ = json.Marshal(s.cfg.wrapProviderMetadata(map[string]interface{}{
			"thoughtSignature": part.ThoughtSignature,
		}))
	}

	willContinueTrue := fc.WillContinue != nil && *fc.WillContinue

	isStreamingChunk := fc.PartialArgsSet || (fc.Name != "" && willContinueTrue)
	isTerminalChunk := fc.Name == "" && !fc.ArgsSet && !fc.PartialArgsSet && fc.WillContinue == nil
	isCompleteCall := fc.Name != "" && fc.ArgsSet && !fc.PartialArgsSet
	isNoArgsCompleteCall := fc.Name != "" && !fc.ArgsSet && !fc.PartialArgsSet && !willContinueTrue

	switch {
	case isStreamingChunk:
		if fc.Name != "" {
			toolCallID := fc.ID
			if toolCallID == "" {
				toolCallID = s.generateID()
			}
			accumulator := NewGoogleJSONAccumulator()
			s.activeStreamingToolCalls = append(s.activeStreamingToolCalls, &activeStreamingToolCall{
				toolCallID:       toolCallID,
				toolName:         fc.Name,
				accumulator:      accumulator,
				providerMetadata: sigMeta,
				thoughtSignature: part.ThoughtSignature,
			})

			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeToolInputStart,
				ToolCall: &types.ToolCall{
					ID:       toolCallID,
					ToolName: fc.Name,
				},
				ProviderMetadata: sigMeta,
			})

			if fc.PartialArgsSet {
				s.applyPartialArgsAndMaybeFinish(accumulator, toolCallID, fc.PartialArgs, willContinueTrue, sigMeta)
			}
		} else if fc.PartialArgsSet && len(s.activeStreamingToolCalls) > 0 {
			// TS never rewrites `active.providerMetadata` (or, by extension, our
			// convenience thoughtSignature mirror) from a continuation chunk: the
			// finish/tool-call chunk always carries whatever metadata was present
			// when the call was first pushed. A continuation chunk's own
			// thoughtSignature only affects the tool-input-delta emitted for it
			// (via the local `sigMeta` passed below), never the stored `active`.
			active := s.activeStreamingToolCalls[len(s.activeStreamingToolCalls)-1]
			s.applyPartialArgsAndMaybeFinish(active.accumulator, active.toolCallID, fc.PartialArgs, willContinueTrue, sigMeta)
		}

	case isTerminalChunk && len(s.activeStreamingToolCalls) > 0:
		s.finishActiveStreamingToolCall()

	case isCompleteCall:
		toolCallID := fc.ID
		if toolCallID == "" {
			toolCallID = s.generateID()
		}
		toolName := fc.Name
		argsJSON := completeCallArgsJSON(fc)
		args := fc.Args
		if fc.ArgsIsString {
			// Best-effort: the raw wire "args" was itself a JSON string (see
			// FunctionCall.UnmarshalJSON); if it happens to decode to an
			// object, expose it structurally too. TS has no equivalent field
			// here (its tool-call chunk only carries `input` text).
			var m map[string]interface{}
			if json.Unmarshal([]byte(argsJSON), &m) == nil {
				args = m
			}
		}
		if args == nil {
			args = map[string]interface{}{}
		}

		s.chunkBuffer = append(s.chunkBuffer,
			&provider.StreamChunk{
				Type:             provider.ChunkTypeToolInputStart,
				ToolCall:         &types.ToolCall{ID: toolCallID, ToolName: toolName},
				ProviderMetadata: sigMeta,
			},
			&provider.StreamChunk{
				Type:             provider.ChunkTypeToolInputDelta,
				ID:               toolCallID,
				Text:             argsJSON,
				ProviderMetadata: sigMeta,
			},
			&provider.StreamChunk{
				Type:             provider.ChunkTypeToolInputEnd,
				ToolCall:         &types.ToolCall{ID: toolCallID},
				ProviderMetadata: sigMeta,
			},
			&provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               toolCallID,
					ToolName:         toolName,
					Arguments:        args,
					RawArguments:     argsJSON,
					ThoughtSignature: part.ThoughtSignature,
				},
				ProviderMetadata: sigMeta,
			},
		)
		s.hasToolCalls = true

	case isNoArgsCompleteCall:
		toolCallID := fc.ID
		if toolCallID == "" {
			toolCallID = s.generateID()
		}
		toolName := fc.Name

		s.chunkBuffer = append(s.chunkBuffer,
			&provider.StreamChunk{
				Type:             provider.ChunkTypeToolInputStart,
				ToolCall:         &types.ToolCall{ID: toolCallID, ToolName: toolName},
				ProviderMetadata: sigMeta,
			},
			&provider.StreamChunk{
				Type:             provider.ChunkTypeToolInputEnd,
				ToolCall:         &types.ToolCall{ID: toolCallID},
				ProviderMetadata: sigMeta,
			},
			&provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               toolCallID,
					ToolName:         toolName,
					Arguments:        map[string]interface{}{},
					RawArguments:     "{}",
					ThoughtSignature: part.ThoughtSignature,
				},
				ProviderMetadata: sigMeta,
			},
		)
		s.hasToolCalls = true
	}
}

// applyPartialArgsAndMaybeFinish feeds partialArgs into accumulator, emits
// the resulting tool-input-delta (if non-empty), and finishes the streaming
// tool call when neither the functionCall-level willContinue nor any
// individual partial arg's willContinue is true.
func (s *stream) applyPartialArgsAndMaybeFinish(accumulator *GoogleJSONAccumulator, toolCallID string, partialArgs []PartialArg, functionCallWillContinue bool, sigMeta json.RawMessage) {
	result, accErr := accumulator.processPartialArgs(partialArgs)
	if result.TextDelta != "" {
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type:             provider.ChunkTypeToolInputDelta,
			ID:               toolCallID,
			Text:             result.TextDelta,
			ProviderMetadata: sigMeta,
		})
	}

	if accErr != nil {
		// TS would throw (or build arguments that JSON.stringify drops) for a
		// path that conflicts with an earlier value; end the stream with an
		// error instead of panicking or emitting a corrupt tool call.
		s.deferredErr = providererrors.NewStreamProviderError("invalid function call partialArgs: "+accErr.Error(), s.cfg.ProviderName, "", nil, nil, nil, nil)
		return
	}

	allArgsDone := true
	for _, arg := range partialArgs {
		if arg.WillContinue != nil && *arg.WillContinue {
			allArgsDone = false
			break
		}
	}

	if !functionCallWillContinue && allArgsDone {
		s.finishActiveStreamingToolCall()
	}
}

// completeCallArgsJSON returns the JSON text for a single-chunk complete
// function call's arguments, preserving the original wire key order (TS:
// `JSON.stringify(part.functionCall.args ?? {})`, where args was already
// parsed from JSON text that preserves object key insertion order).
func completeCallArgsJSON(fc *FunctionCall) string {
	if fc.ArgsIsString {
		return fc.ArgsString
	}
	if !fc.ArgsSet {
		return "{}"
	}
	tree, err := decodeOrderedJSON(fc.ArgsRaw)
	if err != nil {
		return marshalJSONCompact(fc.Args)
	}
	return marshalOrdered(tree)
}
