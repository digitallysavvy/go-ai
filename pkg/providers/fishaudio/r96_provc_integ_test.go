package fishaudio

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR96provcErrorStatusOutOfRange: status is z.number().nullish(), which
// rejects Infinity (JSON.parse of 1e400): the body does not match, so the
// message is the HTTP status text.
func TestR96provcErrorStatusOutOfRange(t *testing.T) {
	err := handleError(&internalhttp.HTTPStatusError{StatusCode: 500, Body: []byte(`{"status":1e400,"message":"m"}`)})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Internal Server Error" || perr.Data != nil {
		t.Fatalf("err = %v, want the status text", err)
	}
}
