package ai

import (
	"fmt"
	"testing"
)

type httpStatusOnlyError struct{ status int }

func (e *httpStatusOnlyError) Error() string { return fmt.Sprintf("status %d", e.status) }

func (e *httpStatusOnlyError) HTTPStatusDetails() (int, map[string]string, []byte) {
	return e.status, nil, nil
}

// A provider error that is not a ProviderError but carries its HTTP status
// (KlingAI, ByteDance) is retried like a TS APICallError: 408, 409, 429 and
// 5xx only.
func TestIsGatewayCallRetryableHonorsHTTPStatusDetails(t *testing.T) {
	for status, want := range map[int]bool{0: false, 400: false, 401: false, 404: false, 408: true, 409: true, 429: true, 500: true, 503: true} {
		err := fmt.Errorf("wrapped: %w", &httpStatusOnlyError{status: status})
		if got := isGatewayCallRetryable(err); got != want {
			t.Errorf("status %d retryable = %v, want %v", status, got, want)
		}
	}
}
