package voyage

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS APICallError keeps an empty message from {"detail":""}.
func TestHandleErrorKeepsEmptyDetail(t *testing.T) {
	err := handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"detail":""}`)})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "" || perr.StatusCode != 400 || perr.ResponseBody != `{"detail":""}` {
		t.Fatalf("got %#v", perr)
	}
}
