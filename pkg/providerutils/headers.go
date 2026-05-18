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
// chunk before provider content. If the inner stream starts with stream-start
// warnings, that chunk is preserved first to match the TypeScript SDK lifecycle
// order.
type responseMetadataStream struct {
	meta       *provider.StreamChunk
	emitted    bool
	inner      provider.TextStream
	passQueue  []*provider.StreamChunk
	checkedOne bool
	sawStart   bool
}

func (s *responseMetadataStream) Next() (*provider.StreamChunk, error) {
	if len(s.passQueue) > 0 {
		chunk := s.passQueue[0]
		s.passQueue = s.passQueue[1:]
		return chunk, nil
	}
	if !s.emitted {
		if !s.checkedOne {
			s.checkedOne = true
			chunk, err := s.inner.Next()
			if err != nil {
				return chunk, err
			}
			if chunk != nil && chunk.Type == provider.ChunkTypeStreamStart {
				s.sawStart = true
				return chunk, nil
			}
			s.passQueue = append(s.passQueue, chunk)
		}
		if s.sawStart {
			chunk, err := s.inner.Next()
			if err != nil {
				return chunk, err
			}
			if chunk != nil && chunk.Type == provider.ChunkTypeRaw {
				s.passQueue = append(s.passQueue, s.meta)
				s.emitted = true
				return chunk, nil
			}
			s.passQueue = append(s.passQueue, chunk)
		}
		s.emitted = true
		return s.meta, nil
	}
	return s.inner.Next()
}
func (s *responseMetadataStream) Err() error   { return s.inner.Err() }
func (s *responseMetadataStream) Close() error { return s.inner.Close() }

// WithResponseMetadata wraps stream so that a ChunkTypeResponseMetadata chunk
// carrying the given HTTP response headers is emitted before provider content.
// A leading ChunkTypeStreamStart is kept first so warnings preserve TS ordering.
// Consumers (StreamObject, StreamText) will pick up the real headers and
// response ID from these headers, mirroring the TS SDK's 'response-metadata'
// chunk. h may be nil; in that case the wrapped stream is returned unchanged.
func WithResponseMetadata(stream provider.TextStream, h http.Header, modelID string) provider.TextStream {
	headers := ExtractHeaders(h)
	if len(headers) == 0 {
		return stream
	}
	meta := &provider.StreamChunk{
		Type: provider.ChunkTypeResponseMetadata,
		ResponseMetadata: &provider.ResponseMetadata{
			Headers:   headers,
			Timestamp: time.Now(),
			ModelID:   modelID,
		},
	}
	return &responseMetadataStream{meta: meta, inner: stream}
}
