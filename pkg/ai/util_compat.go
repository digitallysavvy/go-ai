package ai

import (
	"io"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/jsonparser"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// ParsePartialJSON parses potentially-incomplete JSON content.
func ParsePartialJSON(text string) jsonparser.ParseResult {
	return jsonparser.ParsePartialJSON(text)
}

// SimulateReadableStream creates a simple text stream from in-memory chunks.
func SimulateReadableStream(chunks []provider.StreamChunk, perChunkDelay time.Duration) provider.TextStream {
	copied := make([]provider.StreamChunk, len(chunks))
	copy(copied, chunks)
	return &simulatedReadableStream{chunks: copied, perChunkDelay: perChunkDelay}
}

type simulatedReadableStream struct {
	chunks        []provider.StreamChunk
	idx           int
	perChunkDelay time.Duration
}

func (s *simulatedReadableStream) Next() (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	if s.perChunkDelay > 0 {
		timer := time.NewTimer(s.perChunkDelay)
		<-timer.C
	}
	ch := s.chunks[s.idx]
	s.idx++
	return &ch, nil
}

func (s *simulatedReadableStream) Close() error { return nil }
func (s *simulatedReadableStream) Err() error   { return nil }
