package deepgram

import (
	"errors"
	"net/http"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS lets a transport failure propagate unchanged and keeps the failed
// response body, data and lower-case headers on an HTTP error (APICallError).
func TestHandleErrorKeepsTransportFailuresAndResponseData(t *testing.T) {
	sentinel := errors.New("Cannot connect to API: boom")
	if got := handleError("deepgram", sentinel); got != sentinel {
		t.Fatalf("transport failure was wrapped: %v", got)
	}

	body := `{"err_code":"E","err_msg":"m","request_id":"x"}`
	got := handleError("deepgram", &internalhttp.HTTPStatusError{StatusCode: 400, Headers: http.Header{"X-Request-Id": {"r1"}}, Body: []byte(body)})
	var perr *providererrors.ProviderError
	if !errors.As(got, &perr) || perr.StatusCode != 400 || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["x-request-id"] != "r1" {
		t.Fatalf("got %#v", perr)
	}
}
