package cerebras

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LanguageModel applies Cerebras-specific parity fixes on top of the
// OpenAI-compatible chat model.
type LanguageModel struct {
	base provider.LanguageModel
}

func (m *LanguageModel) SpecificationVersion() string { return m.base.SpecificationVersion() }
func (m *LanguageModel) Provider() string             { return "cerebras" }
func (m *LanguageModel) ModelID() string              { return m.base.ModelID() }
func (m *LanguageModel) SupportsTools() bool          { return m.base.SupportsTools() }
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}
func (m *LanguageModel) SupportsImageInput() bool { return m.base.SupportsImageInput() }

func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	result, err := m.base.DoGenerate(ctx, opts)
	if err != nil || result == nil {
		return result, parseCerebrasProviderError(err)
	}
	if cerebrasJSONMode(opts) && result.Text != "" && result.FinishReason == types.FinishReasonToolCalls {
		result.ToolCalls = nil
		result.FinishReason = types.FinishReasonStop
	}
	return result, nil
}

func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	stream, err := m.base.DoStream(ctx, opts)
	if err != nil {
		return nil, parseCerebrasProviderError(err)
	}
	if !cerebrasJSONMode(opts) {
		return stream, nil
	}
	return &cerebrasStream{inner: stream}, nil
}

func cerebrasJSONMode(opts *provider.GenerateOptions) bool {
	return opts != nil && opts.ResponseFormat != nil && opts.ResponseFormat.Type != ""
}

type cerebrasStream struct {
	inner    provider.TextStream
	seenText bool
}

func (s *cerebrasStream) Next() (*provider.StreamChunk, error) {
	for {
		chunk, err := s.inner.Next()
		if err != nil || chunk == nil {
			return chunk, err
		}
		switch chunk.Type {
		case provider.ChunkTypeText:
			if chunk.Text != "" {
				s.seenText = true
			}
			return chunk, nil
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall:
			if s.seenText {
				continue
			}
			return chunk, nil
		default:
			return chunk, nil
		}
	}
}

func (s *cerebrasStream) Close() error { return s.inner.Close() }
func (s *cerebrasStream) Err() error   { return s.inner.Err() }
