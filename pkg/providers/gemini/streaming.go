package gemini

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

	// Code execution state (used only when cfg.SupportsCodeExecution is true).
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
}

// newStream creates a stream with the given reader and provider configuration.
func newStream(reader io.ReadCloser, cfg Config) *stream {
	return &stream{
		reader: reader,
		parser: streaming.NewSSEParser(reader),
		cfg:    cfg,
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
		s.err = err
		return nil, err
	}
	if streaming.IsStreamDone(event) {
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

// processSSEEvent converts one SSE event into structured chunks appended to
// s.chunkBuffer. Mirrors the TS SDK TransformStream transform + flush handlers.
func (s *stream) processSSEEvent(chunkData Response) {
	if chunkData.PromptFeedback != nil {
		s.lastPromptFeedback = chunkData.PromptFeedback
	}
	if chunkData.UsageMetadata != nil {
		s.lastUsageMetadata = chunkData.UsageMetadata
	}
	if chunkData.ServiceTier != "" {
		s.lastServiceTier = chunkData.ServiceTier
	}
	if len(chunkData.Candidates) == 0 {
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

	// Finish reason: close open blocks, then emit the finish chunk.
	if candidate.FinishReason != "" {
		s.closeOpenBlocks()

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

		finishChunk := &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			FinishReason: fr,
		}
		if provMeta := s.buildFinishMeta(); provMeta != nil {
			finishChunk.ProviderMetadata = provMeta
		}
		s.chunkBuffer = append(s.chunkBuffer, finishChunk)
	}
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
// Returns nil if there is nothing to include.
func (s *stream) buildFinishMeta() json.RawMessage {
	meta := map[string]json.RawMessage{}
	if s.lastPromptFeedback != nil {
		meta["promptFeedback"] = s.lastPromptFeedback
	}
	if s.lastGroundingMetadata != nil {
		meta["groundingMetadata"] = s.lastGroundingMetadata
	}
	if s.lastUrlContextMetadata != nil {
		meta["urlContextMetadata"] = s.lastUrlContextMetadata
	}
	if s.lastSafetyRatings != nil {
		meta["safetyRatings"] = s.lastSafetyRatings
	}
	if s.lastFinishMessage != "" {
		if fm, err := json.Marshal(s.lastFinishMessage); err == nil {
			meta["finishMessage"] = fm
		}
	}
	if s.lastUsageMetadata != nil {
		if um, err := json.Marshal(s.lastUsageMetadata); err == nil {
			meta["usageMetadata"] = um
		}
	}
	// serviceTier is always emitted (null when absent) to match TS SDK behavior.
	if s.lastServiceTier != "" {
		if st, err := json.Marshal(s.lastServiceTier); err == nil {
			meta["serviceTier"] = st
		}
	} else {
		meta["serviceTier"] = json.RawMessage("null")
	}
	if len(meta) == 0 {
		return nil
	}
	provMeta, _ := json.Marshal(map[string]interface{}{s.cfg.MetadataKey: meta})
	return provMeta
}

// processNonFuncPart converts a non-function-call part into chunks.
// Handles code execution, inlineData, and text/reasoning block management.
func (s *stream) processNonFuncPart(part Part) {
	// Code execution (Google only).
	if s.cfg.SupportsCodeExecution {
		if part.ExecutableCode != nil && part.ExecutableCode.Code != "" {
			s.codeExecCount++
			toolCallID := fmt.Sprintf("code-exec-%d", s.codeExecCount)
			s.lastCodeExecID = toolCallID
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               toolCallID,
					ToolName:         "code_execution",
					Arguments:        map[string]interface{}{"code": part.ExecutableCode.Code, "language": part.ExecutableCode.Language},
					ProviderExecuted: true,
				},
			})
			return
		}
		if part.CodeExecutionResult != nil && s.lastCodeExecID != "" {
			toolCallID := s.lastCodeExecID
			s.lastCodeExecID = ""
			s.chunkBuffer = append(s.chunkBuffer, &provider.StreamChunk{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID: toolCallID,
					ToolName:   "code_execution",
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
		sigMeta, _ = json.Marshal(map[string]interface{}{
			s.cfg.MetadataKey: map[string]interface{}{
				"thoughtSignature": part.ThoughtSignature,
			},
		})
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
	toolCallID := part.FunctionCall.Name // Gemini does not provide separate call IDs

	var sigMeta json.RawMessage
	if part.ThoughtSignature != "" {
		sigMeta, _ = json.Marshal(map[string]interface{}{
			s.cfg.MetadataKey: map[string]interface{}{
				"thoughtSignature": part.ThoughtSignature,
			},
		})
	}

	argsJSON, _ := json.Marshal(part.FunctionCall.Args)

	s.chunkBuffer = append(s.chunkBuffer,
		&provider.StreamChunk{
			Type: provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{
				ID:       toolCallID,
				ToolName: part.FunctionCall.Name,
			},
			ProviderMetadata: sigMeta,
		},
		&provider.StreamChunk{
			Type:             provider.ChunkTypeToolInputDelta,
			ID:               toolCallID,
			Text:             string(argsJSON),
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
				ToolName:         part.FunctionCall.Name,
				Arguments:        part.FunctionCall.Args,
				ThoughtSignature: part.ThoughtSignature,
			},
			ProviderMetadata: sigMeta,
		},
	)
}
