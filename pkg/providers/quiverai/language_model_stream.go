package quiverai

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// quiverAIStream wraps the underlying Open Responses stream to merge
// QuiverAI's own per-call warnings (from prepareQuiverAICall) into the
// stream-start chunk's warnings, mirroring the TS SDK's doStream
// TransformStream: `{...part, warnings: [...prepared.warnings, ...part.warnings]}`
// for the first `stream-start` part. Open Responses' own stream does not
// currently emit a stream-start chunk of its own, so this wrapper
// synthesizes one carrying just QuiverAI's warnings when the inner stream's
// first chunk isn't one; if the inner stream ever does emit its own
// stream-start first, the two warning lists are merged onto it instead of
// emitting a duplicate.
type quiverAIStream struct {
	inner    provider.TextStream
	warnings []types.Warning

	started      bool
	hasPending   bool
	pendingChunk *provider.StreamChunk
	pendingErr   error
}

func (s *quiverAIStream) Next() (*provider.StreamChunk, error) {
	if !s.started {
		s.started = true
		chunk, err := s.inner.Next()
		if err == nil && chunk != nil && chunk.Type == provider.ChunkTypeStreamStart {
			merged := make([]types.Warning, 0, len(s.warnings)+len(chunk.Warnings))
			merged = append(merged, s.warnings...)
			merged = append(merged, chunk.Warnings...)
			chunk.Warnings = merged
			return chunk, nil
		}
		s.pendingChunk, s.pendingErr, s.hasPending = chunk, err, true
		return &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: s.warnings}, nil
	}
	if s.hasPending {
		s.hasPending = false
		chunk, err := s.pendingChunk, s.pendingErr
		s.pendingChunk, s.pendingErr = nil, nil
		return chunk, err
	}
	return s.inner.Next()
}

func (s *quiverAIStream) Err() error   { return s.inner.Err() }
func (s *quiverAIStream) Close() error { return s.inner.Close() }
