package errors_test

import (
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler keeps errorToMessage's value as sent: no
// trim. Expected values recorded from @ai-sdk/openai-compatible at
// 30548ee7f9 (doGenerate against a 400 response with each body; e.message).
func TestR86bcarryExtractAPIErrorMessageNoTrim(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"error":{"message":"  padded message \n"}}`, "  padded message \n"},
		{`{"error":{"message":" nbsp "}}`, " nbsp "},
		{`{"error":{"message":"   "}}`, "   "},
	} {
		if got := providererrors.ExtractAPIErrorMessage([]byte(tc.body)); got != tc.want {
			t.Errorf("ExtractAPIErrorMessage(%s) = %q, want %q", tc.body, got, tc.want)
		}
		cause := &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(tc.body)}
		e := providererrors.NewProviderError("p", 0, "", cause.Error(), cause)
		if e.Message != tc.want {
			t.Errorf("NewProviderError(%s).Message = %q, want %q", tc.body, e.Message, tc.want)
		}
	}
}
