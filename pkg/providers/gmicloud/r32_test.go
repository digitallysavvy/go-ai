package gmicloud

import (
	"errors"
	"net/http"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS lets a transport failure propagate unchanged and keeps the failed
// response body, data and headers on an HTTP error (APICallError).
func TestHandleErrorKeepsTransportFailuresAndResponseData(t *testing.T) {
	sentinel := errors.New("Cannot connect to API: boom")
	if got := (&LanguageModel{}).handleError(sentinel); got != sentinel {
		t.Fatalf("transport failure was wrapped: %v", got)
	}
	body := `{"error":{"message":"m"}}`
	got := (&LanguageModel{}).handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Headers: http.Header{"X-Request-Id": {"r1"}}, Body: []byte(body)})
	var perr *providererrors.ProviderError
	if !errors.As(got, &perr) || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["X-Request-Id"] != "r1" || perr.Message != "m" {
		t.Fatalf("got %#v", perr)
	}
}
