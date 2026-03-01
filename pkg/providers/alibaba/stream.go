package alibaba

import (
	"encoding/json"
	"io"
	"strings"

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

	// State for accumulating tool calls across chunks
	toolCalls map[int]*toolCallAccumulator
	// Finish reason captured from final chunk
	finishReason string
	// Usage captured from final chunk
	usage *types.Usage
	// Whether we need to emit a finish chunk
	needsFinishChunk bool
}

// toolCallAccumulator tracks the state of a tool call being built across chunks
type toolCallAccumulator struct {
	ID       string
	Type     string
	Name     string
	Args     strings.Builder
	Complete bool
}

// newAlibabaStream creates a new Alibaba stream
func newAlibabaStream(reader io.ReadCloser) *alibabaStream {
	return &alibabaStream{
		reader:    reader,
		parser:    streaming.NewSSEParser(reader),
		toolCalls: make(map[int]*toolCallAccumulator),
	}
}

// Read implements io.Reader
func (s *alibabaStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *alibabaStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *alibabaStream) Next() (*provider.StreamChunk, error) {
	if s.err != nil {
		return nil, s.err
	}

	// Check if we need to emit a finish chunk from previous processing
	if s.needsFinishChunk {
		s.needsFinishChunk = false
		return s.buildFinishChunk(), nil
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

	// Parse the event data as JSON
	var chunk alibabaStreamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
		// Skip unparseable chunks (common in SSE streams)
		return s.Next()
	}

	// Process chunk and return appropriate StreamChunk
	return s.processChunk(&chunk)
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
		// If this chunk also has a finish reason, we need to emit finish chunk next
		if choice.FinishReason != "" {
			s.needsFinishChunk = true
		}
		return &provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			Reasoning: delta.ReasoningContent,
		}, nil
	}

	// Handle text content
	if delta.Content != "" {
		// If this chunk also has a finish reason, we need to emit finish chunk next
		if choice.FinishReason != "" {
			s.needsFinishChunk = true
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: delta.Content,
		}, nil
	}

	// Handle tool call deltas
	if len(delta.ToolCalls) > 0 {
		for _, tc := range delta.ToolCalls {
			if toolCall := s.accumulateToolCall(tc); toolCall != nil {
				// Tool call just became complete, emit it
				// If this chunk also has a finish reason, we need to emit finish chunk later
				if choice.FinishReason != "" {
					s.needsFinishChunk = true
				}
				return &provider.StreamChunk{
					Type:     provider.ChunkTypeToolCall,
					ToolCall: toolCall,
				}, nil
			}
		}
		// No complete tool calls yet
		// If this chunk has a finish reason, emit finish chunk
		if choice.FinishReason != "" {
			return s.buildFinishChunk(), nil
		}
		// Continue to next chunk
		return s.Next()
	}

	// If we have a finish reason but no content, return finish chunk
	if choice.FinishReason != "" {
		return s.buildFinishChunk(), nil
	}

	// Empty chunk, get next
	return s.Next()
}

// accumulateToolCall accumulates tool call data from delta chunks
// Returns a completed ToolCall if the tool call just became complete, nil otherwise
func (s *alibabaStream) accumulateToolCall(tc alibabaToolCallDelta) *types.ToolCall {
	index := tc.Index

	// Get or create accumulator
	acc, exists := s.toolCalls[index]
	if !exists {
		acc = &toolCallAccumulator{
			ID:   tc.ID,
			Type: tc.Type,
		}
		s.toolCalls[index] = acc
	}

	// Update ID and type if provided in this chunk
	if tc.ID != "" {
		acc.ID = tc.ID
	}
	if tc.Type != "" {
		acc.Type = tc.Type
	}

	// Accumulate function name and arguments
	if tc.Function.Name != "" {
		acc.Name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		acc.Args.WriteString(tc.Function.Arguments)
	}

	// Check if just became complete
	if !acc.Complete && acc.ID != "" && acc.Name != "" && acc.Args.Len() > 0 {
		args := acc.Args.String()
		// Check if arguments are valid JSON
		var argsMap map[string]interface{}
		if err := json.Unmarshal([]byte(args), &argsMap); err == nil {
			acc.Complete = true
			// Return the completed tool call
			return &types.ToolCall{
				ID:        acc.ID,
				ToolName:  acc.Name,
				Arguments: argsMap,
			}
		}
	}

	return nil
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
	ID      string               `json:"id,omitempty"`
	Object  string               `json:"object,omitempty"`
	Created int64                `json:"created,omitempty"`
	Model   string               `json:"model,omitempty"`
	Choices []alibabaStreamChoice `json:"choices"`
	Usage   *AlibabaUsage        `json:"usage,omitempty"`
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
	Index    int                          `json:"index"`
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
