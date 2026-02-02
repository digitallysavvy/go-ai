package middleware

import (
	"context"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SimulateStreamingMiddleware returns middleware that converts non-streaming
// generate responses into simulated streams.
//
// This is useful for providers that don't support streaming natively, or for
// testing streaming behavior with non-streaming responses.
//
// Example:
//
//	middleware := SimulateStreamingMiddleware()
//	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)
//
//	// Now stream calls will use generate internally and simulate streaming
//	stream, err := wrapped.DoStream(ctx, opts)
func SimulateStreamingMiddleware() *LanguageModelMiddleware {
	return &LanguageModelMiddleware{
		SpecificationVersion: "v3",

		// Only wrap stream, not generate
		WrapStream: func(
			ctx context.Context,
			doGenerate func() (*types.GenerateResult, error),
			doStream func() (provider.TextStream, error),
			params *provider.GenerateOptions,
			model provider.LanguageModel,
		) (provider.TextStream, error) {
			// Call generate instead of stream
			result, err := doGenerate()
			if err != nil {
				return nil, err
			}

			// Create a simulated stream from the result
			return &simulatedStream{
				result:  result,
				chunks:  nil, // Will be built lazily
				current: 0,
			}, nil
		},
	}
}

// simulatedStream simulates a streaming response from a GenerateResult
type simulatedStream struct {
	result  *types.GenerateResult
	chunks  []*provider.StreamChunk
	current int
	closed  bool
}

// buildChunks creates the sequence of chunks that simulate streaming
func (s *simulatedStream) buildChunks() {
	if s.chunks != nil {
		return
	}

	s.chunks = []*provider.StreamChunk{}

	// Emit text content as a single text chunk
	if len(s.result.Text) > 0 {
		s.chunks = append(s.chunks, &provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: s.result.Text,
		})
	}

	// Emit tool calls
	for i := range s.result.ToolCalls {
		s.chunks = append(s.chunks, &provider.StreamChunk{
			Type:     provider.ChunkTypeToolCall,
			ToolCall: &s.result.ToolCalls[i],
		})
	}

	// Emit usage information
	s.chunks = append(s.chunks, &provider.StreamChunk{
		Type:  provider.ChunkTypeUsage,
		Usage: &s.result.Usage,
	})

	// Emit finish chunk
	s.chunks = append(s.chunks, &provider.StreamChunk{
		Type:         provider.ChunkTypeFinish,
		FinishReason: s.result.FinishReason,
		Usage:        &s.result.Usage,
	})
}

// Next returns the next chunk in the simulated stream
func (s *simulatedStream) Next() (*provider.StreamChunk, error) {
	if s.closed {
		return nil, io.EOF
	}

	// Build chunks on first access
	if s.chunks == nil {
		s.buildChunks()
	}

	// Check if we've reached the end
	if s.current >= len(s.chunks) {
		return nil, io.EOF
	}

	chunk := s.chunks[s.current]
	s.current++
	return chunk, nil
}

// Read implements io.Reader (required by TextStream interface)
func (s *simulatedStream) Read(p []byte) (n int, err error) {
	// Simulated streams don't support raw reading
	// Return EOF to indicate no raw data available
	return 0, io.EOF
}

// Close closes the simulated stream
func (s *simulatedStream) Close() error {
	s.closed = true
	return nil
}

// Err returns any error from the stream (always nil for simulated streams)
func (s *simulatedStream) Err() error {
	return nil
}
