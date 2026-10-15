package googlevertex

import (
	"errors"
	"net/http"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS googleVertexFailedResponseHandler: error.message when the body matches
// the Google error schema, otherwise the HTTP status text.
func TestVertexErrorsUseSchemaMessageOrStatusText(t *testing.T) {
	var pe *providererrors.ProviderError
	err := vertexEmbeddingError("google-vertex", &internalhttp.HTTPStatusError{StatusCode: 502, Body: []byte("upstream")})
	if !errors.As(err, &pe) || pe.Message != "Bad Gateway" || pe.StatusCode != 502 {
		t.Fatalf("embedding: %v", err)
	}
	err = vertexEmbeddingError("google-vertex", &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":{"code":400,"message":"nope","status":"INVALID_ARGUMENT"}}`)})
	if !errors.As(err, &pe) || pe.Message != "nope" {
		t.Fatalf("embedding schema: %v", err)
	}
	err = cloudTTSError(429, []byte(""), http.Header{})
	if !errors.As(err, &pe) || pe.Message != "Too Many Requests" {
		t.Fatalf("tts: %v", err)
	}
}
