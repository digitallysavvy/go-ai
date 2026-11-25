package bytedance

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler: a failed status body that does not match
// byteDanceErrorSchema gives response.statusText, the server's reason phrase.
func TestR98provcByteDancePollReasonPhrase(t *testing.T) {
	err := (&VideoModel{}).handlePollError(context.Background(), &providererrors.DownloadError{URL: "https://x/p", StatusCode: 418, StatusText: "Custom Phrase", Body: []byte(`[1]`)})
	var be *Error
	if !errors.As(err, &be) || be.Message != "Custom Phrase" || err.Error() != "Custom Phrase" {
		t.Fatalf("got %T %v", err, err)
	}
}
