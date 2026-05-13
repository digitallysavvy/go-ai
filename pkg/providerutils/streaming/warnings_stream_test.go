package streaming

import (
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type stubTextStream struct {
	chunks []*provider.StreamChunk
	idx    int
	err    error
	closed bool
}

func (s *stubTextStream) Next() (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	ch := s.chunks[s.idx]
	s.idx++
	return ch, nil
}
func (s *stubTextStream) Err() error { return s.err }
func (s *stubTextStream) Close() error {
	s.closed = true
	return nil
}

func TestWarningsStreamEmitsStartChunk(t *testing.T) {
	inner := &stubTextStream{
		chunks: []*provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "hello"},
		},
		err: errors.New("stream-error"),
	}
	ws := NewWarningsStream(inner, []types.Warning{{Type: "unsupported-setting", Message: "topK not supported"}})

	first, err := ws.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if first.Type != provider.ChunkTypeStreamStart || len(first.Warnings) != 1 {
		t.Fatalf("unexpected first chunk: %+v", first)
	}

	second, err := ws.Next()
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if second.Type != provider.ChunkTypeText || second.Text != "hello" {
		t.Fatalf("unexpected second chunk: %+v", second)
	}

	if ws.Err() == nil || ws.Err().Error() != "stream-error" {
		t.Fatalf("Err() did not delegate to inner stream: %v", ws.Err())
	}

	if err := ws.Close(); err != nil || !inner.closed {
		t.Fatalf("Close() did not delegate: err=%v closed=%v", err, inner.closed)
	}
}

func TestWarningsStreamPassThroughWhenNoWarnings(t *testing.T) {
	inner := &stubTextStream{
		chunks: []*provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "hello"},
			{Type: provider.ChunkTypeFinish},
		},
	}
	ws := NewWarningsStream(inner, nil)

	first, err := ws.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if first.Type != provider.ChunkTypeText {
		t.Fatalf("expected passthrough text chunk, got %+v", first)
	}
}
