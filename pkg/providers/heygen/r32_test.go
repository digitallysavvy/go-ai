package heygen

import (
	"errors"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// The failed response keeps its body, parsed data and TS-style headers.
func TestAPIErrorKeepsResponseBodyAndData(t *testing.T) {
	body := `{"error":{"message":"m","code":"c","param":"p"}}`
	err := newHeyGenAPIError(400, []byte(body), http.Header{"X-Request-Id": {"r1"}})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "m (c) [p]" || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["x-request-id"] != "r1" || perr.ErrorCode != "c" {
		t.Fatalf("got %#v", perr)
	}
}
