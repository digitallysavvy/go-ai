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

// TS doStream returns response.headers beside the stream: the wrapper
// reports them through provider.StreamResponseHeaders and passes every part
// through unchanged, with no header part (review round 87e).
func TestWithResponseMetadata(t *testing.T) {
	base := &stubStream{
		chunks: []*provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"id": "raw-1"}},
			{Type: provider.ChunkTypeText, Text: "hello"},
			{Type: provider.ChunkTypeFinish},
		},
	}
	wrapped := WithResponseMetadata(base, http.Header{"X-Test": {"1"}}, "m1")
	rh, ok := wrapped.(provider.StreamResponseHeaders)
	if !ok || rh.ResponseHeaders()["X-Test"] != "1" {
		t.Fatalf("ResponseHeaders not reported: %T", wrapped)
	}
	var got []provider.ChunkType
	for {
		ch, err := wrapped.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ch.Type)
	}
	want := []provider.ChunkType{provider.ChunkTypeStreamStart, provider.ChunkTypeRaw, provider.ChunkTypeText, provider.ChunkTypeFinish}
	if len(got) != len(want) {
		t.Fatalf("parts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parts = %v, want %v", got, want)
		}
	}
}

func TestWithResponseMetadataNoHeadersReturnsOriginal(t *testing.T) {
	base := &stubStream{}
	wrapped := WithResponseMetadata(base, nil, "m1")
	if wrapped != base {
		t.Fatal("expected original stream when headers are empty")
	}
}
