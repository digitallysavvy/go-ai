package openai

import (
	"encoding/json"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createOpenAIStreamError passes streamError.isRetryable to the APICallError,
// so an insufficient_quota frame (HTTP 429) is not retryable.
func TestOpenAIStreamProviderErrorInsufficientQuotaNotRetryable(t *testing.T) {
	raw := json.RawMessage(`{"error":{"message":"You exceeded your quota","type":"insufficient_quota","code":"insufficient_quota"}}`)
	err := newOpenAIStreamProviderError("openai.responses", raw, http.Header{})
	pe, ok := err.(*providererrors.ProviderError)
	if !ok {
		t.Fatalf("err = %T", err)
	}
	if pe.StatusCode != 429 || pe.IsRetryable() {
		t.Fatalf("status %d retryable %v, want 429 and not retryable", pe.StatusCode, pe.IsRetryable())
	}

	rate := json.RawMessage(`{"error":{"message":"slow down","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
	pe = newOpenAIStreamProviderError("openai.responses", rate, http.Header{}).(*providererrors.ProviderError)
	if !pe.IsRetryable() {
		t.Fatal("rate limit frame must stay retryable")
	}
}
