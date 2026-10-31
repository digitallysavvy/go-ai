package streaming

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// JSONResponseToolStream converts a streamed JSON response tool into text
// events so the SDK can parse it as structured output (TS
// convertJsonResponseToolStream).
//
// Providers that cannot use native structured output for a request use a JSON
// response tool instead. Conversational text is suppressed, application tool
// calls pass through so the caller can execute them, and a response containing
// only the JSON tool finishes with "stop" rather than "tool-calls".
type JSONResponseToolStream struct {
	inner    provider.TextStream
	toolName string

	responseToolIDs         map[string]bool
	responseToolsWithDeltas map[string]bool
	exactArguments          bool
	hasResponseTool         bool
	hasApplicationTool      bool
	queue                   []*provider.StreamChunk
}

// NewJSONResponseToolStream wraps inner so the tool named toolName is surfaced
// as a text block.
func NewJSONResponseToolStream(inner provider.TextStream, toolName string) *JSONResponseToolStream {
	return &JSONResponseToolStream{
		inner:                   inner,
		toolName:                toolName,
		responseToolIDs:         map[string]bool{},
		responseToolsWithDeltas: map[string]bool{},
	}
}

// WithExactArguments makes a tool-call without streamed deltas emit its raw
// argument string as is, including the empty string, as TS does
// (`delta: part.input`). By default an empty string becomes "{}".
func (s *JSONResponseToolStream) WithExactArguments() *JSONResponseToolStream {
	s.exactArguments = true
	return s
}

func chunkToolInputID(chunk *provider.StreamChunk) string {
	if chunk.ID != "" {
		return chunk.ID
	}
	if chunk.ToolCall != nil {
		return chunk.ToolCall.ID
	}
	return ""
}

// Next implements provider.TextStream.
func (s *JSONResponseToolStream) Next() (*provider.StreamChunk, error) {
	for {
		if len(s.queue) > 0 {
			chunk := s.queue[0]
			s.queue = s.queue[1:]
			return chunk, nil
		}
		chunk, err := s.inner.Next()
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			return nil, nil
		}
		s.convert(chunk)
	}
}

func (s *JSONResponseToolStream) convert(chunk *provider.StreamChunk) {
	switch chunk.Type {
	case provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd:
		// The response tool supplies the output; omit conversational text.
		return

	case provider.ChunkTypeToolInputStart:
		if chunk.ToolCall != nil && chunk.ToolCall.ToolName == s.toolName {
			id := chunkToolInputID(chunk)
			s.responseToolIDs[id] = true
			s.queue = append(s.queue, &provider.StreamChunk{
				Type:             provider.ChunkTypeTextStart,
				ID:               id,
				ProviderMetadata: chunk.ProviderMetadata,
			})
			return
		}

	case provider.ChunkTypeToolInputDelta:
		id := chunkToolInputID(chunk)
		if s.responseToolIDs[id] {
			if len(chunk.Text) > 0 {
				s.responseToolsWithDeltas[id] = true
			}
			s.queue = append(s.queue, &provider.StreamChunk{
				Type:             provider.ChunkTypeText,
				ID:               id,
				Text:             chunk.Text,
				ProviderMetadata: chunk.ProviderMetadata,
			})
			return
		}

	case provider.ChunkTypeToolInputEnd:
		if s.responseToolIDs[chunkToolInputID(chunk)] {
			return
		}

	case provider.ChunkTypeToolCall:
		if chunk.ToolCall != nil && chunk.ToolCall.ToolName == s.toolName {
			id := chunk.ToolCall.ID
			if !s.responseToolsWithDeltas[id] {
				text := providerutils.JSONResponseToolText(*chunk.ToolCall)
				if s.exactArguments {
					text = chunk.ToolCall.RawArguments
				}
				s.queue = append(s.queue, &provider.StreamChunk{
					Type:             provider.ChunkTypeText,
					ID:               id,
					Text:             text,
					ProviderMetadata: chunk.ProviderMetadata,
				})
			}
			s.queue = append(s.queue, &provider.StreamChunk{
				Type:             provider.ChunkTypeTextEnd,
				ID:               id,
				ProviderMetadata: chunk.ProviderMetadata,
			})
			s.hasResponseTool = true
			return
		}
		if chunk.ToolCall != nil && !chunk.ToolCall.ProviderExecuted {
			s.hasApplicationTool = true
		}

	case provider.ChunkTypeFinish:
		if s.hasResponseTool && !s.hasApplicationTool && chunk.FinishReason == types.FinishReasonToolCalls {
			converted := *chunk
			converted.FinishReason = types.FinishReasonStop
			s.queue = append(s.queue, &converted)
			return
		}
	}
	s.queue = append(s.queue, chunk)
}

// Err implements provider.TextStream.
func (s *JSONResponseToolStream) Err() error { return s.inner.Err() }

// Close implements provider.TextStream.
func (s *JSONResponseToolStream) Close() error { return s.inner.Close() }

// RequestBody implements provider.StreamRequestBody by delegating to the
// wrapped stream when it supports it.
func (s *JSONResponseToolStream) RequestBody() interface{} {
	if rb, ok := s.inner.(provider.StreamRequestBody); ok {
		return rb.RequestBody()
	}
	return nil
}
