package providerutils

import (
	"encoding/json"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// ExtractHeaders converts an http.Header (map[string][]string) to map[string]string
// by joining multi-value headers with ", " (the last Set-Cookie line wins, as
// fetch iterates each Set-Cookie line separately) and reading each value as the
// JavaScript string TS sees (IsomorphicDecode: each byte one character), like
// TS extractResponseHeaders (Object.fromEntries(response.headers.entries())).
// Returns nil when h is nil or empty.
func ExtractHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vs := range h {
		out[http.CanonicalHeaderKey(k)] = IsomorphicDecode(FetchHeaderEntry(k, vs))
	}
	return out
}

// responseHeadersStream wraps a TextStream and reports the HTTP response
// headers through provider.StreamResponseHeaders, as TS doStream returns
// response.headers beside the stream. The stream's parts pass through
// unchanged: TS has no header part.
type responseHeadersStream struct {
	inner   provider.TextStream
	headers map[string]string
}

func (s *responseHeadersStream) Next() (*provider.StreamChunk, error) { return s.inner.Next() }
func (s *responseHeadersStream) Err() error                           { return s.inner.Err() }
func (s *responseHeadersStream) Close() error                         { return s.inner.Close() }

// ResponseHeaders implements provider.StreamResponseHeaders.
func (s *responseHeadersStream) ResponseHeaders() map[string]string { return s.headers }

// RequestBody implements provider.StreamRequestBody by delegating to inner
// when inner implements it, so wrapping a stream in WithResponseMetadata
// doesn't hide the capability from pkg/ai/stream.go's type assertion.
func (s *responseHeadersStream) RequestBody() interface{} {
	if rb, ok := s.inner.(provider.StreamRequestBody); ok {
		return rb.RequestBody()
	}
	return nil
}

// RequestURL implements provider.StreamRequestURL by delegating to inner when
// inner implements it (TS doStream request.url, 934100cad4), so the wrapper
// doesn't hide the capability from pkg/ai.
func (s *responseHeadersStream) RequestURL() string {
	if ru, ok := s.inner.(provider.StreamRequestURL); ok {
		return ru.RequestURL()
	}
	return ""
}

// WithResponseMetadata wraps stream so that it reports the given HTTP
// response headers through provider.StreamResponseHeaders, which StreamText
// and StreamObject read for the step response (TS doStream returns
// response.headers beside the stream). The stream's parts are unchanged: v0.5
// added a leading header-only response-metadata part, which TS does not have
// (review round 87e). modelID is no longer used; the step response falls back
// to the model's ID, as TS `responseModelId ?? model.modelId`. h may be nil;
// in that case the wrapped stream is returned unchanged.
func WithResponseMetadata(stream provider.TextStream, h http.Header, modelID string) provider.TextStream {
	_ = modelID
	headers := ExtractHeaders(h)
	if len(headers) == 0 {
		return stream
	}
	return &responseHeadersStream{inner: stream, headers: headers}
}

// RawJSONBody decodes a JSON response body for ResponseMetadata.Body, which
// mirrors the TS `response.body` (the parsed raw JSON, not bytes). Bodies that
// are not valid JSON are returned as a string.
func RawJSONBody(b []byte) interface{} {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	return v
}

// ToolArgumentsString returns a tool call's arguments as the JSON text the
// model produced. The wire value is normally a JSON string holding JSON text
// (OpenAI style); some hosts send the object itself, whose text is returned
// as is.
func ToolArgumentsString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
