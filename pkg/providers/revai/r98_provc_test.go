package revai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler: a failed poll body that does not match
// the error schema gives response.statusText, the server's reason phrase.
func TestR98provcRevAIPollReasonPhrase(t *testing.T) {
	err := handleError(&providererrors.DownloadError{URL: "https://x/p", StatusCode: 418, StatusText: "Custom Phrase", Body: []byte(`{"other":1}`)})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Custom Phrase" {
		t.Fatalf("got %T %v", err, err)
	}
}
