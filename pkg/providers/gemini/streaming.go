package gemini

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

	toolInputState map[string]*toolInputAccum
	toolInputOrder []string
	drainedOnDone  bool

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

type toolInputAccum struct {
	name              string
	lastArgumentsJSON string
	lastArguments     map[string]interface{}
	thoughtSignature  string
	signatureMeta     json.RawMessage
}

// newStream creates a stream with the given reader and provider configuration.
// httpHeaders/modelID are used for the single response-metadata chunk; both
// may be zero-valued (e.g. in tests constructing the stream directly).
func newStream(reader io.ReadCloser, cfg Config, tnm toolNameMapping, httpHeaders http.Header, modelID string) *stream {
	return &stream{
		reader:         reader,
		parser:         streaming.NewSSEParser(reader),
		cfg:            cfg,
		toolInputState: make(map[string]*toolInputAccum),
		tnm:            tnm,
		httpHeaders:    httpHeaders,
		modelID:        modelID,
		finishReason:   types.FinishReasonOther,
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
	s.flushToolInputs()
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

func (s *stream) flushToolInputs() {
	for _, id := range s.toolInputOrder {
		accum := s.toolInputState[id]
		if accum == nil {
			continue
		}
		args := accum.lastArguments
		if args == nil {
			args = map[string]interface{}{}
		}
		s.chunkBuffer = append(s.chunkBuffer,
			&provider.StreamChunk{
				Type:             provider.ChunkTypeToolInputEnd,
				ToolCall:         &types.ToolCall{ID: id},
				ProviderMetadata: accum.signatureMeta,
			},
			&provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               id,
					ToolName:         accum.name,
					Arguments:        args,
					ThoughtSignature: accum.thoughtSignature,
				},
				ProviderMetadata: accum.signatureMeta,
			},
		)
		delete(s.toolInputState, id)
	}
	s.toolInputOrder = nil
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

// processFuncCallPart emits the tool-input-start/delta/end + tool-call sequence
// for one function call part. Matches TS getToolCallsFromParts output.
func (s *stream) processFuncCallPart(part Part) {
	if part.FunctionCall == nil {
		return
	}
	s.hasToolCalls = true
	toolCallID := part.FunctionCall.ID
	if toolCallID == "" {
		toolCallID = part.FunctionCall.Name
	}

	var sigMeta json.RawMessage
	if part.ThoughtSignature != "" {
		sigMeta, _ = json.Marshal(s.cfg.wrapProviderMetadata(map[string]interface{}{
			"thoughtSignature": part.ThoughtSignature,
		}))
	}

	args := part.FunctionCall.Args
	if args == nil {
		args = map[string]interface{}{}
	}
	argsJSONBytes, _ := json.Marshal(args)
	argsJSON := string(argsJSONBytes)

	accum, ok := s.toolInputState[toolCallID]
	if !ok {
		accum = &toolInputAccum{name: part.FunctionCall.Name}
		s.toolInputState[toolCallID] = accum
		s.toolInputOrder = append(s.toolInputOrder, toolCallID)
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type: provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{
				ID:       toolCallID,
				ToolName: part.FunctionCall.Name,
			},
			ProviderMetadata: sigMeta,
		})
	}

	delta := argsJSON
	if strings.HasPrefix(argsJSON, accum.lastArgumentsJSON) {
		delta = argsJSON[len(accum.lastArgumentsJSON):]
	}
	if delta != "" {
		s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
			Type:             provider.ChunkTypeToolInputDelta,
			ID:               toolCallID,
			Text:             delta,
			ProviderMetadata: sigMeta,
		})
	}

	accum.lastArgumentsJSON = argsJSON
	accum.lastArguments = args
	if part.ThoughtSignature != "" {
		accum.thoughtSignature = part.ThoughtSignature
		accum.signatureMeta = sigMeta
	}
}
