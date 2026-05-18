package providerutils

import (
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

type stubStream struct {
	chunks []*provider.StreamChunk
	idx    int
	err    error
}

func (s *stubStream) Next() (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, io.EOF
	}
	ch := s.chunks[s.idx]
	s.idx++
	return ch, nil
}
func (s *stubStream) Err() error   { return s.err }
func (s *stubStream) Close() error { return nil }

func TestExtractHeaders(t *testing.T) {
	if got := ExtractHeaders(nil); got != nil {
		t.Fatalf("ExtractHeaders(nil) = %+v, want nil", got)
	}
	headers := http.Header{
		"x-request-id": {"a", "b"},
		"content-type": {"application/json"},
	}
	got := ExtractHeaders(headers)
	if got["X-Request-Id"] != "a, b" {
		t.Fatalf("expected joined values, got %+v", got)
	}
	if got["Content-Type"] != "application/json" {
		t.Fatalf("expected canonicalized header, got %+v", got)
	}
}

func TestWithResponseMetadata(t *testing.T) {
	base := &stubStream{
		chunks: []*provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "hello"},
			{Type: provider.ChunkTypeFinish},
		},
	}
	wrapped := WithResponseMetadata(base, http.Header{"X-Test": {"1"}}, "m1")

	first, err := wrapped.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if first.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("first chunk type = %v, want response metadata", first.Type)
	}
	if first.ResponseMetadata == nil || first.ResponseMetadata.ModelID != "m1" {
		t.Fatalf("missing response metadata: %+v", first)
	}
	if first.ResponseMetadata.Headers["X-Test"] != "1" {
		t.Fatalf("missing propagated headers: %+v", first.ResponseMetadata.Headers)
	}

	second, err := wrapped.Next()
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if second.Type != provider.ChunkTypeText || second.Text != "hello" {
		t.Fatalf("second chunk = %+v", second)
	}
}

func TestWithResponseMetadataPreservesLeadingStreamStart(t *testing.T) {
	base := &stubStream{
		chunks: []*provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeText, Text: "hello"},
		},
	}
	wrapped := WithResponseMetadata(base, http.Header{"X-Test": {"1"}}, "m1")

	first, err := wrapped.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if first.Type != provider.ChunkTypeStreamStart {
		t.Fatalf("first chunk type = %v, want stream-start", first.Type)
	}

	second, err := wrapped.Next()
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if second.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("second chunk type = %v, want response metadata", second.Type)
	}

	third, err := wrapped.Next()
	if err != nil {
		t.Fatalf("third Next() error = %v", err)
	}
	if third.Type != provider.ChunkTypeText || third.Text != "hello" {
		t.Fatalf("third chunk = %+v", third)
	}
}

func TestWithResponseMetadataNoHeadersReturnsOriginal(t *testing.T) {
	base := &stubStream{}
	wrapped := WithResponseMetadata(base, nil, "m1")
	if wrapped != base {
		t.Fatal("expected original stream when headers are empty")
	}
}
