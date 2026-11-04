package moonshot

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler with moonshotAIErrorSchema: an empty
// error.message is a valid message (z.string()); a missing one is not.
func TestR87provbErrorEmptyMessageKept(t *testing.T) {
	err := parseMoonshotProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":{"message":"","type":"invalid_request_error"}}`)})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "" || pe.ErrorCode != "invalid_request_error" {
		t.Fatalf("err = %#v, want a ProviderError with an empty message", err)
	}
	raw := &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":{"type":"x"}}`)}
	if err := parseMoonshotProviderError(raw); err != raw {
		t.Fatalf("missing message: err = %#v, want the status error unchanged", err)
	}
}
