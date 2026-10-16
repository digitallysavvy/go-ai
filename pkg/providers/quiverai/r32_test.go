package quiverai

import (
	"errors"
	"net/http"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// The failed response keeps its body, data and headers (APICallError) on the
// image model and on the Responses handler, with the 429/5xx retry rule.
func TestFailedResponsesKeepBodyDataAndHeaders(t *testing.T) {
	body := `{"status":400,"code":"bad","message":"m","request_id":"r"}`
	httpErr := func() *internalhttp.HTTPStatusError {
		return &internalhttp.HTTPStatusError{StatusCode: 409, Headers: http.Header{"X-Request-Id": {"r1"}}, Body: []byte(body)}
	}
	for name, got := range map[string]error{
		"image":     (&ImageModel{}).handleError(httpErr()),
		"responses": quiverAIFailedResponseHandler(httpErr()),
	} {
		var perr *providererrors.ProviderError
		if !errors.As(got, &perr) || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["x-request-id"] != "r1" ||
			perr.Message != "m" || perr.IsRetryable() {
			t.Errorf("%s: got %#v", name, perr)
		}
	}
}
