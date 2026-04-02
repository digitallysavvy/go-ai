package providerutils

import (
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// ExtractHeaders converts an http.Header (map[string][]string) to map[string]string
// by joining multi-value headers with ", " — matching TypeScript SDK behaviour.
// Returns nil when h is nil or empty.
func ExtractHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vs := range h {
		out[http.CanonicalHeaderKey(k)] = strings.Join(vs, ", ")
	}
	return out
}

// responseMetadataStream wraps a TextStream and emits a ChunkTypeResponseMetadata
// chunk as the very first chunk, then delegates all subsequent calls to the inner
// stream. This is the standard way providers attach HTTP response headers to a
// stream without modifying each stream's internal buffer.
type responseMetadataStream struct {
	meta    *provider.StreamChunk
	emitted bool
	inner   provider.TextStream
}

func (s *responseMetadataStream) Next() (*provider.StreamChunk, error) {
	if !s.emitted {
		s.emitted = true
		return s.meta, nil
	}
	return s.inner.Next()
}
func (s *responseMetadataStream) Err() error   { return s.inner.Err() }
func (s *responseMetadataStream) Close() error { return s.inner.Close() }

// WithResponseMetadata wraps stream so that the first chunk is a
// ChunkTypeResponseMetadata chunk carrying the given HTTP response headers.
// Consumers (StreamObject, StreamText) will pick up the real headers and
// response ID from these headers, mirroring the TS SDK's 'response-metadata'
// chunk. h may be nil; in that case the wrapped stream is returned unchanged.
func WithResponseMetadata(stream provider.TextStream, h http.Header) provider.TextStream {
	headers := ExtractHeaders(h)
	if len(headers) == 0 {
		return stream
	}
	meta := &provider.StreamChunk{
		Type: provider.ChunkTypeResponseMetadata,
		ResponseMetadata: &provider.ResponseMetadata{
			Headers:   headers,
			Timestamp: time.Now(),
		},
	}
	return &responseMetadataStream{meta: meta, inner: stream}
}
