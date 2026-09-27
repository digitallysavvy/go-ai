package gemini

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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

	// Block boundary state, tracked across SSE events.
	// Mirrors currentTextBlockId / currentReasoningBlockId in the TS SDK.
	currentTextBlockID      string
	currentReasoningBlockID string
	blockCounter            int

	// hasToolCalls tracks whether any user-invoked tool calls were seen,
	// so that a STOP finish reason can be mapped to tool-calls.
	hasToolCalls bool

	// Code execution state. TS parses these parts for both Google and Vertex.
	codeExecCount  int
	lastCodeExecID string

	// Metadata accumulated across SSE events, emitted on the finish chunk.
	lastGroundingMetadata  json.RawMessage
	lastUrlContextMetadata json.RawMessage
	lastSafetyRatings      json.RawMessage
	lastFinishMessage      string
	lastPromptFeedback     json.RawMessage
	lastUsageMetadata      *UsageMetadata
	// lastServiceTier accumulates serviceTier across chunks; last non-empty value wins.
	lastServiceTier string

	// activeStreamingToolCalls is a LIFO stack of in-progress streamed
	// function calls, mirroring TS `activeStreamingToolCalls`. A continuation
	// chunk that carries `partialArgs` but no `name` always applies to the
	// top of the stack (TS: `activeStreamingToolCalls[length - 1]`).
	activeStreamingToolCalls []*activeStreamingToolCall
	drainedOnDone            bool

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

	// finishReason/hasFinished hold the terminal finish reason. TS only
	// enqueues the 'finish' chunk once, from the stream's flush handler, not
	// per SSE event — so we track state here and emit at end-of-stream.
	finishReason types.FinishReason
	hasFinished  bool

	// confirmedPromptBlockReason freezes content processing once a genuine
	// (non-"unspecified") promptFeedback.blockReason is seen: later chunks
	// still contribute usage/metadata but no more content (TS: "A confirmed
	// prompt block is terminal for generated content, but later chunks can
	// still contribute usage and provider metadata.").
	confirmedPromptBlockReason string
}

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
		finishReason: types.FinishReasonOther,
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
func (s *stream) Next() (*provider.StreamChunk, error) {
	if s.err != nil {
		return nil, s.err
	}

	if len(s.chunkBuffer) > 0 {
		chunk := s.chunkBuffer[0]
		s.chunkBuffer = s.chunkBuffer[1:]
		return chunk, nil
	}

	event, err := s.parser.Next()
	if err != nil {
		// The underlying reader can end without an explicit "[DONE]" SSE
		// event (e.g. in tests, or providers that just close the connection).
		// TS always calls its stream's flush() exactly once regardless of how
		// the stream ends, so finalize here too.
		if err == io.EOF && s.finalizeOnce() {
			return s.Next()
		}
		s.err = err
		return nil, err
	}
	if streaming.IsStreamDone(event) {
		if s.finalizeOnce() {
			return s.Next()
		}
		s.err = io.EOF
		return nil, io.EOF
	}

	var chunkData Response
	if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
		return nil, fmt.Errorf("failed to parse stream chunk: %w", err)
	}

	s.processSSEEvent(chunkData)
	return s.Next()
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
	if !s.emittedResponseMetadata {
		s.emittedResponseMetadata = true
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
		if chunkData.UsageMetadata.ServiceTier != "" {
			s.lastServiceTier = chunkData.UsageMetadata.ServiceTier
		}
	} else if chunkData.ServiceTier != "" {
		s.lastServiceTier = chunkData.ServiceTier
	}

	if chunkData.PromptFeedback != nil && s.confirmedPromptBlockReason == "" {
		s.lastPromptFeedback = chunkData.PromptFeedback
		if reason := promptFeedbackBlockReason(chunkData.PromptFeedback); isConfirmedPromptBlockReason(reason) {
			s.confirmedPromptBlockReason = reason
			s.finishReason = types.FinishReasonContentFilter
			s.hasFinished = true
		}
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
		case "MAX_TOKENS":
			fr = types.FinishReasonLength
		case "IMAGE_SAFETY", "RECITATION", "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			fr = types.FinishReasonContentFilter
		case "MALFORMED_FUNCTION_CALL":
			fr = types.FinishReasonError
		default:
			fr = types.FinishReasonOther
		}
		s.finishReason = fr
		s.hasFinished = true
	}
}

// buildFinishChunk builds the single 'finish' chunk emitted at end-of-stream
// (TS: flush(controller)), after any open blocks/tool inputs are closed.
func (s *stream) buildFinishChunk() *provider.StreamChunk {
	fr := s.finishReason
	if !s.hasFinished {
		fr = types.FinishReasonOther
	}
	usage := convertUsage(s.lastUsageMetadata)
	return &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     fr,
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
		if mtc, err := json.Marshal(modalityTokenCounts(s.lastUsageMetadata)); err == nil {
			meta["modalityTokenCounts"] = mtc
		}
	} else {
		meta["usageMetadata"] = json.RawMessage("null")
	}
	// serviceTier is always emitted (null when absent) to match TS SDK behavior.
	if s.lastServiceTier != "" {
		if st, err := json.Marshal(s.lastServiceTier); err == nil {
			meta["serviceTier"] = st
		}
	} else {
		meta["serviceTier"] = json.RawMessage("null")
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
			s.codeExecCount++
			toolCallID := fmt.Sprintf("code-exec-%d", s.codeExecCount)
			s.lastCodeExecID = toolCallID
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               toolCallID,
					ToolName:         s.tnm.toCustomToolName("code_execution"),
					Arguments:        map[string]interface{}{"code": part.ExecutableCode.Code, "language": part.ExecutableCode.Language},
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

	// InlineData → close open blocks, emit reasoning-file or file chunk.
	// Matches TS: 'inlineData' in part → type: hasThought ? 'reasoning-file' : 'file'.
	if part.InlineData != nil {
		s.closeOpenBlocks()
		data := decodeInlineData(part.InlineData.Data)
		if part.Thought {
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeReasoningFile,
				ReasoningFileContent: &types.ReasoningFileContent{
					MediaType: part.InlineData.MimeType,
					Data:      data,
				},
			})
		} else {
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeFile,
				GeneratedFileContent: &types.GeneratedFileContent{
					MediaType: part.InlineData.MimeType,
					Data:      data,
				},
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
					Arguments:        fc.Args,
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
	result := accumulator.ProcessPartialArgs(partialArgs)
	if result.TextDelta != "" {
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type:             provider.ChunkTypeToolInputDelta,
			ID:               toolCallID,
			Text:             result.TextDelta,
			ProviderMetadata: sigMeta,
		})
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
	if !fc.ArgsSet {
		return "{}"
	}
	tree, err := decodeOrderedJSON(fc.ArgsRaw)
	if err != nil {
		return marshalJSONCompact(fc.Args)
	}
	return marshalOrdered(tree)
}
