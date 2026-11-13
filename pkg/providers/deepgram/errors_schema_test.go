package deepgram

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS deepgramErrorDataSchema: a body matching the error schema gives its message; any other
// body gives the HTTP status text.
func TestHandleErrorFollowsTSErrorSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"schema", `{"err_code":"X","err_msg":"bad"}`, "bad", 400},
		{"missing err_code", `{"err_msg":"bad"}`, "Bad Request", 400},
		{"plain text", `oops`, "Bad Gateway", 502},
	} {
		err := handleError("deepgram", &internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("%s: %T", c.name, err)
		}
		if perr.Message != c.want {
			t.Errorf("%s: message = %q, want %q", c.name, perr.Message, c.want)
		}
	}
}
