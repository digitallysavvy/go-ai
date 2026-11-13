package assemblyai

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS assemblyaiErrorDataSchema: {"error": string} (c87b19ee75) and
// {"error":{"message","code"}} bodies give their message; anything else
// gives the HTTP status text.
func TestHandleErrorFollowsTSErrorSchema(t *testing.T) {
	m := &TranscriptionModel{}
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"schema", `{"error":{"message":"bad","code":1}}`, "bad", 400},
		{"flat error string", `{"error":"nope"}`, "nope", 401},
		{"plain text", `oops`, "Bad Gateway", 502},
	} {
		err := m.handleError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.Message != c.want {
			t.Errorf("%s: got %v, want message %q", c.name, err, c.want)
		}
	}
}
