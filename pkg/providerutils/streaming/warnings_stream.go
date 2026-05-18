package streaming

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// WarningsStream wraps an inner TextStream and emits a ChunkTypeStreamStart
// chunk carrying pre-stream warnings as the very first chunk. The TypeScript
// SDK emits stream-start even when warnings is empty.
type WarningsStream struct {
	inner    provider.TextStream
	warnings []types.Warning
	started  bool
}

// NewWarningsStream wraps inner, prepending a stream-start chunk with warnings.
func NewWarningsStream(inner provider.TextStream, warnings []types.Warning) *WarningsStream {
	return &WarningsStream{inner: inner, warnings: warnings}
}

func (s *WarningsStream) Next() (*provider.StreamChunk, error) {
	if !s.started {
		s.started = true
		return &provider.StreamChunk{
			Type:     provider.ChunkTypeStreamStart,
			Warnings: s.warnings,
		}, nil
	}
	return s.inner.Next()
}

func (s *WarningsStream) Err() error   { return s.inner.Err() }
func (s *WarningsStream) Close() error { return s.inner.Close() }
