package deepseek

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// P1-1c part 2: a mid-stream `error` frame's type/code must be mapped
// through deepseekStreamErrorMetadata (ported from TS
// getDeepSeekStreamErrorMetadata) into a structured StreamProviderError, not
// just a bare message.

func TestDeepSeekStreamErrorInsufficientQuotaForcedNonRetryable(t *testing.T) {
	// TS special-case: insufficient_quota is (429, isRetryable:false), even
	// though 429 would normally be retryable via isRetryableStatusCode.
	se := newDeepSeekStreamProviderErrorChunk([]byte(`{"message":"quota exceeded","type":"insufficient_quota"}`))
	if se.StatusCode == nil || *se.StatusCode != 429 {
		t.Errorf("StatusCode = %v, want 429", se.StatusCode)
	}
	if se.IsRetryable {
		t.Error("IsRetryable = true, want false for insufficient_quota")
	}
}

func TestDeepSeekStreamErrorExplicitHTTPStatusCodeWins(t *testing.T) {
	// TS: an explicit HTTP-status-shaped code takes precedence over the
	// discriminator table.
	se := newDeepSeekStreamProviderErrorChunk([]byte(`{"message":"custom","type":"rate_limit_exceeded","code":"503"}`))
	if se.StatusCode == nil || *se.StatusCode != 503 {
		t.Errorf("StatusCode = %v, want explicit 503 (not the rate_limit_exceeded discriminator's 429)", se.StatusCode)
	}
	if !se.IsRetryable {
		t.Error("IsRetryable = false, want true for 503")
	}
}

func TestDeepSeekStreamErrorDiscriminatorTable(t *testing.T) {
	se := newDeepSeekStreamProviderErrorChunk([]byte(`{"message":"model not found","type":"model_not_found"}`))
	if se.StatusCode == nil || *se.StatusCode != 404 {
		t.Errorf("StatusCode = %v, want 404", se.StatusCode)
	}
	if se.IsRetryable {
		t.Error("IsRetryable = true, want false for model_not_found")
	}
}

// TestDeepSeekStream_ErrorChunkAttachesStructuredPayload exercises the full
// stream path (SSE `error` field on a chunk) end to end.
func TestDeepSeekStream_ErrorChunkAttachesStructuredPayload(t *testing.T) {
	sseData := `data: {"error":{"message":"Rate limited","type":"rate_limit_exceeded"}}

data: [DONE]

`
	stream := newDeepseekStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("chunk.Type = %v, want ChunkTypeError", chunk.Type)
	}
	var streamErr *providererrors.StreamProviderError
	if !errors.As(chunk.Err, &streamErr) {
		t.Fatalf("chunk.Err = %v (%T), want *providererrors.StreamProviderError", chunk.Err, chunk.Err)
	}
	if streamErr.StatusCode == nil || *streamErr.StatusCode != 429 {
		t.Errorf("StatusCode = %v, want 429", streamErr.StatusCode)
	}
	if !streamErr.IsRetryable {
		t.Error("IsRetryable = false, want true for rate_limit_exceeded")
	}
}
