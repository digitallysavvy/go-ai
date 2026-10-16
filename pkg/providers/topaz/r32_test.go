package topaz

import (
	"errors"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// The failed response keeps its body, parsed data and TS-style headers.
func TestAPIErrorKeepsResponseBodyAndData(t *testing.T) {
	body := `{"message":"m","errorCode":"E"}`
	err := newTopazAPIError(400, []byte(body), http.Header{"X-Request-Id": {"r1"}})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["X-Request-Id"] != "r1" || perr.ErrorCode != "E" {
		t.Fatalf("got %#v", perr)
	}
}
