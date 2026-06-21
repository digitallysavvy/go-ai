package streaming

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// OpenAICompatStream implements the accumulate-and-flush streaming pattern for
// any provider that uses the OpenAI chat completions SSE format.
//
// Security: tool call arguments are accumulated across all SSE deltas and only
// emitted when finish_reason is received, preventing early finalization based on
// JSON parsability of intermediate fragments.
//
// Embed this struct in provider-specific stream types to inherit Next/Err/Close.
type OpenAICompatStream struct {
	reader             io.ReadCloser
	parser             *SSEParser
	err                error
	toolCallTracker    *StreamingToolCallTracker
	flushQueue         []*provider.StreamChunk
	finishReasonMapper func(string) types.FinishReason
	pendingEventData   string
	// IncludeRawChunks emits the raw parsed SSE event before normal processing,
	// matching providers that expose TypeScript's includeRawChunks behavior.
	IncludeRawChunks bool
	// OnExtraDelta is an optional hook called with raw SSE event bytes before
	// standard delta processing. If it returns (chunk, true), that chunk is
	// returned immediately. Return (nil, false) to fall through to standard
	// processing. Use this to handle provider-specific delta fields (e.g. xAI's
	// reasoning_content).
	OnExtraDelta func(eventBytes []byte) (*provider.StreamChunk, bool)

	// OnBeforeDelta is an optional hook called with raw SSE event bytes before
	// standard delta processing. Any chunks it returns are prepended to the
	// flush queue; standard processing continues regardless. Use this to inject
	// additional chunks from top-level event fields that standard processing
	// does not handle (e.g. xAI's top-level citations array).
	OnBeforeDelta func(eventBytes []byte) []*provider.StreamChunk

	// OnReasoningDelta extracts reasoning text from the raw SSE event bytes.
	// Called before standard text/tool processing. If it returns (text, true)
	// where text != "", the stream manages reasoning-start/delta/end blocks.
	// If it returns ("", true), hook handled it but no reasoning text (no-op).
	// If it returns ("", false), standard processing continues unchanged.
	OnReasoningDelta func(eventBytes []byte) (text string, ok bool)

	// isActiveReasoning tracks whether we are inside a reasoning block.
	isActiveReasoning bool
}

// NewOpenAICompatStream creates a new OpenAICompatStream.
// mapper converts a raw finish_reason string to a types.FinishReason.
// Pass providerutils.MapOpenAIFinishReason for the standard mapping, or a
// custom function for providers that extend the standard set (e.g. Mistral's
// "model_length").
func NewOpenAICompatStream(reader io.ReadCloser, mapper func(string) types.FinishReason) *OpenAICompatStream {
	return &OpenAICompatStream{
		reader:             reader,
		parser:             NewSSEParser(reader),
		toolCallTracker:    NewStreamingToolCallTracker(),
		finishReasonMapper: mapper,
	}
}

// Close closes the underlying HTTP response body.
func (s *OpenAICompatStream) Close() error {
	return s.reader.Close()
}

// Err returns any non-EOF error that occurred during streaming.
func (s *OpenAICompatStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Next returns the next chunk from the stream.
//
// Text deltas are returned immediately. Tool call argument deltas are buffered
// only until function.name is available, then tool input start/delta chunks are
// emitted live. When finish_reason is received, open tool inputs are closed and
// finalized tool-call chunks are enqueued before the finish chunk.
func (s *OpenAICompatStream) Next() (*provider.StreamChunk, error) {
	// Drain any fully-assembled chunks before reading more SSE events.
	if len(s.flushQueue) > 0 {
		chunk := s.flushQueue[0]
		s.flushQueue = s.flushQueue[1:]
		return chunk, nil
	}

	if s.err != nil {
		return nil, s.err
	}

	eventData := s.pendingEventData
	if eventData != "" {
		s.pendingEventData = ""
	} else {
		event, err := s.parser.Next()
		if err != nil {
			s.err = err
			return nil, err
		}

		if IsStreamDone(event) {
			s.err = io.EOF
			return nil, io.EOF
		}

		eventData = event.Data
		if s.IncludeRawChunks {
			s.pendingEventData = eventData
			var raw interface{}
			if err := json.Unmarshal([]byte(eventData), &raw); err != nil {
				raw = eventData
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeRaw,
				Raw:  raw,
			}, nil
		}
	}

	// The OpenAI-compatible SSE format sends choices[0].delta for streaming.
	// Tool call deltas include an "index" field used to correlate fragments
	// belonging to the same tool call across multiple events.
	var chunkData struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    *int    `json:"index"`
					ID       string  `json:"id"`
					Type     *string `json:"type"` // nullable mid-stream
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
					ExtraContent map[string]interface{} `json:"extra_content,omitempty"`
				} `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Error json.RawMessage `json:"error,omitempty"`
	}

	if err := json.Unmarshal([]byte(eventData), &chunkData); err != nil {
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
		}, nil
	}
	if len(chunkData.Error) > 0 {
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: rawStreamErrorText(chunkData.Error),
		}, nil
	}

	// Pre-delta hook: enqueue extra chunks (e.g. top-level citations) before
	// standard processing. Prepend so they drain before the finish chunk.
	if s.OnBeforeDelta != nil {
		if extra := s.OnBeforeDelta([]byte(eventData)); len(extra) > 0 {
			s.flushQueue = append(extra, s.flushQueue...)
		}
	}

	// Provider-specific delta hook (e.g. xAI reasoning_content).
	if s.OnExtraDelta != nil {
		if chunk, handled := s.OnExtraDelta([]byte(eventData)); handled {
			if chunk != nil && len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, chunk)
				return s.Next()
			}
			return chunk, nil
		}
	}

	// Reasoning delta hook — manage start/end lifecycle.
	if s.OnReasoningDelta != nil {
		if rc, handled := s.OnReasoningDelta([]byte(eventData)); handled && rc != "" {
			if !s.isActiveReasoning {
				s.isActiveReasoning = true
				s.flushQueue = append([]*provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"},
					{Type: provider.ChunkTypeReasoning, Reasoning: rc, ID: "reasoning-0"},
				}, s.flushQueue...)
				return s.Next()
			}
			chunk := &provider.StreamChunk{
				Type:      provider.ChunkTypeReasoning,
				Reasoning: rc,
				ID:        "reasoning-0",
			}
			if len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, chunk)
				return s.Next()
			}
			return chunk, nil
		}
	}

	if len(chunkData.Choices) > 0 {
		choice := chunkData.Choices[0]

		// Text delta — emit immediately.
		if choice.Delta.Content != "" {
			if s.isActiveReasoning {
				s.isActiveReasoning = false
				s.flushQueue = append([]*provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
					{Type: provider.ChunkTypeText, Text: choice.Delta.Content},
				}, s.flushQueue...)
				return s.Next()
			}
			chunk := &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				Text: choice.Delta.Content,
			}
			if len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, chunk)
				return s.Next()
			}
			return chunk, nil
		}

		// Tool call delta — accumulate arguments by index, never emit yet.
		if len(choice.Delta.ToolCalls) > 0 {
			for _, tc := range choice.Delta.ToolCalls {
				chunks := s.toolCallTracker.TrackDelta(ToolCallDelta{
					Index:            tc.Index,
					ID:               tc.ID,
					Name:             tc.Function.Name,
					ArgumentsDelta:   tc.Function.Arguments,
					ProviderMetadata: openAICompatToolCallMetadata(tc.ExtraContent),
				})
				s.enqueueChunks(chunks)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				s.flushToolCallsAndFinish(*choice.FinishReason)
			}
			return s.Next()
		}

		// Finish event — flush all accumulated tool calls, then emit finish.
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			s.flushToolCallsAndFinish(*choice.FinishReason)
			return s.Next()
		}
	}

	// Empty or unrecognised event — skip and fetch the next one.
	return s.Next()
}

func rawStreamErrorText(raw json.RawMessage) string {
	var withMessage struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &withMessage); err == nil && withMessage.Message != "" {
		return withMessage.Message
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func (s *OpenAICompatStream) enqueueChunks(chunks []ToolCallChunk) {
	for _, chunk := range chunks {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
}

func (s *OpenAICompatStream) flushToolCallsAndFinish(finishReason string) {
	if s.isActiveReasoning {
		s.isActiveReasoning = false
		s.flushQueue = append([]*provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
		}, s.flushQueue...)
	}
	for _, chunk := range s.toolCallTracker.Flush() {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
		if c.Type == provider.ChunkTypeError {
			return
		}
	}
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
		Type:         provider.ChunkTypeFinish,
		FinishReason: s.finishReasonMapper(finishReason),
	})
}

func openAICompatToolCallMetadata(extraContent map[string]interface{}) map[string]interface{} {
	if len(extraContent) == 0 {
		return nil
	}
	googleRaw, ok := extraContent["google"].(map[string]interface{})
	if !ok {
		return nil
	}
	signature, _ := googleRaw["thought_signature"].(string)
	if signature == "" {
		return nil
	}
	return map[string]interface{}{
		"google": map[string]interface{}{
			"thoughtSignature": signature,
		},
	}
}
