package klingai

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler: a failed status body that does not match
// klingaiErrorDataSchema gives response.statusText, the server's reason phrase.
func TestR98provcKlingAIPollReasonPhrase(t *testing.T) {
	err := (&VideoModel{}).handlePollError(context.Background(), &providererrors.DownloadError{URL: "https://x/p", StatusCode: 418, StatusText: "Custom Phrase", Body: []byte(`{"other":1}`)})
	var ke *Error
	if !errors.As(err, &ke) || ke.Message != "Custom Phrase" {
		t.Fatalf("got %T %v", err, err)
	}
}
