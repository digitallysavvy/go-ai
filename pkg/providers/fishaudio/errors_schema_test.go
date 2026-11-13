package fishaudio

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS fishAudioErrorDataSchema: a body matching the error schema gives its message; any other
// body gives the HTTP status text.
func TestHandleErrorFollowsTSErrorSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"message", `{"status":401,"message":"no permission"}`, "no permission", 401},
		{"empty object", `{}`, "Unknown Fish Audio error", 402},
		{"null message", `{"message":null}`, "Unknown Fish Audio error", 402},
		{"not json", `oops`, "Bad Gateway", 502},
		{"wrong type", `{"message":5}`, "Bad Request", 400},
	} {
		err := handleError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("%s: %T", c.name, err)
		}
		if perr.Message != c.want {
			t.Errorf("%s: message = %q, want %q", c.name, perr.Message, c.want)
		}
	}
}
