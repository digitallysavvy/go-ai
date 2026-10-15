package errors_test

import (
	"fmt"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"net/http"
)

func TestNewProviderErrorLiftsHTTPStatus(t *testing.T) {
	cause := &internalhttp.HTTPStatusError{
		StatusCode: 401,
		Headers:    http.Header{"Retry-After": {"3"}},
		Body:       []byte(`{"error":{"message":"bad key"}}`),
	}
	e := providererrors.NewProviderError("openai", 0, "", cause.Error(), fmt.Errorf("wrap: %w", cause))
	if e.StatusCode != 401 || e.IsRetryable() {
		t.Fatalf("status=%d retryable=%v", e.StatusCode, e.IsRetryable())
	}
	if e.Message != "bad key" {
		t.Fatalf("message=%q", e.Message)
	}
	if e.ResponseHeaders["retry-after"] != "3" || e.ResponseBody == "" || e.Data == nil {
		t.Fatalf("missing response data: %+v", e)
	}
}

func TestNewProviderErrorKeepsCustomMessageAndRetryable(t *testing.T) {
	cause := &internalhttp.HTTPStatusError{StatusCode: 429, Body: []byte(`{"message":"slow down"}`)}
	e := providererrors.NewProviderError("x", 0, "", "custom", cause)
	if e.Message != "custom" || !e.IsRetryable() || e.StatusCode != 429 {
		t.Fatalf("%+v", e)
	}
	for _, c := range []int{408, 409} {
		if !providererrors.NewProviderError("x", c, "", "m", nil).IsRetryable() {
			t.Fatalf("%d should be retryable", c)
		}
	}
	if providererrors.NewProviderError("x", 400, "", "m", nil).IsRetryable() {
		t.Fatal("400 must not be retryable")
	}
}
