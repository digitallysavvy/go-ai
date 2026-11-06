package voyage

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS voyageErrorDataSchema is {detail: string}; any other body gives the HTTP
// status text.
func TestHandleErrorFollowsTSSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"detail", `{"detail":"Invalid API key"}`, "Invalid API key", 401},
		{"message is off schema", `{"message":"nope"}`, "Unauthorized", 401},
		{"detail object is off schema", `{"detail":[{"msg":"x"}]}`, "Unprocessable Entity", 422},
		{"plain text", `oops`, "Bad Gateway", 502},
	} {
		err := handleError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.Message != c.want || perr.StatusCode != c.status {
			t.Errorf("%s: got %v, want message %q status %d", c.name, err, c.want, c.status)
		}
	}
}
