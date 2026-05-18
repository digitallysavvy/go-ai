package alibaba

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// alibabaStream implements provider.TextStream for Alibaba streaming responses
type alibabaStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error

	toolCallTracker *streaming.StreamingToolCallTracker
	// Finish reason captured from final chunk
	finishReason string
	// Usage captured from final chunk
	usage *types.Usage
	// flushQueue holds fully-assembled chunks to emit before reading more SSE events.
	// Tool calls are enqueued here only when finish_reason is received (flush).
	flushQueue        []*provider.StreamChunk
	isActiveReasoning bool
	includeRawChunks  bool
	responseHeaders   map[string]string
	metadataEmitted   bool
}

// newAlibabaStream creates a new Alibaba stream
func newAlibabaStream(reader io.ReadCloser, includeRawChunks ...bool) *alibabaStream {
	emitRaw := len(includeRawChunks) > 0 && includeRawChunks[0]
	return &alibabaStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		toolCallTracker:  streaming.NewStreamingToolCallTracker(),
		includeRawChunks: emitRaw,
	}
}

// Close implements io.Closer
func (s *alibabaStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *alibabaStream) Next() (*provider.StreamChunk, error) {
	// Emit any fully-assembled chunks (tool calls + finish) before reading more SSE.
	if len(s.flushQueue) > 0 {
		chunk := s.flushQueue[0]
		s.flushQueue = s.flushQueue[1:]
		return chunk, nil
	}

	if s.err != nil {
		return nil, s.err
	}

	// Get next SSE event
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}

	// Check for stream completion
	if streaming.IsStreamDone(event) {
		s.err = io.EOF
		return nil, io.EOF
	}
	rawQueued := false
	if s.includeRawChunks {
		var raw interface{}
		if err := json.Unmarshal([]byte(event.Data), &raw); err != nil {
			raw = event.Data
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type: provider.ChunkTypeRaw,
			Raw:  raw,
		})
		rawQueued = true
	}

	// Parse the event data as JSON
	var chunk alibabaStreamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
		errorChunk := &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
		}
		if rawQueued {
			s.flushQueue = append(s.flushQueue, errorChunk)
			return s.Next()
		}
		return errorChunk, nil
	}
	if !s.metadataEmitted && (chunk.ID != "" || chunk.Model != "" || chunk.Created != 0 || len(s.responseHeaders) > 0) {
		metadata := &provider.ResponseMetadata{
			ID:      chunk.ID,
			ModelID: chunk.Model,
			Headers: s.responseHeaders,
		}
		if chunk.Created != 0 {
			metadata.Timestamp = time.Unix(chunk.Created, 0)
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type:             provider.ChunkTypeResponseMetadata,
			ResponseMetadata: metadata,
		})
		s.metadataEmitted = true
	}

	// Process chunk and return appropriate StreamChunk
	return s.processChunk(&chunk)
}

func (s *alibabaStream) emitParsedChunk(chunk *provider.StreamChunk) (*provider.StreamChunk, error) {
	if len(s.flushQueue) == 0 {
		return chunk, nil
	}
	s.flushQueue = append(s.flushQueue, chunk)
	return s.Next()
}

// processChunk processes a single Alibaba stream chunk
func (s *alibabaStream) processChunk(chunk *alibabaStreamChunk) (*provider.StreamChunk, error) {
	// Handle usage-only chunks (final chunk with usage data)
	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil {
			s.usage = convertAlibabaUsageToTypes(chunk.Usage)
		}
		// No content, get next chunk
		return s.Next()
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	// Capture finish reason if present
	if choice.FinishReason != "" {
		s.finishReason = choice.FinishReason
	}

	// Capture usage if present
	if chunk.Usage != nil {
		s.usage = convertAlibabaUsageToTypes(chunk.Usage)
	}

	// Handle reasoning content (Alibaba thinking mode)
	if delta.ReasoningContent != "" {
		var extra []*provider.StreamChunk
		if !s.isActiveReasoning {
			s.isActiveReasoning = true
			extra = []*provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"},
			}
		}
		reasoningChunk := &provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			Reasoning: delta.ReasoningContent,
			ID:        "reasoning-0",
		}
		if choice.FinishReason != "" {
			s.isActiveReasoning = false
			s.flushQueue = append(extra, reasoningChunk, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"}, s.buildFinishChunk())
			return s.Next()
		}
		if len(extra) > 0 {
			s.flushQueue = append(extra, reasoningChunk)
			return s.Next()
		}
		return s.emitParsedChunk(reasoningChunk)
	}

	// Handle text content - end reasoning if active
	if delta.Content != "" {
		if s.isActiveReasoning {
			s.isActiveReasoning = false
			endChunk := &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"}
			textChunk := &provider.StreamChunk{Type: provider.ChunkTypeText, Text: delta.Content}
			if choice.FinishReason != "" {
				s.flushQueue = append([]*provider.StreamChunk{endChunk, textChunk}, s.buildFinishChunk())
			} else {
				s.flushQueue = []*provider.StreamChunk{endChunk, textChunk}
			}
			return s.Next()
		}
		if choice.FinishReason != "" {
			textChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				Text: delta.Content,
			}
			if len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, textChunk, s.buildFinishChunk())
				return s.Next()
			}
			s.flushQueue = append(s.flushQueue, s.buildFinishChunk())
			return textChunk, nil
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: delta.Content,
		})
	}

	// Handle tool call deltas — only accumulate, never emit mid-stream.
	// Tool calls are finalized and emitted only when finish_reason is received.
	if len(delta.ToolCalls) > 0 {
		for _, tc := range delta.ToolCalls {
			s.accumulateToolCall(tc)
		}
		if choice.FinishReason != "" {
			s.flushToolCalls()
			return s.Next()
		}
		return s.Next()
	}

	// If we have a finish reason but no content, flush tool calls then finish.
	if choice.FinishReason != "" {
		s.flushToolCalls()
		return s.Next()
	}

	// Empty chunk, get next
	return s.Next()
}

// accumulateToolCall accumulates tool call data from a delta chunk.
// Tool calls are never emitted mid-stream; call flushToolCalls() at finish_reason.
func (s *alibabaStream) accumulateToolCall(tc alibabaToolCallDelta) {
	for _, chunk := range s.toolCallTracker.Track(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments) {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
}

// flushToolCalls enqueues all accumulated tool calls onto flushQueue followed by
// a finish chunk. Must be called only when finish_reason is received.
func (s *alibabaStream) flushToolCalls() {
	if s.isActiveReasoning {
		s.isActiveReasoning = false
		s.flushQueue = append([]*provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
		}, s.flushQueue...)
	}
	for _, chunk := range s.toolCallTracker.Flush() {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
	s.flushQueue = append(s.flushQueue, s.buildFinishChunk())
}

// buildFinishChunk creates a finish chunk with accumulated data
func (s *alibabaStream) buildFinishChunk() *provider.StreamChunk {
	return &provider.StreamChunk{
		Type:         provider.ChunkTypeFinish,
		FinishReason: providerutils.MapOpenAIFinishReason(s.finishReason),
		Usage:        s.usage,
	}
}

// Err returns any error that occurred during streaming
func (s *alibabaStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// alibabaStreamChunk represents a single chunk in the Alibaba SSE stream
type alibabaStreamChunk struct {
	ID      string                `json:"id,omitempty"`
	Object  string                `json:"object,omitempty"`
	Created int64                 `json:"created,omitempty"`
	Model   string                `json:"model,omitempty"`
	Choices []alibabaStreamChoice `json:"choices"`
	Usage   *AlibabaUsage         `json:"usage,omitempty"`
}

// alibabaStreamChoice represents a choice in a stream chunk
type alibabaStreamChoice struct {
	Index        int                `json:"index"`
	Delta        alibabaStreamDelta `json:"delta"`
	FinishReason string             `json:"finish_reason,omitempty"`
}

// alibabaStreamDelta represents the delta content in a stream chunk
type alibabaStreamDelta struct {
	Role             string                 `json:"role,omitempty"`
	Content          string                 `json:"content,omitempty"`
	ReasoningContent string                 `json:"reasoning_content,omitempty"` // Alibaba thinking mode
	ToolCalls        []alibabaToolCallDelta `json:"tool_calls,omitempty"`
}

// alibabaToolCallDelta represents a tool call delta in the stream
type alibabaToolCallDelta struct {
	Index    *int                         `json:"index"`
	ID       string                       `json:"id,omitempty"`
	Type     string                       `json:"type,omitempty"`
	Function alibabaToolCallFunctionDelta `json:"function"`
}

// alibabaToolCallFunctionDelta represents a tool call function delta
type alibabaToolCallFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// convertAlibabaUsageToTypes converts Alibaba usage to types.Usage
func convertAlibabaUsageToTypes(usage *AlibabaUsage) *types.Usage {
	if usage == nil {
		return nil
	}
	converted := ConvertAlibabaUsage(*usage)
	return &converted
}
