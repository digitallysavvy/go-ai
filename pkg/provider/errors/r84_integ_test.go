package errors_test

import (
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

type r84integBodyStatus struct{ body string }

func (e r84integBodyStatus) Error() string { return "HTTP 400" }
func (e r84integBodyStatus) HTTPStatusDetails() (int, map[string]string, []byte) {
	return 400, nil, []byte(e.body)
}

// provb CR 2: TS createJsonErrorResponseHandler parses response.text(): a
// BOM-prefixed error body gives its message and data, and an invalid
// sequence is one U+FFFD in ResponseBody and the message.
func TestR84integErrorBodyDecodedAsText(t *testing.T) {
	cause := r84integBodyStatus{body: "\xEF\xBB\xBF{\"error\":{\"message\":\"x\xF0\x9F\x98y\"}}"}
	got := providererrors.NewProviderError("p", 0, "", cause.Error(), cause)
	if got.Message != "x\uFFFDy" || got.Data == nil || got.ResponseBody != "{\"error\":{\"message\":\"x\uFFFDy\"}}" {
		t.Fatalf("got message %q data %v body %q", got.Message, got.Data, got.ResponseBody)
	}
}
