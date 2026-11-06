package providerutils

import (
	"errors"
	"fmt"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestStatusCodeDownloadError(t *testing.T) {
	dl := providererrors.NewDownloadError("https://x/y", 404, "404 Not Found", "", nil)
	dl.Body = []byte("gone")
	dl.Headers = map[string][]string{"X-Req": {"a", "b"}}
	err := StatusCodeDownloadError("luma", fmt.Errorf("wrapped: %w", dl))
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T %v", err, err)
	}
	if pe.StatusCode != 404 || pe.Message != "Not Found" || pe.ResponseBody != "gone" || pe.Provider != "luma" {
		t.Fatalf("pe = %+v", pe)
	}

	// Validation and size failures pass through unchanged.
	other := providererrors.NewDownloadError("https://x/y", 0, "", "too big", nil)
	if got := StatusCodeDownloadError("luma", other); got != error(other) {
		t.Fatalf("passthrough changed the error: %v", got)
	}
	plain := errors.New("dial failed")
	if got := StatusCodeDownloadError("luma", plain); got != plain {
		t.Fatalf("plain error changed: %v", got)
	}
}
