package alibaba

import (
	"encoding/json"
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
	endQueued         bool
	includeRawChunks  bool
	responseHeaders   map[string]string
	metadataEmitted   bool
	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *alibabaStream) RequestBody() interface{} { return s.requestBody }

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

// Next returns the next chunk in the stream.
//
// Implemented as an explicit loop rather than self-recursion: Next() used to
// tail-call processChunk(), which itself tail-called back into s.Next() (and
// into emitParsedChunk(), which also tail-called s.Next()) whenever an event
// produced no immediately-returnable chunk. Go does not eliminate tail
// calls, so a long run of such events within one external Next() call could
// grow the goroutine stack without bound and crash the process with an
// unrecoverable stack overflow. processChunk and emitParsedChunk now return
// (*provider.StreamChunk, bool) -- ok meaning "return this chunk" vs
// continue-the-loop -- so this loop drives all looping without adding stack
// frames through either helper.
func (s *alibabaStream) Next() (*provider.StreamChunk, error) {
	for {
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
			if err == io.EOF && s.queueEnd() {
				continue
			}
			s.err = err
			return nil, err
		}

		// Check for stream completion
		if streaming.IsStreamDone(event) {
			if s.queueEnd() {
				continue
			}
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
		// TS alibabaChatChunkSchema has no error member: a frame without
		// `choices` (for example an {"error": ...} envelope) or with a
		// mistyped member is a parse failure carrying a typed error.
		msg, perr := providerutils.CheckChatChunk(event.Data, providerutils.ChatChunkShape{Kind: providerutils.ChunkKindAlibaba})
		if perr == nil {
			if derr := providerutils.UnmarshalChunk([]byte(event.Data), &chunk); derr != nil {
				msg, perr = providerutils.ChunkParseFailure(event.Data, derr)
			}
		}
		if perr != nil {
			errorChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: msg,
				Err:  perr,
			}
			if rawQueued {
				s.flushQueue = append(s.flushQueue, errorChunk)
				continue
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

		// Process chunk and return appropriate StreamChunk.
		result, ok := s.processChunk(&chunk)
		if ok {
			return result, nil
		}
	}
}

// processChunk processes a single Alibaba stream chunk. Returns
// (*provider.StreamChunk, bool), where ok=true means Next() should return
// the chunk immediately and ok=false means Next() should continue its loop
// (see the comment on Next() for why this no longer recurses).
func (s *alibabaStream) processChunk(chunk *alibabaStreamChunk) (*provider.StreamChunk, bool) {
	// Handle usage-only chunks (final chunk with usage data)
	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil {
			s.usage = convertAlibabaUsageToTypes(chunk.Usage)
		}
		// No content, get next chunk
		return nil, false
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

	// TS handles reasoning, content and tool calls independently, in that
	// order. Chunks are appended behind anything already queued for this event.
	if delta.ReasoningContent != "" {
		if !s.isActiveReasoning {
			s.isActiveReasoning = true
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"})
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			Reasoning: delta.ReasoningContent,
			ID:        "reasoning-0",
		})
	}
	if delta.Content != "" {
		if s.isActiveReasoning {
			s.isActiveReasoning = false
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeText, Text: delta.Content})
	}
	// Tool call deltas only accumulate; the tracker finalizes at stream end
	// (TS flush()).
	if len(delta.ToolCalls) > 0 {
		if s.isActiveReasoning {
			s.isActiveReasoning = false
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
		}
		for _, tc := range delta.ToolCalls {
			s.accumulateToolCall(tc)
		}
	}
	return nil, false
}

// accumulateToolCall accumulates tool call data from a delta chunk.
func (s *alibabaStream) accumulateToolCall(tc alibabaToolCallDelta) {
	for _, chunk := range s.toolCallTracker.TrackWirePtr(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments) {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
}

// queueEnd mirrors TS flush(): close the reasoning block, finalize tool calls
// and enqueue the terminal finish part (reason "other" when no finish_reason
// arrived). It reports whether it queued anything and queues at most once.
func (s *alibabaStream) queueEnd() bool {
	if s.endQueued {
		return false
	}
	s.endQueued = true
	if s.isActiveReasoning {
		s.isActiveReasoning = false
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
	}
	for _, chunk := range s.toolCallTracker.Flush() {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
	s.flushQueue = append(s.flushQueue, s.buildFinishChunk())
	return true
}

// buildFinishChunk creates a finish chunk with accumulated data
func (s *alibabaStream) buildFinishChunk() *provider.StreamChunk {
	reason := types.FinishReasonOther
	if s.finishReason != "" {
		reason = providerutils.MapOpenAIFinishReason(s.finishReason)
	}
	return &provider.StreamChunk{
		Type:            provider.ChunkTypeFinish,
		FinishReason:    reason,
		RawFinishReason: s.finishReason,
		Usage:           s.usage,
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
	Name      streaming.WireName `json:"name"`
	Arguments *string            `json:"arguments"`
}

// convertAlibabaUsageToTypes converts Alibaba usage to types.Usage
func convertAlibabaUsageToTypes(usage *AlibabaUsage) *types.Usage {
	if usage == nil {
		return nil
	}
	converted := ConvertAlibabaUsage(*usage)
	return &converted
}
